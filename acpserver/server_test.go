package acpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/agent"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/runtime"
	"github.com/lsongdev/miya-agents/session"
)

type recordingSender struct {
	updates []acp.SessionUpdate
}

func (s *recordingSender) Send(update acp.SessionUpdate) error {
	s.updates = append(s.updates, update)
	return nil
}

func TestNewSessionUsesRequestedProfileAndListsMetadata(t *testing.T) {
	previousPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = previousPath })

	m := New(runtime.New(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "gpt-4"},
			"coding":  {Provider: "openai", ModelName: "gpt-5"},
		},
		Providers: map[string]*config.ProviderConfig{},
	}))
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
	m := New(runtime.New(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "gpt-4"},
		},
	}))
	_, err := m.NewSession(context.Background(), &acp.NewSessionRequest{
		Meta: acp.Meta{acp.MiyaProfileMetaKey: "missing"},
	}, &recordingSender{})
	if err == nil {
		t.Fatal("NewSession succeeded, want unknown profile error")
	}
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
	call := acpToolCall(agent.ToolEvent{Call: llm.ToolCall{ID: "call-raw", Name: "exec", Arguments: `{"command":"yt-dlp -x"}`}})
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

	update := toolCallDoneUpdate(agent.ToolEvent{Call: llm.ToolCall{ID: "call-raw"}, Result: `bad \x escape from tool output`})
	if _, err := json.Marshal(update); err != nil {
		t.Fatalf("marshal update with raw output: %v", err)
	}
	if !json.Valid(update.ToolCallUpdate.RawOutput) {
		t.Fatalf("raw output is invalid JSON: %s", update.ToolCallUpdate.RawOutput)
	}
}
