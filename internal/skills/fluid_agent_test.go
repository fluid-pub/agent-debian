package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyELFAcceptsBinary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fakebin")
	if err := os.WriteFile(p, []byte{0x7f, 'E', 'L', 'F', 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyELF(p); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyELFRejectsNonELF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "html")
	if err := os.WriteFile(p, []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyELF(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureBootstrapPathsCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "aws", "agent.yml")
	cred := filepath.Join(dir, "aws", "credentials.yaml")
	env := filepath.Join(dir, "aws", "enrollment.env")
	if err := ensureBootstrapPaths(cfg, cred, env); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Dir(cfg), filepath.Dir(cred), filepath.Dir(env)} {
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			t.Fatalf("missing dir %q: %v", p, err)
		}
	}
}

func TestWriteAgentConfigFromPayload_writesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yml")
	if err := writeAgentConfigFromPayload(cfg, map[string]interface{}{
		"agent_config_yaml": "a: 1",
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfg)
	if !strings.Contains(string(raw), "a: 1") {
		t.Fatalf("expected a: 1, got %s", raw)
	}
	if err := writeAgentConfigFromPayload(cfg, map[string]interface{}{
		"agent_config_yaml": "a: 2",
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(cfg)
	if !strings.Contains(string(raw), "a: 2") {
		t.Fatalf("expected overwrite a: 2, got %s", raw)
	}
}

func TestWriteAgentConfigFromPayload_missingFileWithoutYAML(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yml")
	if err := writeAgentConfigFromPayload(cfg, map[string]interface{}{}); err == nil {
		t.Fatal("expected error")
	} else if !strings.Contains(err.Error(), "agent_config_yaml is required") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestWriteAgentConfigFromPayload_emptyYAMLWithExistingFile(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yml")
	_ = os.WriteFile(cfg, []byte("ok: true\n"), 0o644)
	if err := writeAgentConfigFromPayload(cfg, map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), "ok: true") {
		t.Fatalf("file should be unchanged, got %s", b)
	}
}

func TestWriteAgentConfigFromPayload_rejectsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yml")
	if err := writeAgentConfigFromPayload(cfg, map[string]interface{}{
		"agent_config_yaml": "[unclosed",
	}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAgentConfigYAMLString_rejectsNonString(t *testing.T) {
	_, err := agentConfigYAMLString(map[string]interface{}{"agent_config_yaml": 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInheritedParentEnviron_Whitelist(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"FLUID_SERVICE_CREDENTIALS=local",
		"FLUID_FOO=bar",
		"HOME=/root",
		"AWS_REGION=us-east-1",
		"LC_ALL=C.UTF-8",
		"SECRET=1",
		"http_proxy=http://p:1",
	}
	out := inheritedParentEnviron(in)
	if len(out) != 4 {
		t.Fatalf("len got %d want 4: %v", len(out), out)
	}
	got := make(map[string]string, len(out))
	for _, e := range out {
		k := environmentEntryName(e)
		got[k] = e
	}
	if got["PATH"] != "PATH=/usr/bin" {
		t.Fatalf("PATH: %v", got)
	}
	if got["HOME"] != "HOME=/root" {
		t.Fatalf("HOME: %v", got)
	}
	if got["LC_ALL"] != "LC_ALL=C.UTF-8" {
		t.Fatalf("LC_ALL: %v", got)
	}
	if got["http_proxy"] != "http_proxy=http://p:1" {
		t.Fatalf("http_proxy: %v", got)
	}
}
