// Package runtime assembles agents from configuration.
package runtime

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lsongdev/miya-agents/agent"
	"github.com/lsongdev/miya-agents/anthropic"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/session"
	"github.com/lsongdev/miya-agents/tools"
)

const maxDelegateDepth = 4

type delegateDepthKey struct{}

// Runtime builds agents from configuration: it resolves a profile to a
// model, a system prompt and a tool set, and lets agents delegate to one
// another.
type Runtime struct {
	config *config.Config
}

func New(config *config.Config) *Runtime {
	return &Runtime{config: config}
}

// Agent builds the agent described by the named profile.
func (r *Runtime) Agent(name string) (*agent.Agent, error) {
	profile, ok := r.config.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", name)
	}
	provider, ok := r.config.Providers[profile.Provider]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", profile.Provider)
	}

	var model llm.Model
	switch strings.ToLower(strings.TrimSpace(provider.Type)) {
	case "", "openai":
		client, err := openai.NewClient(&openai.Configuration{API: provider.APIBase, APIKey: provider.APIKey})
		if err != nil {
			return nil, err
		}
		model = llm.OpenAI(client)
	case "anthropic":
		model = llm.Anthropic(anthropic.NewClient(&anthropic.Configuration{API: provider.APIBase, APIKey: provider.APIKey}))
	default:
		return nil, fmt.Errorf("unsupported provider type %q", provider.Type)
	}

	agentTools, err := r.toolsFor(profile)
	if err != nil {
		return nil, fmt.Errorf("agent %q tools: %w", name, err)
	}
	return &agent.Agent{
		Name:          name,
		Model:         model,
		ModelName:     profile.ModelName,
		System:        systemPrompt(profile.GetWorkspace()),
		MaxTokens:     profile.MaxTokens,
		ContextWindow: r.ContextWindow(name),
		Compaction: agent.CompactOptions{
			Threshold: profile.ContextWarnRatio,
			Prompt:    readWorkspaceFile(profile.GetWorkspace(), "COMPACT.md"),
		},
		Tools: agentTools,
	}, nil
}

// systemPrompt reads the agent's instructions from AGENTS.md in its workspace.
func systemPrompt(workspace string) string {
	return cmp.Or(readWorkspaceFile(workspace, "AGENTS.md"), "You are a helpful assistant.")
}

// readWorkspaceFile returns the content of an optional file in the agent's
// workspace, or "" if it is missing.
func readWorkspaceFile(workspace, name string) string {
	if workspace == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(workspace, name))
	if err != nil {
		return ""
	}
	return string(data)
}

func (r *Runtime) toolsFor(profile *config.ProfileConfig) ([]agent.Tool, error) {
	workspace := profile.GetWorkspace()
	if workspace != "" {
		_ = os.MkdirAll(workspace, 0755)
	}

	available := []agent.Tool{
		&tools.WebFetchTool{},
		&tools.WebSearchTool{},
		&tools.ReadFileTool{Workspace: workspace},
		&tools.WriteFileTool{Workspace: workspace},
		&tools.AppendFileTool{Workspace: workspace},
		&tools.EditFileTool{Workspace: workspace},
		&tools.AttachFileTool{Workspace: workspace},
		&tools.ExecTool{
			Workspace:           workspace,
			DefaultTimeout:      tools.ExecDefaultTimeoutSeconds,
			RestrictToWorkspace: true,
		},
		&tools.SkillsTool{Workspace: filepath.Join(config.ConfigPath, "skills")},
		tools.NewDelegateTool(r.Delegate, r.agentDescriptions()),
	}

	needsMCP := len(profile.Tools) == 0
	if !needsMCP {
		for _, name := range profile.Tools {
			if strings.HasPrefix(name, "mcp_") {
				needsMCP = true
				break
			}
		}
	}
	if needsMCP {
		mcpManager := tools.NewMcpManager(r.config.McpServers)
		for _, tool := range mcpManager.Tools {
			available = append(available, tool)
		}
	}
	if len(profile.Tools) == 0 {
		return available, nil
	}

	byName := make(map[string]agent.Tool, len(available))
	for _, tool := range available {
		byName[tool.Def().Name] = tool
	}
	selected := make([]agent.Tool, 0, len(profile.Tools))
	for _, name := range profile.Tools {
		tool, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown tool %q", name)
		}
		selected = append(selected, tool)
	}
	return selected, nil
}

func (r *Runtime) agentDescriptions() map[string]string {
	agents := make(map[string]string, len(r.config.Profiles))
	for name, profile := range r.config.Profiles {
		agents[name] = profile.Description
	}
	return agents
}

// HasProfile reports whether a profile with this name is configured.
func (r *Runtime) HasProfile(name string) bool {
	_, ok := r.config.Profiles[name]
	return ok
}

// DefaultAgent returns the profile used when none is named: "default" if it
// exists, otherwise the first by name.
func (r *Runtime) DefaultAgent() (string, error) {
	if _, ok := r.config.Profiles["default"]; ok {
		return "default", nil
	}
	names := make([]string, 0, len(r.config.Profiles))
	for name := range r.config.Profiles {
		names = append(names, name)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no profiles configured")
	}
	sort.Strings(names)
	return names[0], nil
}

// ResolveAgent returns name if it is a configured profile, and the default
// agent otherwise.
func (r *Runtime) ResolveAgent(name string) (string, error) {
	if name != "" {
		if _, ok := r.config.Profiles[name]; ok {
			return name, nil
		}
	}
	return r.DefaultAgent()
}

// Delegate runs an agent with an isolated, in-memory session and returns only
// its final output. Delegated agents are otherwise normal profile-backed agents.
func (r *Runtime) Delegate(ctx context.Context, name, task string) (string, error) {
	depth, _ := ctx.Value(delegateDepthKey{}).(int)
	if depth >= maxDelegateDepth {
		return "", fmt.Errorf("maximum delegation depth %d reached", maxDelegateDepth)
	}
	ag, err := r.Agent(name)
	if err != nil {
		return "", err
	}

	sess := session.New(ag.Name)
	_ = sess.AddUser(task) // an in-memory session cannot fail to add
	// Fallback output for a run that ends without a final text message.
	var output strings.Builder
	capture := func(event agent.Event) error {
		switch {
		case event.Type == agent.EventText:
			output.WriteString(event.Text)
		case event.Type == agent.EventFile && event.File.URL != "":
			output.WriteString(event.File.URL)
		case event.Type == agent.EventFile:
			output.WriteString(event.File.Name)
		}
		return nil
	}
	ctx = context.WithValue(ctx, delegateDepthKey{}, depth+1)
	if err := ag.Run(ctx, sess, capture); err != nil {
		return "", fmt.Errorf("agent %q failed: %w", name, err)
	}
	messages := sess.Context()
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if text := message.Text(); message.Role == llm.RoleAssistant && len(message.ToolCalls()) == 0 && text != "" {
			return text, nil
		}
	}
	return output.String(), nil
}

// ContextWindow returns the context size configured for a profile.
func (r *Runtime) ContextWindow(profile string) int {
	// 128000 is the documented default of contextWindowTokens.
	const fallback = 128000
	if p, ok := r.config.Profiles[profile]; ok {
		return cmp.Or(p.ContextWindowTokens, fallback)
	}
	return fallback
}
