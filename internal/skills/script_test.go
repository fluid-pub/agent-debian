package skills

import (
	"context"
	"testing"

	agentexec "fluid/agents/debian/internal/exec"
)

func TestScriptRunRejectsDisallowedEnv(t *testing.T) {
	r := &fakeRunner{result: agentexec.Result{ExitCode: 0}}
	s := NewScriptSkills(r, []string{"FLUID_"})

	_, err := s.Run(context.Background(), map[string]interface{}{
		"script": "echo ok",
		"env": map[string]interface{}{
			"AWS_REGION": "ca-central-1",
		},
	})
	if err == nil {
		t.Fatalf("expected env rejection")
	}
}

func TestScriptRunAcceptsAllowedEnv(t *testing.T) {
	r := &fakeRunner{result: agentexec.Result{ExitCode: 0}}
	s := NewScriptSkills(r, []string{"FLUID_", "AWS_"})

	out, err := s.Run(context.Background(), map[string]interface{}{
		"script": "echo ok",
		"env": map[string]interface{}{
			"AWS_REGION": "ca-central-1",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed, _ := out["changed"].(bool); !changed {
		t.Fatalf("expected changed=true")
	}
}

func TestScriptRunRunAsUnknownUser(t *testing.T) {
	r := &fakeRunner{result: agentexec.Result{ExitCode: 0}}
	s := NewScriptSkills(r, []string{"FLUID_"})

	_, err := s.Run(context.Background(), map[string]interface{}{
		"script": "echo ok",
		"run_as": "no_such_user_fluid_test_xxxxx",
	})
	if err == nil {
		t.Fatalf("expected error for unknown run_as user")
	}
}
