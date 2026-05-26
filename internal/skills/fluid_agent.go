package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

)

// BootstrapFluidExecutionAgent downloads a Fluid execution agent binary from the control plane
// static tree and starts it detached so it can enroll (fluid/agents/core/enroll) and connect.
// Child agent.yml content must be supplied in the step payload as agent_config_yaml (single source
// of truth); it is written to agent_config_path with overwrite. If agent_config_yaml is empty and
// the config file is missing, the skill fails. Enrollment credentials file path is separate.
func BootstrapFluidExecutionAgent(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	slug := strings.TrimSpace(stringFromPayload(payload, "agent_slug", ""))
	if slug == "" {
		return nil, fmt.Errorf("agent_slug is required")
	}
	token := strings.TrimSpace(stringFromPayload(payload, "enrollment_token", ""))
	if token == "" {
		return nil, fmt.Errorf("enrollment_token is required")
	}
	runID := strings.TrimSpace(stringFromPayload(payload, "use_case_run_id", ""))
	if runID == "" {
		return nil, fmt.Errorf("use_case_run_id is required")
	}
	httpBase := strings.TrimSpace(stringFromPayload(payload, "controlplane_http_base", ""))
	if httpBase == "" {
		return nil, fmt.Errorf("controlplane_http_base is required")
	}
	cfgPath := strings.TrimSpace(stringFromPayload(payload, "agent_config_path", ""))
	if cfgPath == "" {
		return nil, fmt.Errorf("agent_config_path is required")
	}
	credPath := strings.TrimSpace(stringFromPayload(payload, "credentials_path", ""))
	if credPath == "" {
		return nil, fmt.Errorf("credentials_path is required")
	}
	enrollEnv := strings.TrimSpace(stringFromPayload(payload, "enrollment_env_path", ""))
	if enrollEnv == "" {
		return nil, fmt.Errorf("enrollment_env_path is required")
	}
	installPath := strings.TrimSpace(stringFromPayload(payload, "install_path", ""))
	if installPath == "" {
		return nil, fmt.Errorf("install_path is required")
	}

	purge := boolFromPayload(payload, "purge_enrollment_sources", false)
	logPath := stringFromPayload(payload, "log_path", filepath.Join("/var/log", fmt.Sprintf("fluid-agent-%s.log", slug)))

	filename := fmt.Sprintf("fluid-agent-%s", slug)
	base := strings.TrimRight(httpBase, "/")
	downloadURL := fmt.Sprintf("%s/binaries/agents/%s/linux/amd64/%s", base, slug, filename)

	if err := downloadBinary(ctx, downloadURL, installPath); err != nil {
		return nil, err
	}

	if err := ensureBootstrapPaths(cfgPath, credPath, enrollEnv); err != nil {
		return nil, err
	}

	if err := writeAgentConfigFromPayload(cfgPath, payload); err != nil {
		return nil, err
	}

	extra, err := json.Marshal(map[string]string{
		"use_case_run_id": runID,
		"execution_role":  slug,
	})
	if err != nil {
		return nil, fmt.Errorf("extra_args json: %w", err)
	}

	// Inherit only a small allowlist from the parent (see inheritedParentEnviron). No FLUID_* is
	// taken from the parent: enrollment and run correlation for the child are set here (or in
	// agent_config_yaml on disk), not inherited from the Debian agent.
	env := childAgentEnviron()
	env = append(env,
		"FLUID_ENROLLMENT_TOKEN="+token,
		"FLUID_ENROLLMENT_EXTRA_ARGS="+string(extra),
		"FLUID_CONTROLPLANE_HTTP_BASE="+httpBase,
		// Same run id as in FLUID_ENROLLMENT_EXTRA_ARGS; enroll.RunIDFromPrefetchSources prefers
		// this env for CP credential issuance (e.g. aws.agent.health prefetch).
		"FLUID_USE_CASE_RUN_ID="+runID,
	)
	// controlplane.websocket_url, enrollment.name, service_credentials, etc. come from
	// agent_config_yaml on disk. Do not set FLUID_CONTROLPLANE_WEBSOCKET_URL, FLUID_ENROLLMENT_NAME,
	// or FLUID_SERVICE_CREDENTIALS here: they would override the file and break single-source config.
	if purge {
		env = append(env, "FLUID_ENROLL_PURGE_ENROLLMENT_SOURCES=true")
	} else {
		env = append(env, "FLUID_ENROLL_PURGE_ENROLLMENT_SOURCES=false")
	}

	if bundle := strings.TrimSpace(stringFromPayload(payload, "gitlab_token_env_file", "")); bundle != "" {
		tok, err := gitlabTokenFromBundleFile(bundle)
		if err != nil {
			return nil, fmt.Errorf("gitlab_token_env_file: %w", err)
		}
		if tok != "" {
			env = append(env, "GITLAB_TOKEN="+tok)
		}
	}

	if scf := strings.TrimSpace(stringFromPayload(payload, "service_credentials_env_file", "")); scf != "" {
		env = append(env, "FLUID_SERVICE_CREDENTIALS_ENV_FILE="+scf)
	}

	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log %q: %w", logPath, err)
	}
	defer logF.Close()

	// Detached child must not use the skill ctx: execute() defers cancel() on that ctx when the
	// skill returns, and exec.CommandContext would kill the child on cancel (immediate defunct,
	// empty log, wait_worker_* never succeeds).
	cmd := exec.Command(installPath,
		"-config", cfgPath,
		"-credentials", credPath,
		"-enrollment-env", enrollEnv,
	)
	cmd.Env = env
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", installPath, err)
	}
	go func() { _ = cmd.Wait() }()
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	return map[string]interface{}{
		"started":      true,
		"pid":          pid,
		"install_path": installPath,
		"log_path":     logPath,
		"agent_slug":   slug,
	}, nil
}

