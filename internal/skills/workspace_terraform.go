package skills

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// PrepareWorkspace creates the standard Fluid workspace directories under /tmp/fluid/{run_id}/.
// Layout: secrets/, workspace/, and artifacts/ are run-level siblings; repo/ lives under workspace/.
// With optional workspace_owner (POSIX login), only the traversable run root and workspace tree are
// chowned to that user. secrets/ stays root-only so the root agent remains the secrets broker.
// Payload: use_case_run_id (or fluid_use_case_run_id), optional workspace_owner, optional run_context.
func PrepareWorkspace(_ context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	rid := runIDFromPayload(payload)
	if rid == "" {
		return nil, fmt.Errorf("use_case_run_id or fluid_use_case_run_id is required")
	}
	runRoot := filepath.Join("/tmp", "fluid", sanitizePathSegment(rid))
	base := filepath.Join(runRoot, "workspace")
	repo := filepath.Join(base, "repo")
	secrets := filepath.Join(runRoot, "secrets")
	artifacts := filepath.Join(runRoot, "artifacts")
	ownerLogin := strings.TrimSpace(stringField(payload, "workspace_owner"))

	type dirPerm struct {
		path string
		mode os.FileMode
	}
	toCreate := []dirPerm{
		{runRoot, 0o755},
		{secrets, 0o700},
		{artifacts, 0o750},
	}
	if ownerLogin != "" {
		toCreate = append(toCreate,
			dirPerm{base, 0o750},
			dirPerm{repo, 0o750},
		)
	} else {
		toCreate = append(toCreate,
			dirPerm{base, 0o755},
			dirPerm{repo, 0o755},
		)
	}
	for _, dp := range toCreate {
		if err := os.MkdirAll(dp.path, dp.mode); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", dp.path, err)
		}
		if err := os.Chmod(dp.path, dp.mode); err != nil {
			return nil, fmt.Errorf("chmod %s: %w", dp.path, err)
		}
	}

	var (
		hasOwner bool
		ownerUID int
		ownerGID int
	)
	if ownerLogin != "" {
		u, err := user.Lookup(ownerLogin)
		if err != nil {
			return nil, fmt.Errorf("workspace_owner: lookup %q: %w", ownerLogin, err)
		}
		uid, err := strconv.Atoi(u.Uid)
		if err != nil {
			return nil, fmt.Errorf("workspace_owner: uid %q: %w", u.Uid, err)
		}
		gid, err := strconv.Atoi(u.Gid)
		if err != nil {
			return nil, fmt.Errorf("workspace_owner: gid %q: %w", u.Gid, err)
		}
		hasOwner, ownerUID, ownerGID = true, uid, gid
		// Include runRoot for traversal; secrets and artifacts remain root-owned run-level siblings.
		for _, p := range []string{runRoot, base, repo} {
			if err := os.Chown(p, ownerUID, ownerGID); err != nil {
				return nil, fmt.Errorf("workspace_owner chown %s: %w", p, err)
			}
			if err := os.Chmod(p, 0o750); err != nil {
				return nil, fmt.Errorf("workspace_owner chmod %s: %w", p, err)
			}
		}
	}

	out := map[string]interface{}{
		"ok":             true,
		"workspace_path": base,
		"repo_path":      repo,
		"secrets_dir":    secrets,
		"artifacts_dir":  artifacts,
	}

	if ctxVal, ok := payload["run_context"]; ok && ctxVal != nil {
		contextFile := filepath.Join(base, "fluid_context.json")
		if err := writeWorkspaceContextFile(contextFile, ctxVal); err != nil {
			return nil, err
		}
		if hasOwner {
			if err := os.Chown(contextFile, ownerUID, ownerGID); err != nil {
				return nil, fmt.Errorf("workspace_owner chown %s: %w", contextFile, err)
			}
		}
		out["context_file"] = contextFile
	}

	return out, nil
}

// configureExecCmdRunAs drops privileges for cmd when runAs is set and the agent is root (same rules as debian.script.run).
// Returns env to assign to cmd.Env (may rewrite HOME/USER/LOGNAME via envWithPasswdIdentity from script.go).
func configureExecCmdRunAs(cmd *exec.Cmd, env []string, runAs string) ([]string, error) {
	runAs = strings.TrimSpace(runAs)
	if runAs == "" {
		return env, nil
	}
	urec, err := user.Lookup(runAs)
	if err != nil {
		return nil, fmt.Errorf("run_as: unknown user %q: %w", runAs, err)
	}
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
		return env, nil
	}
	if uid == 0 {
		return env, nil
	}
	outEnv := env
	if h := strings.TrimSpace(urec.HomeDir); h != "" {
		outEnv = envWithPasswdIdentity(env, urec.Username, h)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	return outEnv, nil
}

