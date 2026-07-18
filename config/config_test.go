package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentEndpointsDerivesProfilesBeforeExternalAgents(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]*ProfileConfig{
			"coding":  {},
			"default": {},
		},
		Agents: []ACPAgentConfig{
			{ID: "opencode", Type: "stdio", Command: "opencode"},
			{ID: "old-builtin", Type: "builtin", Profile: "default"},
		},
	}
	endpoints, err := AgentEndpoints(cfg)
	if err != nil {
		t.Fatalf("AgentEndpoints: %v", err)
	}
	if len(endpoints) != 3 {
		t.Fatalf("endpoints = %d, want 3", len(endpoints))
	}
	if endpoints[0].ID != "default" || endpoints[0].Profile != "default" || endpoints[0].Type != "builtin" {
		t.Fatalf("default endpoint = %#v", endpoints[0])
	}
	if endpoints[1].ID != "coding" || endpoints[2].ID != "opencode" {
		t.Fatalf("endpoint order = %#v", endpoints)
	}
}

func TestAgentEndpointsRejectsExternalProfileIDConflict(t *testing.T) {
	_, err := AgentEndpoints(&Config{
		Profiles: map[string]*ProfileConfig{"default": {}},
		Agents:   []ACPAgentConfig{{ID: "default", Type: "stdio", Command: "other"}},
	})
	if err == nil {
		t.Fatal("AgentEndpoints succeeded with conflicting ids")
	}
}

func TestAgentProfileBindingIsRuntimeOnly(t *testing.T) {
	data, err := json.Marshal(ACPAgentConfig{ID: "default", Type: "builtin", Profile: "default"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != `{"id":"default","type":"builtin"}` {
		t.Fatalf("json = %s", data)
	}
}

func TestProfileGetWorkspaceDefaultsToMiyaWorkspace(t *testing.T) {
	old := ConfigPath
	ConfigPath = filepath.Join(t.TempDir(), ".miya")
	t.Cleanup(func() {
		ConfigPath = old
	})

	profile := &ProfileConfig{}

	if got, want := profile.GetWorkspace(), filepath.Join(ConfigPath, "workspace"); got != want {
		t.Fatalf("workspace = %q, want %q", got, want)
	}
}

func TestLoadConfigFromEmptyFileReturnsNormalizedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatalf("write empty config: %v", err)
	}

	cfg, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFromFile: %v", err)
	}
	if cfg == nil || cfg.Profiles == nil || cfg.Providers == nil || cfg.McpServers == nil || cfg.Channels == nil {
		t.Fatalf("config was not normalized: %#v", cfg)
	}
}

func TestSaveConfigToFileNormalizesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	cfg := &Config{
		McpServers: map[string]*McpServerConfig{
			"remote": {URL: "https://example.com/sse"},
		},
	}

	if err := SaveConfigToFile(path, cfg); err != nil {
		t.Fatalf("SaveConfigToFile: %v", err)
	}
	loaded, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFromFile: %v", err)
	}
	if loaded.McpServers["remote"].Type != "sse" {
		t.Fatalf("mcp server type = %q, want sse", loaded.McpServers["remote"].Type)
	}
}

func TestConfigRejectsChannelsObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"channels":{"telegram":{"token":"old"}}}`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfigFromFile(path); err == nil {
		t.Fatal("expected channels object to be rejected")
	}
}
