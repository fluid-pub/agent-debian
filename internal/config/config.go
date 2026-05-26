package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Agent        AgentConfig        `yaml:"agent"`
	Debian       DebianConfig        `yaml:"debian"`
	Controlplane ControlplaneConfig `yaml:"controlplane"`
	Logs         LogsConfig         `yaml:"logs"`
	Skills       SkillsConfig       `yaml:"skills"`
}

type AgentConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Mode    string `yaml:"mode"`
}

type DebianConfig struct {
	DistroFamily             string   `yaml:"distro_family"`
	ScriptAllowedEnvPrefixes []string `yaml:"script_allowed_env_prefixes"`
	DefaultTimeoutSeconds    int      `yaml:"default_timeout_seconds"`
}

type ControlplaneConfig struct {
	WebSocketURL     string `yaml:"websocket_url"`
	OrganizationUUID string `yaml:"organization_uuid"`
	Token            string `yaml:"token"`
}

type LogsConfig struct {
	Enabled   *bool  `yaml:"enabled"`
	Verbosity string `yaml:"verbosity"`
}

type SkillsConfig struct {
	Allowed     []string               `yaml:"allowed"`
	Definitions map[string]interface{} `yaml:"definitions,omitempty"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	cfg.resolveEnvVars()
	cfg.applyDefaults()
	// Do not call Validate here: first-boot enrollment only has WebSocket URL + enrollment
	// secrets in the environment; organization_uuid and connection token appear after enroll.
	// Callers (e.g. cmd/main) run Validate after merge credentials and optional enrollment.
	return &cfg, nil
}

func (c *Config) resolveEnvVars() {
	c.Controlplane.WebSocketURL = resolve(c.Controlplane.WebSocketURL)
	c.Controlplane.OrganizationUUID = resolve(c.Controlplane.OrganizationUUID)
	c.Controlplane.Token = resolve(c.Controlplane.Token)

	for i := range c.Debian.ScriptAllowedEnvPrefixes {
		c.Debian.ScriptAllowedEnvPrefixes[i] = resolve(c.Debian.ScriptAllowedEnvPrefixes[i])
	}
	c.Debian.DistroFamily = resolve(c.Debian.DistroFamily)

	c.resolveControlplaneFromRegistrationURL()
}

func (c *Config) applyDefaults() {
	if c.Agent.Name == "" {
		c.Agent.Name = "debian-exec"
	}
	if c.Agent.Version == "" {
		c.Agent.Version = "1.0.0"
	}
	if c.Agent.Mode == "" {
		c.Agent.Mode = "execution"
	}
	if c.Debian.DistroFamily == "" {
		c.Debian.DistroFamily = "debian"
	}
	if len(c.Debian.ScriptAllowedEnvPrefixes) == 0 {
		c.Debian.ScriptAllowedEnvPrefixes = []string{"FLUID_"}
	}
	if c.Debian.DefaultTimeoutSeconds <= 0 {
		c.Debian.DefaultTimeoutSeconds = 600
	}
	if c.Logs.Enabled == nil {
		v := true
		c.Logs.Enabled = &v
	}
	if strings.TrimSpace(c.Logs.Verbosity) == "" {
		c.Logs.Verbosity = "normal"
	}
}

func (c *Config) resolveControlplaneFromRegistrationURL() {
	if c.Controlplane.WebSocketURL == "" {
		return
	}

	u, err := url.Parse(c.Controlplane.WebSocketURL)
	if err != nil {
		return
	}

	q := u.Query()
	if c.Controlplane.OrganizationUUID == "" {
		c.Controlplane.OrganizationUUID = q.Get("organization_uuid")
	}
	if c.Controlplane.Token == "" {
		c.Controlplane.Token = q.Get("token")
	}

	if q.Has("organization_uuid") || q.Has("token") {
		q.Del("organization_uuid")
		q.Del("token")
		u.RawQuery = q.Encode()
		c.Controlplane.WebSocketURL = u.String()
	}
}

func (c *Config) Validate() error {
	if c.Agent.Mode != "execution" {
		return fmt.Errorf("agent.mode must be execution")
	}
	if c.Controlplane.WebSocketURL == "" || c.Controlplane.OrganizationUUID == "" || c.Controlplane.Token == "" {
		return fmt.Errorf("controlplane websocket_url, organization_uuid and token are required")
	}
	if !strings.EqualFold(strings.TrimSpace(c.Debian.DistroFamily), "debian") {
		return fmt.Errorf("debian.distro_family must be debian for now")
	}
	if len(c.Skills.Allowed) == 0 {
		return fmt.Errorf("skills.allowed must contain at least one skill")
	}

	seen := map[string]struct{}{}
	for i, p := range c.Debian.ScriptAllowedEnvPrefixes {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		if !strings.HasSuffix(v, "_") {
			return fmt.Errorf("debian.script_allowed_env_prefixes[%d] must end with underscore", i)
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
	}
	if c.Debian.DefaultTimeoutSeconds <= 0 {
		return fmt.Errorf("debian.default_timeout_seconds must be positive")
	}
	return nil
}

func resolve(v string) string {
	if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
		key := strings.TrimSuffix(strings.TrimPrefix(v, "${"), "}")
		return os.Getenv(key)
	}
	return v
}
