package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsongdev/miya-agents/mcp"
)

// Config is the root configuration structure.
type Config struct {
	Agents     map[string]*AgentConfig         `json:"agents" yaml:"agents"`
	Providers  map[string]*ProviderConfig      `json:"providers" yaml:"providers"` // Provider configurations
	McpServers map[string]*mcp.McpServerConfig `json:"mcpServers,omitempty"`
	// Channels   map[string]json.RawMessage      `json:"channels,omitempty" yaml:"channels,omitempty"`
	Tools   map[string]json.RawMessage `json:"tools,omitempty" yaml:"tools,omitempty"`
	Logging LoggingConfig              `json:"logging,omitempty" yaml:"logging,omitempty"`
}

// ProviderConfig contains API credentials for a providers.
type ProviderConfig struct {
	APIKey  string `json:"apiKey" yaml:"apiKey"`
	APIBase string `json:"apiBase,omitempty" yaml:"apiBase,omitempty"` // optional custom base URL
	Type    string `json:"type,omitempty" yaml:"type,omitempty"`       // "openai" (default) or "anthropic"
}

// ThreadConfig contains thread runtime defaults.
type AgentConfig struct {
	Provider            string  `json:"provider" yaml:"provider"`                                           // provider name, e.g. "openai"
	ModelName           string  `json:"model,omitempty" yaml:"model"`                                       // model name, e.g. "deepseek-chat"
	Workspace           string  `json:"workspace,omitempty" yaml:"workspace,omitempty"`                     // defaults to ~/.miya/workspace
	MaxTokens           int     `json:"maxTokens,omitempty" yaml:"maxTokens,omitempty"`                     // defaults to 8192
	Temperature         float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`                 // defaults to 0.95
	ContextWindowTokens int     `json:"contextWindowTokens,omitempty" yaml:"contextWindowTokens,omitempty"` // defaults to 128000
	ContextWarnRatio    float64 `json:"contextWarnRatio,omitempty" yaml:"contextWarnRatio,omitempty"`       // defaults to 0.8
}

func (ac *AgentConfig) GetWorkspace() string {
	return expandPath(ac.Workspace)
}

// LoggingConfig contains logging configuration.
type LoggingConfig struct {
	Enabled bool   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Level   string `json:"level,omitempty" yaml:"level,omitempty"`   // debug, info, warn, error
	Stdout  bool   `json:"stdout,omitempty" yaml:"stdout,omitempty"` // log to stdout
	File    string `json:"file,omitempty" yaml:"file,omitempty"`     // log file path
}

var ConfigPath = filepath.Join(os.Getenv("HOME"), ".miya")
var ConfigFile = filepath.Join(ConfigPath, "config.json")

func LoadConfig() (cfg *Config, err error) {
	if _, err := os.Stat(ConfigFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", ConfigFile)
	}
	f, err := os.Open(ConfigFile)
	if err != nil {
		return
	}
	defer f.Close()
	if err = json.NewDecoder(f).Decode(&cfg); err != nil {
		return
	}
	return
}

// expandPath expands ~ to home directory and resolves the path.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	return path
}

func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// log.Println(string(data))
	return os.WriteFile(ConfigFile, data, 0644)
}