// TerraformInit runs `terraform init` in repo_path/terraform_working_relpath.
// Optional env_file is loaded as KEY=value lines into the command environment.
// Optional run_as: when the agent is root, terraform runs as that POSIX login (see debian.script.run).
func TerraformInit(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	workDir, err := terraformWorkDir(payload)
	if err != nil {
		return nil, err
	}
	env, err := mergeEnvFile(os.Environ(), stringField(payload, "env_file"))
	if err != nil {
		return nil, err
	}
	args := []string{"init", "-input=false", "-no-color"}
	if boolField(payload, "upgrade") {
		args = append(args, "-upgrade")
	}
	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = workDir
	env, err = configureExecCmdRunAs(cmd, env, stringField(payload, "run_as"))
	if err != nil {
		return nil, err
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			return nil, fmt.Errorf("terraform init: %w", err)
		}
	}
	data := map[string]interface{}{
		"ok":          exit == 0,
		"exit_code":   exit,
		"log_tail":    tailString(string(out), 8000),
		"working_dir": workDir,
	}
	if exit != 0 {
		return data, fmt.Errorf("terraform init failed with exit code %d", exit)
	}
	return data, nil
}

// TerraformPlan runs `terraform plan` with optional -detailed-exitcode (Terraform exit: 0 empty, 1 error, 2 changes).
// Optional run_as: when the agent is root, terraform runs as that POSIX login (see debian.script.run).
func TerraformPlan(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	workDir, err := terraformWorkDir(payload)
	if err != nil {
		return nil, err
	}
	env, err := mergeEnvFile(os.Environ(), stringField(payload, "env_file"))
	if err != nil {
		return nil, err
	}
	args := []string{"plan", "-input=false", "-no-color"}
	if boolField(payload, "detailed_exitcode") {
		args = append(args, "-detailed-exitcode")
	}
	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = workDir
	env, err = configureExecCmdRunAs(cmd, env, stringField(payload, "run_as"))
	if err != nil {
		return nil, err
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			return nil, fmt.Errorf("terraform plan: %w", err)
		}
	}
	data := map[string]interface{}{
		"ok":                exit == 0 || exit == 2,
		"exit_code":         exit,
		"detailed_exitcode": boolField(payload, "detailed_exitcode"),
		"log_tail":          tailString(string(out), 12000),
		"working_dir":       workDir,
	}
	if exit != 0 && exit != 2 {
		return data, fmt.Errorf("terraform plan failed with exit code %d", exit)
	}
	return data, nil
}

func terraformWorkDir(payload map[string]interface{}) (string, error) {
	repo := strings.TrimSpace(stringField(payload, "repo_path"))
	if repo == "" {
		return "", fmt.Errorf("repo_path is required")
	}
	rel := strings.TrimSpace(stringField(payload, "terraform_working_relpath"))
	if rel == "" || rel == "." {
		return filepath.Clean(repo), nil
	}
	if strings.Contains(rel, "..") {
		return "", fmt.Errorf("terraform_working_relpath must not contain '..'")
	}
	return filepath.Clean(filepath.Join(repo, rel)), nil
}

func runIDFromPayload(payload map[string]interface{}) string {
	if s := strings.TrimSpace(stringField(payload, "use_case_run_id")); s != "" {
		return s
	}
	return strings.TrimSpace(stringField(payload, "fluid_use_case_run_id"))
}

func stringField(payload map[string]interface{}, key string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

func boolField(payload map[string]interface{}, key string) bool {
	switch v := payload[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	case float64:
		return v != 0
	default:
		return false
	}
}

func sanitizePathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "run"
	}
	out := b.String()
	if len(out) > 128 {
		return out[:128]
	}
	return out
}

func mergeEnvFile(base []string, path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return base, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open env_file %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" {
			continue
		}
		v = strings.Trim(v, `"'`)
		base = append(base, k+"="+v)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return base, nil
}

func tailString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

func writeWorkspaceContextFile(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run_context: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return fmt.Errorf("write context file %s: %w", path, err)
	}
	return nil
}
