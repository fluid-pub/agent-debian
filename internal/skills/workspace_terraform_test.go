package skills

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTerraformInit_nonZeroExitReturnsErrorWithData(t *testing.T) {
	repo := t.TempDir()
	installFakeTerraform(t, 1, "init failed")

	out, err := TerraformInit(context.Background(), map[string]interface{}{
		"repo_path": repo,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if out["exit_code"] != 1 {
		t.Fatalf("exit_code want 1 got %#v", out["exit_code"])
	}
	if out["ok"] != false {
		t.Fatalf("ok want false got %#v", out["ok"])
	}
	if !strings.Contains(out["log_tail"].(string), "init failed") {
		t.Fatalf("missing log tail: %#v", out["log_tail"])
	}
}

func TestTerraformPlan_exitOneReturnsErrorWithData(t *testing.T) {
	repo := t.TempDir()
	installFakeTerraform(t, 1, "plan failed")

	out, err := TerraformPlan(context.Background(), map[string]interface{}{
		"repo_path":         repo,
		"detailed_exitcode": true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if out["exit_code"] != 1 {
		t.Fatalf("exit_code want 1 got %#v", out["exit_code"])
	}
	if out["ok"] != false {
		t.Fatalf("ok want false got %#v", out["ok"])
	}
}

func TestTerraformPlan_detailedExitTwoReturnsSuccess(t *testing.T) {
	repo := t.TempDir()
	installFakeTerraform(t, 2, "changes present")

	out, err := TerraformPlan(context.Background(), map[string]interface{}{
		"repo_path":         repo,
		"detailed_exitcode": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["exit_code"] != 2 {
		t.Fatalf("exit_code want 2 got %#v", out["exit_code"])
	}
	if out["ok"] != true {
		t.Fatalf("ok want true got %#v", out["ok"])
	}
}

func installFakeTerraform(t *testing.T, exitCode int, output string) {
	t.Helper()
	binDir := t.TempDir()
	path := filepath.Join(binDir, "terraform")
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %q\nexit %d\n", output, exitCode)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestConfigureExecCmdRunAs_unknownUser(t *testing.T) {
	cmd := exec.Command("true")
	_, err := configureExecCmdRunAs(cmd, []string{"PATH=/bin"}, "no_such_user_fluid_terraform_xxxxx")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "run_as") {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestPrepareWorkspace_createsDirs(t *testing.T) {
	t.Parallel()
	payload := map[string]interface{}{"use_case_run_id": "abc-123-test"}
	out, err := PrepareWorkspace(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	base := out["workspace_path"].(string)
	defer os.RemoveAll(filepath.Dir(base))

	repo := out["repo_path"].(string)
	st, err := os.Stat(repo)
	if err != nil {
		t.Fatalf("repo dir: %v", err)
	}
	if m := st.Mode().Perm(); m&0o005 == 0 {
		t.Fatalf("repo dir should be world-traversable (o+x) for run_as scripts; got mode %o", m)
	}
}

func TestPrepareWorkspace_workspaceOwner_chownsDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix ownership")
	}
	u, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current: %v", err)
	}
	if u.Username == "" {
		t.Skip("no username")
	}
	rid := fmt.Sprintf("owner-spec-%d", time.Now().UnixNano())
	payload := map[string]interface{}{
		"use_case_run_id": rid,
		"workspace_owner": u.Username,
	}
	out, err := PrepareWorkspace(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	base := out["workspace_path"].(string)
	defer os.RemoveAll(filepath.Dir(base))

	repo := out["repo_path"].(string)
	st, err := os.Stat(repo)
	if err != nil {
		t.Fatal(err)
	}
	if m := st.Mode().Perm(); m != 0o750 {
		t.Fatalf("repo perm want 0750 got %o", m)
	}
	secrets := out["secrets_dir"].(string)
	if filepath.Dir(secrets) != filepath.Dir(base) {
		t.Fatalf("secrets should be a run-level sibling of workspace, got %s", secrets)
	}
	stSec, err := os.Stat(secrets)
	if err != nil {
		t.Fatal(err)
	}
	if m := stSec.Mode().Perm(); m != 0o700 {
		t.Fatalf("secrets perm want 0700 got %o", m)
	}
	artifacts := out["artifacts_dir"].(string)
	if filepath.Dir(artifacts) != filepath.Dir(base) {
		t.Fatalf("artifacts should be a run-level sibling of workspace, got %s", artifacts)
	}
	stArt, err := os.Stat(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if m := stArt.Mode().Perm(); m != 0o750 {
		t.Fatalf("artifacts perm want 0750 got %o", m)
	}
	runRoot := filepath.Dir(base)
	stRun, err := os.Stat(runRoot)
	if err != nil {
		t.Fatal(err)
	}
	wantUID, err := strconv.Atoi(u.Uid)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		sys, ok := stRun.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatalf("unexpected stat type %T", stRun.Sys())
		}
		if int(sys.Uid) != wantUID {
			t.Fatalf("run root %s uid want %d got %d", runRoot, wantUID, sys.Uid)
		}
	}
	sysSec, ok := stSec.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("unexpected stat type %T", stSec.Sys())
	}
	if os.Geteuid() == 0 && wantUID != 0 && int(sysSec.Uid) == wantUID {
		t.Fatalf("secrets should remain root-owned, got workspace owner uid %d", wantUID)
	}
}

func TestPrepareWorkspace_writesContextFile(t *testing.T) {
	t.Parallel()
	payload := map[string]interface{}{
		"use_case_run_id": "abc-123-with-context",
		"run_context": map[string]interface{}{
			"vm_name": "dependabot-mr-694",
			"meta": map[string]interface{}{
				"run_id": "run-123",
			},
		},
	}

	out, err := PrepareWorkspace(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}

	base := out["workspace_path"].(string)
	defer os.RemoveAll(filepath.Dir(base))

	contextFile := out["context_file"].(string)
	raw, err := os.ReadFile(contextFile)
	if err != nil {
		t.Fatalf("read context file: %v", err)
	}

	body := string(raw)
	if !strings.Contains(body, "\"vm_name\": \"dependabot-mr-694\"") {
		t.Fatalf("unexpected context file content: %s", body)
	}
}
