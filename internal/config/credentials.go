package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type credentialsFile struct {
	Controlplane ControlplaneConfig `yaml:"controlplane"`
}

// MergeCredentialsFromFile loads /etc/fluid/debian/credentials.yaml-style overlay.
// If the file does not exist, it returns nil. Non-empty fields override cfg.Controlplane.
func MergeCredentialsFromFile(cfg *Config, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read credentials file: %w", err)
	}
	var f credentialsFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("decode credentials file: %w", err)
	}
	cp := f.Controlplane
	if strings.TrimSpace(cp.WebSocketURL) != "" {
		cfg.Controlplane.WebSocketURL = strings.TrimSpace(cp.WebSocketURL)
	}
	if strings.TrimSpace(cp.OrganizationUUID) != "" {
		cfg.Controlplane.OrganizationUUID = strings.TrimSpace(cp.OrganizationUUID)
	}
	if strings.TrimSpace(cp.Token) != "" {
		cfg.Controlplane.Token = strings.TrimSpace(cp.Token)
	}
	return nil
}

// WriteCredentialsFile writes durable WebSocket + organization + connection token (0600, atomic replace).
func WriteCredentialsFile(path string, cp ControlplaneConfig) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("credentials path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir credentials dir: %w", err)
	}
	out, err := yaml.Marshal(&credentialsFile{
		Controlplane: ControlplaneConfig{
			WebSocketURL:     strings.TrimSpace(cp.WebSocketURL),
			OrganizationUUID: strings.TrimSpace(cp.OrganizationUUID),
			Token:            strings.TrimSpace(cp.Token),
		},
	})
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".fluid-credentials-*")
	if err != nil {
		return fmt.Errorf("create temp credentials: %w", err)
	}
	tmpName := tmp.Name()
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp credentials: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("sync temp credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp credentials: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace credentials file: %w", err)
	}
	return nil
}