// childAgentEnviron is the parent environment slice we pass through before adding bootstrap
// variables. We use an explicit allowlist so no FLUID_* (or any other parent-only) variable
// can override the child's agent.yml or the FLUID_* values set below.
func childAgentEnviron() []string {
	return inheritedParentEnviron(os.Environ())
}

// inheritedParentEnviron returns env entries from the parent (typically os.Environ) that are
// safe to pass to a bootstrapped child: OS/network/locale, never FLUID_* (all Fluid config is
// explicit in the payload or the written child YAML). LC_* is allowed as a group for locale.
func inheritedParentEnviron(in []string) []string {
	out := make([]string, 0, 24)
	seen := make(map[string]struct{}, 32)
	for _, e := range in {
		k := environmentEntryName(e)
		if k == "" {
			continue
		}
		if strings.HasPrefix(k, "FLUID_") {
			continue
		}
		if !parentEnvKeyAllowed(k) {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, e)
	}
	return out
}

// parentEnvAllowlist is the exact set of non-FLUID parent keys we copy. Proxies, TLS trust,
// basic POSIX, and time/locale. Do not add AWS_* / GITHUB_* / GITLAB_* here: the child agent
// should not inherit cloud credentials or URLs from the Debian parent; use use case + YAML.
var parentEnvAllowlist = map[string]struct{}{
	"PATH":       {},
	"HOME":       {},
	"USER":       {},
	"LOGNAME":    {},
	"SHELL":      {},
	"TMPDIR":     {},
	"TERM":       {},
	"LANG":       {},
	"TZ":         {},
	"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "NO_PROXY": {},
	"http_proxy": {}, "https_proxy": {}, "no_proxy": {},
	"SSL_CERT_FILE":       {},
	"SSL_CERT_DIR":        {},
	"REQUESTS_CA_BUNDLE":  {},
}

func parentEnvKeyAllowed(key string) bool {
	if strings.HasPrefix(key, "LC_") {
		return true
	}
	_, ok := parentEnvAllowlist[key]
	return ok
}

func environmentEntryName(s string) string {
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return s
	}
	return s[:i]
}

// writeAgentConfigFromPayload writes agent_config_yaml to cfgPath, overwriting the file.
// If agent_config_yaml is empty: no write when the file already exists; error when it does not.
func writeAgentConfigFromPayload(cfgPath string, payload map[string]interface{}) error {
	cfgPath = strings.TrimSpace(cfgPath)
	if cfgPath == "" {
		return nil
	}
	raw, err := agentConfigYAMLString(payload)
	if err != nil {
		return err
	}
	if raw == "" {
		if _, err := os.Stat(cfgPath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("agent_config_yaml is required when agent config file %q does not exist", cfgPath)
			}
			return fmt.Errorf("stat agent config %q: %w", cfgPath, err)
		}
		return nil
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return fmt.Errorf("agent_config_yaml: invalid yaml: %w", err)
	}
	header := []byte("# Written by debian.fluid_execution_agent.bootstrap (use case-injected config).\n")
	if err := os.WriteFile(cfgPath, append(header, []byte(raw)...), 0o644); err != nil {
		return fmt.Errorf("write agent config %q: %w", cfgPath, err)
	}
	return nil
}

func agentConfigYAMLString(payload map[string]interface{}) (string, error) {
	v, ok := payload["agent_config_yaml"]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("agent_config_yaml must be a string")
	}
	return strings.TrimSpace(s), nil
}

func downloadBinary(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".fluid-agent-dl-*")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 256<<20)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := verifyELF(tmpName); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("rename to %s: %w", dest, err)
	}
	cleanup = false
	return nil
}

func verifyELF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	if !bytes.Equal(hdr, []byte{0x7f, 'E', 'L', 'F'}) {
		return fmt.Errorf("downloaded file is not a Linux ELF executable")
	}
	return nil
}

func ensureBootstrapPaths(cfgPath, credPath, enrollEnv string) error {
	for _, p := range []string{cfgPath, credPath, enrollEnv} {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		dir := filepath.Dir(p)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}
	}
	return nil
}

// gitlabTokenFromBundleFile reads FLUID_GITLAB_SA_TOKEN from a bundle.env-style file (KEY="value" lines).
func gitlabTokenFromBundleFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		const p = "FLUID_GITLAB_SA_TOKEN="
		if strings.HasPrefix(line, p) {
			return parseEnvLineValue(strings.TrimPrefix(line, p)), nil
		}
	}
	return "", fmt.Errorf("FLUID_GITLAB_SA_TOKEN not found in %s", path)
}

func parseEnvLineValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = strings.Trim(s, `"`)
		s = strings.ReplaceAll(s, `\"`, `"`)
		s = strings.ReplaceAll(s, `\\`, `\`)
	}
	return s
}
