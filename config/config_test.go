package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
