package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/mcp"
	"github.com/lsongdev/miya-agents/session"
)

func TestResolveAgentNameFallsBackWhenSessionProfileMissing(t *testing.T) {
	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"coding": {Provider: "openai", ModelName: "gpt-4"},
		},
		Providers: map[string]*config.ProviderConfig{},
	})

	got, err := m.resolveAgentName("default")
	if err != nil {
		t.Fatalf("resolveAgentName: %v", err)
	}
	if got != "coding" {
		t.Fatalf("resolveAgentName = %q, want coding", got)
	}
}

func TestResolveAgentNamePrefersDefault(t *testing.T) {
	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "gpt-4"},
			"coding":  {Provider: "openai", ModelName: "gpt-4"},
		},
		Providers: map[string]*config.ProviderConfig{},
	})

	got, err := m.resolveAgentName("")
	if err != nil {
		t.Fatalf("resolveAgentName: %v", err)
	}
	if got != "default" {
		t.Fatalf("resolveAgentName = %q, want default", got)
	}
}

func TestResolveAgentNameReportsEmptyProfiles(t *testing.T) {
	m := NewAgentManager(&config.Config{
		Profiles:  map[string]*config.ProfileConfig{},
		Providers: map[string]*config.ProviderConfig{},
	})

	if _, err := m.resolveAgentName("default"); err == nil {
		t.Fatal("resolveAgentName succeeded, want error")
	}
}

func TestNewSessionUsesRequestedProfileAndListsMetadata(t *testing.T) {
	previousPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = previousPath })

	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "gpt-4"},
			"coding":  {Provider: "openai", ModelName: "gpt-5"},
		},
		Providers: map[string]*config.ProviderConfig{},
	})
	created, err := m.NewSession(context.Background(), &acp.NewSessionRequest{
		Meta: acp.Meta{acp.MiyaProfileMetaKey: "coding"},
	}, &recordingSender{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := created.Meta[acp.MiyaProfileMetaKey]; got != "coding" {
		t.Fatalf("created profile = %v, want coding", got)
	}

	listed, err := m.ListSessions(context.Background(), &acp.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(listed.Sessions))
	}
	if got := listed.Sessions[0].Meta[acp.MiyaProfileMetaKey]; got != "coding" {
		t.Fatalf("listed profile = %v, want coding", got)
	}
}

func TestNewSessionRejectsUnknownRequestedProfile(t *testing.T) {
	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "gpt-4"},
		},
	})
	_, err := m.NewSession(context.Background(), &acp.NewSessionRequest{
		Meta: acp.Meta{acp.MiyaProfileMetaKey: "missing"},
	}, &recordingSender{})
	if err == nil {
		t.Fatal("NewSession succeeded, want unknown profile error")
	}
}

func TestUseAgentIncludesConfiguredMCPTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			writeMCPEvent(t, w, req.ID, map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "coffee-test",
					"version": "1.0.0",
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeMCPEvent(t, w, req.ID, map[string]any{
				"tools": []map[string]any{{
					"name":        "queryShopList",
					"description": "query shops",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{},
					},
				}},
			})
		default:
			t.Fatalf("unexpected method: %s", req.Method)
		}
	}))
	defer server.Close()

	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "gpt-4"},
		},
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIKey: "test-key"},
		},
		McpServers: map[string]*mcp.McpServerConfig{
			"coffee": {Type: "streamablehttp", URL: server.URL},
		},
	})

	ag, err := m.UseAgent("default")
	if err != nil {
		t.Fatalf("UseAgent: %v", err)
	}
	if ag.tool("mcp_coffee_queryShopList") == nil {
		t.Fatalf("missing MCP tool; tools = %#v", ag.Tools)
	}
}

func TestUseAgentUsesAnthropicProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req["model"] != "claude-test" {
			t.Fatalf("model = %#v", req["model"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "claude", ModelName: "claude-test", Workspace: t.TempDir()},
		},
		Providers: map[string]*config.ProviderConfig{
			"claude": {Type: "anthropic", APIBase: server.URL, APIKey: "test-key"},
		},
	})
	ag, err := m.UseAgent("default")
	if err != nil {
		t.Fatal(err)
	}
	var builder llm.Builder
	request := &llm.Request{Model: ag.ModelName, Messages: []llm.Message{llm.UserText("hi")}}
	for delta, err := range ag.Model.Stream(context.Background(), request) {
		if err != nil {
			t.Fatal(err)
		}
		builder.Add(delta)
	}
	if got := builder.Message().Text(); got != "hello" {
		t.Fatalf("content = %q", got)
	}
}

func writeMCPEvent(t *testing.T, w http.ResponseWriter, id int64, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
}

type recordingSender struct {
	updates []acp.SessionUpdate
}

func (s *recordingSender) Send(update acp.SessionUpdate) error {
	s.updates = append(s.updates, update)
	return nil
}

func TestReplaySessionDerivesUpdatesFromLog(t *testing.T) {
	sess := session.New("default")
	sess.AddUser("read file")
	sess.SetTitle("Read a file")
	call := llm.Message{Role: llm.RoleAssistant, Content: []llm.Block{
		llm.Thinking("need the file"),
		{Type: llm.BlockToolCall, ToolCall: &llm.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"README.md"}`}},
	}}
	sess.Add(session.Entry{Message: &call, Usage: &llm.Usage{InputTokens: 50, OutputTokens: 5}})
	result := llm.ToolResultMessage(llm.ToolResult{ID: "call-1", Name: "read_file", Content: "hello"})
	sess.Add(session.Entry{Message: &result, Files: []llm.Media{{Name: "a.png", MimeType: "image/png", Data: "AAAA"}}})
	answer := llm.AssistantText("It says hello.")
	sess.Add(session.Entry{Message: &answer, Usage: &llm.Usage{InputTokens: 70, OutputTokens: 10}})

	sender := &recordingSender{}
	if err := replaySession(sess, 1000, sender); err != nil {
		t.Fatalf("replay: %v", err)
	}

	var kinds []string
	for _, update := range sender.updates {
		kinds = append(kinds, update.SessionUpdate)
	}
	want := "user_message_chunk session_info_update agent_thought_chunk tool_call agent_message_chunk tool_call_update agent_message_chunk usage_update"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("updates = %q\nwant      %q", got, want)
	}
	if got := sender.updates[3].ToolCall; got.ToolCallID != "call-1" || got.Kind != acp.ToolKindRead {
		t.Fatalf("tool call = %#v", got)
	}
	if got := sender.updates[4].Content; got.Type != "image" || got.Data != "AAAA" {
		t.Fatalf("file = %#v", got)
	}
	// Only the latest usage is replayed.
	if got := sender.updates[7].Usage; got.Used != 80 || got.Size != 1000 {
		t.Fatalf("usage = %#v", got)
	}
}

func TestToolCallRawJSONValueMarshalsJSONSafely(t *testing.T) {
	call := acpToolCall(ToolEvent{Call: llm.ToolCall{ID: "call-raw", Name: "exec", Arguments: `{"command":"yt-dlp -x"}`}})
	if !json.Valid(call.RawInput) {
		t.Fatalf("raw input is invalid JSON: %s", call.RawInput)
	}
	var raw map[string]any
	if err := json.Unmarshal(call.RawInput, &raw); err != nil {
		t.Fatalf("raw input unmarshal: %v", err)
	}
	if got := raw["command"]; got != "yt-dlp -x" {
		t.Fatalf("command = %#v", got)
	}

	update := toolCallDoneUpdate(ToolEvent{Call: llm.ToolCall{ID: "call-raw"}, Result: `bad \x escape from tool output`})
	if _, err := json.Marshal(update); err != nil {
		t.Fatalf("marshal update with raw output: %v", err)
	}
	if !json.Valid(update.ToolCallUpdate.RawOutput) {
		t.Fatalf("raw output is invalid JSON: %s", update.ToolCallUpdate.RawOutput)
	}
}
