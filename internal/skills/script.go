package skills

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	agentexec "fluid/agents/debian/internal/exec"
)

type ScriptSkills struct {
	runner          agentexec.Runner
	allowedPrefixes []string
}

func NewScriptSkills(runner agentexec.Runner, allowedPrefixes []string) *ScriptSkills {
	return &ScriptSkills{
		runner:          runner,
		allowedPrefixes: allowedPrefixes,
	}
}

func (s *ScriptSkills) Run(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	script := strings.TrimSpace(stringFromPayload(payload, "script", ""))
	if script == "" {
		return nil, fmt.Errorf("script is required")
	}
	args, err := optionalStringList(payload, "args")
	if err != nil {
		return nil, err
	}
	workingDir := stringFromPayload(payload, "working_dir", "")
	envMap, err := optionalStringMap(payload, "env")
	if err != nil {
		return nil, err
	}
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	if fluidLogPath := strings.TrimSpace(stringFromPayload(payload, "fluid_log_path", "")); fluidLogPath != "" {
		env = append(env, fmt.Sprintf("FLUID_LOG_PATH=%s", fluidLogPath))
	}
	for k, v := range envMap {
		if !s.isAllowedEnvKey(k) {
			return nil, fmt.Errorf("env key not allowed: %s", k)
		}
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	var runAsUser *user.User

	tmpDir := os.TempDir()
	scriptFile, err := os.CreateTemp(tmpDir, "fluid-debian-script-*.sh")
	if err != nil {
		return nil, fmt.Errorf("create temp script: %w", err)
	}
	defer func() { _ = os.Remove(scriptFile.Name()) }()
	defer func() { _ = scriptFile.Close() }()

	if _, err := scriptFile.WriteString(script + "\n"); err != nil {
		return nil, fmt.Errorf("write temp script: %w", err)
	}
	if err := scriptFile.Chmod(0o700); err != nil {
		return nil, fmt.Errorf("chmod temp script: %w", err)
	}

	scriptPath := filepath.Clean(scriptFile.Name())
	var cred *syscall.Credential

	if runAs := strings.TrimSpace(stringFromPayload(payload, "run_as", "")); runAs != "" {
		urec, err := user.Lookup(runAs)
		if err != nil {
			return nil, fmt.Errorf("run_as: unknown user %q: %w", runAs, err)
		}
		runAsUser = urec
		uid, err := strconv.Atoi(urec.Uid)
		if err != nil {
			return nil, fmt.Errorf("run_as: uid: %w", err)
		}
		gid, err := strconv.Atoi(urec.Gid)
		if err != nil {
			return nil, fmt.Errorf("run_as: gid: %w", err)
		}
		euid := os.Geteuid()
		if euid != 0 {
			if euid != uid {
				return nil, fmt.Errorf("run_as=%q requires a root agent (euid=%d)", runAs, euid)
			}
		} else if uid != 0 {
			if err := os.Chown(scriptPath, uid, gid); err != nil {
				return nil, fmt.Errorf("run_as: chown: %w", err)
			}
			cred = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
		}
	}

	if runAsUser != nil {
		h := strings.TrimSpace(runAsUser.HomeDir)
		if h != "" {
			env = envWithPasswdIdentity(env, runAsUser.Username, h)
		}
	}

	cmdArgs := append([]string{scriptPath}, args...)
	res, runErr := s.runner.RunWithCred(ctx, "/bin/bash", cmdArgs, env, workingDir, cred)
	accepted := parseAcceptedExitCodes(payload)
	out := map[string]interface{}{
		"changed":     intSliceContains(accepted, res.ExitCode),
		"exit_code":   res.ExitCode,
		"stdout_tail": res.StdoutTail,
		"stderr_tail": res.StderrTail,
		"duration_ms": res.DurationMs,
	}
	if runErr != nil {
		if intSliceContains(accepted, res.ExitCode) {
			return out, nil
		}
		return out, runErr
	}
	return out, nil
}

func parseAcceptedExitCodes(payload map[string]interface{}) []int {
	v, ok := payload["accepted_exit_codes"]
	if !ok || v == nil {
		return []int{0}
	}
	raw, ok := v.([]interface{})
	if !ok {
		return []int{0}
	}
	out := make([]int, 0, len(raw))
	for _, x := range raw {
		switch n := x.(type) {
		case float64:
			out = append(out, int(n))
		case int:
			out = append(out, n)
		case int64:
			out = append(out, int(n))
		default:
			// ignore invalid entries
		}
	}
	if len(out) == 0 {
		return []int{0}
	}
	return out
}

func intSliceContains(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

func (s *ScriptSkills) isAllowedEnvKey(k string) bool {
	for _, prefix := range s.allowedPrefixes {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// envWithPasswdIdentity forces HOME/USER/LOGNAME for run_as so shells never inherit root's HOME
// when the child euid is dropped to an unprivileged account.
func envWithPasswdIdentity(env []string, login, home string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "USER=") || strings.HasPrefix(e, "LOGNAME=") {
			continue
		}
		filtered = append(filtered, e)
	}
	id := []string{
		fmt.Sprintf("HOME=%s", home),
		fmt.Sprintf("USER=%s", login),
		fmt.Sprintf("LOGNAME=%s", login),
	}
	return append(id, filtered...)
}
