package skills

import (
	"context"
	"syscall"
	"testing"

	agentexec "fluid/agents/debian/internal/exec"
)

type fakeRunner struct {
	lastCommand string
	lastArgs    []string
	lastEnv     []string
	result      agentexec.Result
	err         error
}

func (f *fakeRunner) Run(ctx context.Context, command string, args []string, env []string, wd string) (agentexec.Result, error) {
	return f.RunWithCred(ctx, command, args, env, wd, nil)
}

func (f *fakeRunner) RunWithCred(_ context.Context, command string, args []string, env []string, _ string, _ *syscall.Credential) (agentexec.Result, error) {
	f.lastCommand = command
	f.lastArgs = append([]string{}, args...)
	f.lastEnv = append([]string{}, env...)
	return f.result, f.err
}

func TestPkgInstallBuildsAptCommand(t *testing.T) {
	r := &fakeRunner{result: agentexec.Result{ExitCode: 0}}
	s := NewPkgSkills(r)
	_, err := s.Install(context.Background(), map[string]interface{}{
		"packages":        []interface{}{"jq", "curl"},
		"allow_downgrade": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.lastCommand != "apt-get" {
		t.Fatalf("expected apt-get, got %s", r.lastCommand)
	}
	if len(r.lastArgs) < 5 || r.lastArgs[0] != "install" || r.lastArgs[1] != "-y" {
		t.Fatalf("unexpected args: %#v", r.lastArgs)
	}
}

func TestPkgInstallRequiresPackages(t *testing.T) {
	r := &fakeRunner{}
	s := NewPkgSkills(r)
	_, err := s.Install(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestPkgUpgradeRejectsInvalidMode(t *testing.T) {
	r := &fakeRunner{}
	s := NewPkgSkills(r)
	_, err := s.Upgrade(context.Background(), map[string]interface{}{
		"mode": "weird",
	})
	if err == nil {
		t.Fatalf("expected invalid mode error")
	}
}
