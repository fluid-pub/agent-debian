package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigExtractsCPFromWebsocketURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yml")
	content := `
agent:
  name: "debian-exec"
  version: "1.0.0"
  mode: "execution"
debian:
  script_allowed_env_prefixes:
    - "FLUID_"
controlplane:
  websocket_url: "wss://dev.fluid.pub/v1/agents/websocket?organization_uuid=org-uuid&token=tok"
skills:
  allowed:
    - debian.script.run
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Controlplane.OrganizationUUID != "org-uuid" || cfg.Controlplane.Token != "tok" {
		t.Fatalf("expected cp credentials from URL, got %q / %q", cfg.Controlplane.OrganizationUUID, cfg.Controlplane.Token)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate after full URL credentials: %v", err)
	}
}

func TestLoadConfigSkipsFullValidateForEnrollmentBootstrap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yml")
	content := `
agent:
  name: "debian-exec"
  version: "1.0.0"
  mode: "execution"
debian:
  script_allowed_env_prefixes:
    - "FLUID_"
controlplane:
  websocket_url: "${FLUID_CONTROLPLANE_WEBSOCKET_URL}"
  organization_uuid: "${FLUID_CONTROLPLANE_ORGANIZATION_UUID}"
  token: "${FLUID_CONTROLPLANE_TOKEN}"
skills:
  allowed:
    - debian.script.run
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLUID_CONTROLPLANE_WEBSOCKET_URL", "wss://cp.example/v1/agents/websocket")
	t.Setenv("FLUID_CONTROLPLANE_ORGANIZATION_UUID", "")
	t.Setenv("FLUID_CONTROLPLANE_TOKEN", "")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig should succeed before enrollment (no org/token yet): %v", err)
	}
	if cfg.Controlplane.WebSocketURL == "" {
		t.Fatal("expected websocket URL from env")
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate should still fail until org and token exist")
	}
}
