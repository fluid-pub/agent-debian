package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeCredentialsFromFile(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, "credentials.yaml")
	content := `
controlplane:
  websocket_url: "ws://cp/ws"
  organization_uuid: "org-1"
  token: "conn-tok"
`
	if err := os.WriteFile(credPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Agent: AgentConfig{Name: "debian-exec", Version: "1", Mode: "execution"},
		Debian: DebianConfig{DistroFamily: "debian", ScriptAllowedEnvPrefixes: []string{"FLUID_"}, DefaultTimeoutSeconds: 600},
		Controlplane: ControlplaneConfig{
			WebSocketURL:     "",
			OrganizationUUID: "",
			Token:            "",
		},
		Skills: SkillsConfig{Allowed: []string{"debian.script.run"}},
	}
	if err := MergeCredentialsFromFile(cfg, credPath); err != nil {
		t.Fatal(err)
	}
	if cfg.Controlplane.WebSocketURL != "ws://cp/ws" || cfg.Controlplane.OrganizationUUID != "org-1" || cfg.Controlplane.Token != "conn-tok" {
		t.Fatalf("unexpected merge: %+v", cfg.Controlplane)
	}
}

func TestWriteCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.yaml")
	cp := ControlplaneConfig{
		WebSocketURL:     "ws://x",
		OrganizationUUID: "u",
		Token:            "t",
	}
	if err := WriteCredentialsFile(path, cp); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Controlplane: ControlplaneConfig{}}
	if err := MergeCredentialsFromFile(cfg, path); err != nil {
		t.Fatal(err)
	}
	if cfg.Controlplane != cp {
		t.Fatalf("round-trip: %+v", cfg.Controlplane)
	}
}
