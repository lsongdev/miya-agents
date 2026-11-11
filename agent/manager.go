package agent

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/anthropic"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/session"
	"github.com/lsongdev/miya-agents/tools"
)

const maxDelegateDepth = 4

type delegateDepthKey struct{}

type Manager struct {
	config   *config.Config
	sessions map[string]*session.Session
	mu       sync.RWMutex
}

func NewAgentManager(config *config.Config) *Manager {
	return &Manager{
		config:   config,
		sessions: make(map[string]*session.Session),
	}
}

func (m *Manager) UseAgent(name string) (*Agent, error) {
	profile, ok := m.config.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", name)
	}
	provider, ok := m.config.Providers[profile.Provider]
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

	agentTools, err := m.toolsFor(profile)
	if err != nil {
		return nil, fmt.Errorf("agent %q tools: %w", name, err)
	}
	return &Agent{
		Name:          name,
		Model:         model,
		ModelName:     profile.ModelName,
		System:        systemPrompt(profile.GetWorkspace()),
		MaxTokens:     profile.MaxTokens,
		ContextWindow: m.contextWindow(name),
		Tools:         agentTools,
	}, nil
}

// systemPrompt reads the agent's instructions from AGENTS.md in its workspace.
func systemPrompt(workspace string) string {
	const fallback = "You are a helpful assistant."
	if workspace == "" {
		return fallback
	}
	data, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		return fallback
	}
	return string(data)
}

func (m *Manager) toolsFor(profile *config.ProfileConfig) ([]Tool, error) {
	workspace := profile.GetWorkspace()
	if workspace != "" {
		_ = os.MkdirAll(workspace, 0755)
	}

	available := []Tool{
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
		tools.NewDelegateTool(m.Delegate, m.agentDescriptions()),
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
		mcpManager := tools.NewMcpManager(m.config.McpServers)
		for _, tool := range mcpManager.Tools {
			available = append(available, tool)
		}
	}
	if len(profile.Tools) == 0 {
		return available, nil
	}

	byName := make(map[string]Tool, len(available))
	for _, tool := range available {
		byName[tool.Def().Name] = tool
	}
	selected := make([]Tool, 0, len(profile.Tools))
	for _, name := range profile.Tools {
		tool, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown tool %q", name)
		}
		selected = append(selected, tool)
	}
	return selected, nil
}

func (m *Manager) agentDescriptions() map[string]string {
	agents := make(map[string]string, len(m.config.Profiles))
	for name, profile := range m.config.Profiles {
		agents[name] = profile.Description
	}
	return agents
}

func (m *Manager) defaultAgentName() (string, error) {
	if _, ok := m.config.Profiles["default"]; ok {
		return "default", nil
	}
	names := make([]string, 0, len(m.config.Profiles))
	for name := range m.config.Profiles {
		names = append(names, name)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no profiles configured")
	}
	sort.Strings(names)
	return names[0], nil
}

func (m *Manager) resolveAgentName(name string) (string, error) {
	if name != "" {
		if _, ok := m.config.Profiles[name]; ok {
			return name, nil
		}
	}
	return m.defaultAgentName()
}

