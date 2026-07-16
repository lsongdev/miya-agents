package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsongdev/miya-agents/mcp"
)

type McpServerConfig = mcp.McpServerConfig

// Config is the root configuration structure.
type Config struct {
	Agents     []ACPAgentConfig            `json:"agents,omitempty" yaml:"agents,omitempty"`
	Profiles   map[string]*ProfileConfig   `json:"profiles" yaml:"profiles"`
	Providers  map[string]*ProviderConfig  `json:"providers" yaml:"providers"` // Provider configurations
	McpServers map[string]*McpServerConfig `json:"mcpServers,omitempty"`
	Channels   map[string]any              `json:"channels,omitempty" yaml:"channels,omitempty"`
	// ChannelsEnabled controls whether desktop should run the remote channel gateway.
	ChannelsEnabled *bool                      `json:"channelsEnabled,omitempty" yaml:"channelsEnabled,omitempty"`
	Tools           map[string]json.RawMessage `json:"tools,omitempty" yaml:"tools,omitempty"`
	Logging         LoggingConfig              `json:"logging,omitempty" yaml:"logging,omitempty"`
}

// ACPAgentConfig contains an externally callable ACP agent endpoint.
type ACPAgentConfig struct {
	ID      string            `json:"id" yaml:"id"`
	Name    string            `json:"name,omitempty" yaml:"name,omitempty"`
	Enabled *bool             `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Type    string            `json:"type,omitempty" yaml:"type,omitempty"` // builtin, stdio (default), http, or sse
	Command string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []string          `json:"args,omitempty" yaml:"args,omitempty"`
	URL     string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

func (a ACPAgentConfig) IsEnabled() bool {
	return a.Enabled == nil || *a.Enabled
}

func (c *Config) UnmarshalJSON(data []byte) error {
	var probe struct {
		Agents json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if len(probe.Agents) > 0 {
		var v any
		if err := json.Unmarshal(probe.Agents, &v); err != nil {
			return err
		}
		if _, ok := v.(map[string]any); ok {
			return fmt.Errorf("config field agents must be an array of ACP endpoints; move old agents object to profiles")
		}
	}

	type alias Config
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = Config(decoded)
	return nil
}

// ProviderConfig contains API credentials for a providers.
type ProviderConfig struct {
	APIKey  string `json:"apiKey" yaml:"apiKey"`
	APIBase string `json:"apiBase,omitempty" yaml:"apiBase,omitempty"` // optional custom base URL
	Type    string `json:"type,omitempty" yaml:"type,omitempty"`       // "openai" (default) or "anthropic"
}

// ProfileConfig contains miya-agents runtime defaults.
type ProfileConfig struct {
	Provider            string  `json:"provider" yaml:"provider"`                                           // provider name, e.g. "openai"
	ModelName           string  `json:"model,omitempty" yaml:"model"`                                       // model name, e.g. "deepseek-chat"
	Workspace           string  `json:"workspace,omitempty" yaml:"workspace,omitempty"`                     // defaults to ~/.miya/workspace
	MaxTokens           int     `json:"maxTokens,omitempty" yaml:"maxTokens,omitempty"`                     // defaults to 8192
	Temperature         float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`                 // defaults to 0.95
	ContextWindowTokens int     `json:"contextWindowTokens,omitempty" yaml:"contextWindowTokens,omitempty"` // defaults to 128000
	ContextWarnRatio    float64 `json:"contextWarnRatio,omitempty" yaml:"contextWarnRatio,omitempty"`       // defaults to 0.9
}

func (ac *ProfileConfig) GetWorkspace() string {
	workspace := expandPath(ac.Workspace)
	if workspace != "" {
		return workspace
	}
	return filepath.Join(ConfigPath, "workspace")
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
