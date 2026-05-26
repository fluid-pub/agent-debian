package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	coreexec "fluid/agents/core/execution"
	"fluid/agents/core/skillresult"
	"fluid/agents/debian/internal/config"
	agentexec "fluid/agents/debian/internal/exec"
	"fluid/agents/debian/internal/skills"
)

type Agent struct {
	cfg     *config.Config
	core    *coreexec.Agent
	allowed map[string]struct{}
	pkg     *skills.PkgSkills
	script  *skills.ScriptSkills
}

func New(cfg *config.Config) (*Agent, error) {
	allowed := make(map[string]struct{}, len(cfg.Skills.Allowed))
	for _, s := range cfg.Skills.Allowed {
		allowed[strings.TrimSpace(s)] = struct{}{}
	}

	runner := agentexec.NewOSRunner()
	return &Agent{
		cfg:     cfg,
		allowed: allowed,
		pkg:     skills.NewPkgSkills(runner),
		script:  skills.NewScriptSkills(runner, cfg.Debian.ScriptAllowedEnvPrefixes),
	}, nil
}

func (a *Agent) Start() error {
	a.core = coreexec.New(coreexec.Config{
		WebSocketURL:     a.cfg.Controlplane.WebSocketURL,
		OrganizationUUID: a.cfg.Controlplane.OrganizationUUID,
		Token:            a.cfg.Controlplane.Token,
		Name:             "debian",
		AllowedSkills:    len(a.allowed),
		LogEventsEnabled: a.cfg.Logs.Enabled == nil || *a.cfg.Logs.Enabled,
		LogVerbosity:     a.cfg.Logs.Verbosity,
		RuntimeConfig:    runtimeConfigForControlPlane(a.cfg),
	}, a.execute)
	return a.core.Start()
}

func runtimeConfigForControlPlane(cfg *config.Config) map[string]interface{} {
	skillsCfg := map[string]interface{}{
		"allowed": cfg.Skills.Allowed,
	}
	if len(cfg.Skills.Definitions) > 0 {
		skillsCfg["definitions"] = cfg.Skills.Definitions
	}
	return map[string]interface{}{
		"agent": map[string]interface{}{
			"mode":    cfg.Agent.Mode,
			"name":    cfg.Agent.Name,
			"version": cfg.Agent.Version,
		},
		"skills": skillsCfg,
	}
}

func (a *Agent) Stop() {
	if a.core != nil {
		a.core.Stop()
	}
}

func (a *Agent) execute(skill string, payload map[string]interface{}, _ map[string]interface{}) (map[string]interface{}, error) {
	if _, ok := a.allowed[skill]; !ok {
		return nil, fmt.Errorf("skill not allowed: %s", skill)
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}

	timeoutSeconds := int64(600)
	if raw, ok := payload["timeout_seconds"].(float64); ok && raw > 0 {
		timeoutSeconds = int64(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	var (
		data map[string]interface{}
		err  error
	)
	switch skill {
	case "debian.agent.health":
		// Liveness for core.wait_for (same contract as gitlab.agent.health / aws.agent.health).
		data = map[string]interface{}{"connected": true}
	case "debian.pkg.update_index":
		data, err = a.pkg.UpdateIndex(ctx, payload)
	case "debian.pkg.install":
		data, err = a.pkg.Install(ctx, payload)
	case "debian.pkg.remove":
		data, err = a.pkg.Remove(ctx, payload)
	case "debian.pkg.upgrade":
		data, err = a.pkg.Upgrade(ctx, payload)
	case "debian.script.run":
		data, err = a.script.Run(ctx, payload)
		if err != nil {
			// Preserve script execution tails in the envelope so control plane can persist/inspect them.
			return skillresult.FailureWithData(err.Error(), data), nil
		}
	case "debian.workspace.prepare":
		data, err = skills.PrepareWorkspace(ctx, payload)
	case "debian.terraform.init":
		data, err = skills.TerraformInit(ctx, payload)
		if err != nil {
			return skillresult.FailureWithData(err.Error(), data), nil
		}
	case "debian.terraform.plan":
		data, err = skills.TerraformPlan(ctx, payload)
		if err != nil {
			return skillresult.FailureWithData(err.Error(), data), nil
		}
	case "debian.fluid_execution_agent.bootstrap":
		data, err = skills.BootstrapFluidExecutionAgent(ctx, payload)
	default:
		return nil, fmt.Errorf("unsupported skill: %s", skill)
	}
	if err != nil {
		return nil, err
	}
	return skillresult.Success(data), nil
}

func stringField(payload map[string]interface{}, key string) string {
	if v, ok := payload[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