// Delegate runs an agent with an isolated, in-memory session and returns only
// its final output. Delegated agents are otherwise normal profile-backed agents.
func (m *Manager) Delegate(ctx context.Context, name, task string) (string, error) {
	depth, _ := ctx.Value(delegateDepthKey{}).(int)
	if depth >= maxDelegateDepth {
		return "", fmt.Errorf("maximum delegation depth %d reached", maxDelegateDepth)
	}
	ag, err := m.UseAgent(name)
	if err != nil {
		return "", err
	}

	sess := session.New(ag.Name)
	_ = sess.AddUser(task) // an in-memory session cannot fail to add
	// Fallback output for a run that ends without a final text message.
	var output strings.Builder
	capture := func(event Event) error {
		switch {
		case event.Type == EventText:
			output.WriteString(event.Text)
		case event.Type == EventFile && event.File.URL != "":
			output.WriteString(event.File.URL)
		case event.Type == EventFile:
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

// acp.ServerHandler implementation

func (m *Manager) Initialize(ctx context.Context, req *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	caps := acp.DefaultAgentCapabilities()
	caps.LoadSession = true
	caps.SessionCapabilities = acp.SessionCapabilities{
		List:   &acp.SessionListCapabilities{},
		Resume: &acp.SessionResumeCapabilities{},
		Close:  &acp.SessionCloseCapabilities{},
		Delete: &acp.SessionDeleteCapabilities{},
	}

	return &acp.InitializeResponse{
		ProtocolVersion:   req.ProtocolVersion,
		AgentCapabilities: caps,
		AgentInfo: &acp.Implementation{
			Name:    "miya",
			Version: "0.1.0",
		},
	}, nil
}

func (m *Manager) Authenticate(ctx context.Context, req *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	return &acp.AuthenticateResponse{}, nil
}

func (m *Manager) NewSession(ctx context.Context, req *acp.NewSessionRequest, sender acp.SessionUpdateSender) (*acp.NewSessionResponse, error) {
	requestedProfile, _ := req.Meta[acp.MiyaProfileMetaKey].(string)
	requestedProfile = strings.TrimSpace(requestedProfile)
	var agentName string
	var err error
	if requestedProfile != "" {
		if _, ok := m.config.Profiles[requestedProfile]; !ok {
			return nil, fmt.Errorf("profile not found: %s", requestedProfile)
		}
		agentName = requestedProfile
	} else {
		agentName, err = m.defaultAgentName()
	}
	if err != nil {
		return nil, err
	}

	sess, err := session.Create(agentName)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	m.mu.Lock()
	m.sessions[sess.ID] = sess
	m.mu.Unlock()

	return &acp.NewSessionResponse{
		SessionID: acp.SessionID(sess.ID),
		Meta:      acp.Meta{acp.MiyaProfileMetaKey: agentName},
	}, nil
}

func (m *Manager) Prompt(ctx context.Context, req *acp.PromptRequest, sender acp.SessionUpdateSender) (*acp.PromptResponse, error) {
	sess, err := session.Load(string(req.SessionID))
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	agentName, err := m.resolveAgentName(sess.AgentName)
	if err != nil {
		return nil, err
	}
	if sess.AgentName != agentName {
		sess.AgentName = agentName
	}

	var textParts []string
	for _, block := range req.Prompt {
		if block.Text != "" {
			textParts = append(textParts, block.Text)
		}
	}
	prompt := strings.Join(textParts, "\n")
	if err := sess.AddUser(prompt); err != nil {
		return nil, fmt.Errorf("save prompt: %w", err)
	}

	ag, err := m.UseAgent(agentName)
	if err != nil {
		return nil, fmt.Errorf("use agent: %w", err)
	}

	sink := acpSink(sender)
	if sess.Title == "" {
		if title := sess.DisplayTitle(); title != "" {
			if err := sess.SetTitle(title); err != nil {
				return nil, fmt.Errorf("save title: %w", err)
			}
			if err := sink(Event{Type: EventSessionInfo, Title: title}); err != nil {
				return nil, err
			}
		}
	}
	if err := ag.Run(ctx, sess, sink); err != nil {
		return nil, fmt.Errorf("agent loop: %w", err)
	}

	return &acp.PromptResponse{StopReason: acp.StopEndTurn}, nil
}

func (m *Manager) LoadSession(ctx context.Context, req *acp.LoadSessionRequest, sender acp.SessionUpdateSender) (*acp.LoadSessionResponse, error) {
	sess, err := session.Load(string(req.SessionID))
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if err := replaySession(sess, m.contextWindow(sess.AgentName), sender); err != nil {
		return nil, fmt.Errorf("replay session: %w", err)
	}
	return &acp.LoadSessionResponse{}, nil
}

// contextWindow returns the context size configured for a profile.
func (m *Manager) contextWindow(profile string) int {
	// 128000 is the documented default of contextWindowTokens.
	const fallback = 128000
	if p, ok := m.config.Profiles[profile]; ok {
		return cmp.Or(p.ContextWindowTokens, fallback)
	}
	return fallback
}

func (m *Manager) ResumeSession(ctx context.Context, req *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
	if _, err := session.Load(string(req.SessionID)); err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	return &acp.ResumeSessionResponse{}, nil
}

func (m *Manager) CloseSession(ctx context.Context, req *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
	return &acp.CloseSessionResponse{}, nil
}

func (m *Manager) DeleteSession(ctx context.Context, req *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error) {
	if err := session.Delete(string(req.SessionID)); err != nil {
		return nil, fmt.Errorf("delete session: %w", err)
	}
	return &acp.DeleteSessionResponse{}, nil
}

func (m *Manager) ListSessions(ctx context.Context, req *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	sessions, err := session.List()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	infos := make([]acp.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		var title *string
		if value := s.Title; value != "" {
			title = &value
		}
		var updatedAt *string
		if !s.UpdatedAt.IsZero() {
			value := s.UpdatedAt.Format(time.RFC3339)
			updatedAt = &value
		}
		infos = append(infos, acp.SessionInfo{
			SessionID: acp.SessionID(s.ID),
			Title:     title,
			UpdatedAt: updatedAt,
			Meta:      acp.Meta{acp.MiyaProfileMetaKey: s.AgentName},
		})
	}
	return &acp.ListSessionsResponse{Sessions: infos}, nil
}

func (m *Manager) SetSessionMode(ctx context.Context, req *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
	return &acp.SetSessionModeResponse{}, nil
}

func (m *Manager) SetSessionConfigOption(ctx context.Context, req *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	return &acp.SetSessionConfigOptionResponse{}, nil
}

func (m *Manager) Logout(ctx context.Context, req *acp.LogoutRequest) (*acp.LogoutResponse, error) {
	return &acp.LogoutResponse{}, nil
}
