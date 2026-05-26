package agent

import (
	"testing"

	"fluid/agents/debian/internal/config"
)

func TestRuntimeConfigForControlPlane(t *testing.T) {
	cfg := &config.Config{
		Agent: config.AgentConfig{
			Name:    "debian-exec",
			Version: "1.0.0",
			Mode:    "execution",
		},
		Skills: config.SkillsConfig{
			Allowed: []string{"debian.pkg.install", "debian.script.run"},
		},
	}
	out := runtimeConfigForControlPlane(cfg)
	if out == nil {
		t.Fatalf("expected runtime config")
	}
}
