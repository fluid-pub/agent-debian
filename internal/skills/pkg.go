package skills

import (
	"context"
	"fmt"
	"strings"

	agentexec "fluid/agents/debian/internal/exec"
)

type PkgSkills struct {
	runner agentexec.Runner
}

func NewPkgSkills(runner agentexec.Runner) *PkgSkills {
	return &PkgSkills{runner: runner}
}

func (s *PkgSkills) UpdateIndex(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	res, err := s.runApt(ctx, []string{"update"})
	return resultFromRun(res, true), err
}

func (s *PkgSkills) Install(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	packages, err := requiredStringList(payload, "packages")
	if err != nil {
		return nil, err
	}
	updateIndex := boolFromPayload(payload, "update_index", false)
	allowDowngrade := boolFromPayload(payload, "allow_downgrade", false)

	if updateIndex {
		_, _ = s.runApt(ctx, []string{"update"})
	}

	args := []string{"install", "-y"}
	if allowDowngrade {
		args = append(args, "--allow-downgrades")
	}
	args = append(args, packages...)

	res, runErr := s.runApt(ctx, args)
	data := resultFromRun(res, true)
	data["installed"] = packages
	return data, runErr
}

func (s *PkgSkills) Remove(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	packages, err := requiredStringList(payload, "packages")
	if err != nil {
		return nil, err
	}
	purge := boolFromPayload(payload, "purge", false)
	args := []string{"remove", "-y"}
	if purge {
		args = append(args, "--purge")
	}
	args = append(args, packages...)
	res, runErr := s.runApt(ctx, args)
	data := resultFromRun(res, true)
	data["removed"] = packages
	return data, runErr
}

func (s *PkgSkills) Upgrade(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	mode := stringFromPayload(payload, "mode", "safe")
	autoremove := boolFromPayload(payload, "autoremove", false)

	var args []string
	switch mode {
	case "safe":
		args = []string{"upgrade", "-y"}
	case "full":
		args = []string{"dist-upgrade", "-y"}
	default:
		return nil, fmt.Errorf("invalid mode, expected safe or full")
	}

	res, runErr := s.runApt(ctx, args)
	if runErr == nil && autoremove {
		_, _ = s.runApt(ctx, []string{"autoremove", "-y"})
	}
	data := resultFromRun(res, true)
	data["upgraded_count"] = parseAptUpgradeCount(res.StdoutTail + "\n" + res.StderrTail)
	return data, runErr
}

func (s *PkgSkills) runApt(ctx context.Context, args []string) (agentexec.Result, error) {
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"DEBIAN_FRONTEND=noninteractive",
	}
	return s.runner.Run(ctx, "apt-get", args, env, "")
}

func resultFromRun(res agentexec.Result, changed bool) map[string]interface{} {
	return map[string]interface{}{
		"changed":     changed && res.ExitCode == 0,
		"exit_code":   res.ExitCode,
		"stdout_tail": res.StdoutTail,
		"stderr_tail": res.StderrTail,
		"duration_ms": res.DurationMs,
	}
}

func parseAptUpgradeCount(text string) int {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "upgraded") {
			continue
		}
		var upgraded int
		_, err := fmt.Sscanf(line, "%d upgraded,", &upgraded)
		if err == nil {
			return upgraded
		}
	}
	return 0
}
