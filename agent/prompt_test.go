package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/session"
)

// An ACP prompt that needs one tool call: the request carries the workspace
// system prompt, the tool result is fed back, and the session is persisted in
// the llm message format.
func TestPromptRunsToolRoundTripAndPersists(t *testing.T) {
	oldConfigPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = oldConfigPath })

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("You are Miya."), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("the secret is 42"), 0644); err != nil {
		t.Fatal(err)
	}

	type wireRequest struct {
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	var requests []wireRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wireRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		requests = append(requests, req)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(requests) == 1 {
			fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"note.txt\"}"}}]}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"It is 42."}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "test", Workspace: workspace, Tools: []string{"read_file"}},
		},
		Providers: map[string]*config.ProviderConfig{"openai": {APIBase: server.URL}},
	})
	ctx := context.Background()
	sender := &recordingSender{}
	created, err := m.NewSession(ctx, &acp.NewSessionRequest{}, sender)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	_, err = m.Prompt(ctx, &acp.PromptRequest{
		SessionID: created.SessionID,
		Prompt:    []acp.ContentBlock{{Type: "text", Text: "what is the secret?"}},
	}, sender)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("model calls = %d, want 2", len(requests))
	}
	first := requests[0].Messages
	if len(first) != 2 || first[0].Role != "system" || first[0].Content != "You are Miya." || first[1].Content != "what is the secret?" {
		t.Fatalf("first request messages = %#v", first)
	}
	if len(requests[0].Tools) != 1 {
		t.Fatalf("tools sent = %d, want 1", len(requests[0].Tools))
	}
	second := requests[1].Messages
	if len(second) != 4 || second[3].Role != "tool" || second[3].ToolCallID != "call_1" || !strings.Contains(second[3].Content, "the secret is 42") {
		t.Fatalf("second request messages = %#v", second)
	}

	saved, err := session.Load(string(created.SessionID))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	messages := saved.Context()
	roles := ""
	for _, message := range messages {
		roles += string(message.Role) + " "
	}
	if roles != "user assistant tool assistant " {
		t.Fatalf("saved roles = %q", roles)
	}
	if calls := messages[1].ToolCalls(); len(calls) != 1 || calls[0] != (llm.ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"path":"note.txt"}`}) {
		t.Fatalf("saved tool calls = %#v", calls)
	}
	if got := messages[3].Text(); got != "It is 42." {
		t.Fatalf("saved answer = %q", got)
	}
	if saved.Title != "what is the secret?" {
		t.Fatalf("saved title = %q", saved.Title)
	}

	kinds := ""
	for _, update := range sender.updates {
		kinds += update.SessionUpdate + " "
	}
	if kinds != "session_info_update tool_call tool_call_update agent_message_chunk " {
		t.Fatalf("live updates = %q", kinds)
	}

	// Loading the session replays the same conversation from the log.
	replay := &recordingSender{}
	if _, err := m.LoadSession(ctx, &acp.LoadSessionRequest{SessionID: created.SessionID}, replay); err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	kinds = ""
	for _, update := range replay.updates {
		kinds += update.SessionUpdate + " "
	}
	if kinds != "user_message_chunk session_info_update tool_call tool_call_update agent_message_chunk " {
		t.Fatalf("replayed updates = %q", kinds)
	}
	if got := replay.updates[0].Content.Text; got != "what is the secret?" {
		t.Fatalf("replayed prompt = %q", got)
	}
	if got := replay.updates[3].ToolCallUpdate; string(got.ToolCallID) != "call_1" || !strings.Contains(got.Content[0].Content.Text, "the secret is 42") {
		t.Fatalf("replayed tool result = %#v", got)
	}
}
