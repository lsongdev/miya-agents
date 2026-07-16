package agent

import (
	"encoding/json"
	"testing"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/config"
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

type recordingSender struct {
	updates []acp.SessionUpdate
}

func (s *recordingSender) Send(update acp.SessionUpdate) error {
	s.updates = append(s.updates, update)
	return nil
}

func TestReplaySessionReplaysEventLog(t *testing.T) {
	sess := &session.Session{
		Events: []session.Event{
			{ID: "evt_000001", Update: acp.SessionUpdate{
				SessionUpdate: "user_message_chunk",
				Content:       acp.ContentBlock{Type: "text", Text: "read file"},
			}},
			{ID: "evt_000002", Update: acp.SessionUpdate{
				SessionUpdate: "tool_call",
				ToolCall:      acpToolCall(ToolCallEvent{ID: "call-1", Name: "read_file", Arguments: `{"path":"README.md"}`}),
			}},
			{ID: "evt_000003", Update: acp.SessionUpdate{
				SessionUpdate:  "tool_call_update",
				ToolCallUpdate: acpToolCallUpdate(ToolCallEvent{ID: "call-1", Result: "hello"}, acp.ToolCallCompleted),
			}},
		},
	}
	sender := &recordingSender{}

	if err := replaySession(sess, sender); err != nil {
		t.Fatalf("replaySession: %v", err)
	}

	if len(sender.updates) != len(sess.Events) {
		t.Fatalf("updates = %d, want %d", len(sender.updates), len(sess.Events))
	}
	if sender.updates[0].SessionUpdate != "user_message_chunk" {
		t.Fatalf("first update = %q", sender.updates[0].SessionUpdate)
	}
	if sender.updates[1].ToolCall == nil || sender.updates[1].ToolCall.ToolCallID != "call-1" {
		t.Fatal("missing replayed tool_call update")
	}
	if sender.updates[2].ToolCallUpdate == nil || sender.updates[2].ToolCallUpdate.ToolCallID != "call-1" {
		t.Fatal("missing replayed tool_call_update update")
	}
}

func TestToolCallRawJSONValueMarshalsJSONSafely(t *testing.T) {
	call := acpToolCall(ToolCallEvent{ID: "call-raw", Name: "exec", Arguments: `{"command":"yt-dlp -x"}`})
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

	update := toolCallDoneUpdate(ToolCallEvent{ID: "call-raw", Result: `bad \x escape from tool output`})
	if _, err := json.Marshal(update); err != nil {
		t.Fatalf("marshal update with raw output: %v", err)
	}
	if !json.Valid(update.ToolCallUpdate.RawOutput) {
		t.Fatalf("raw output is invalid JSON: %s", update.ToolCallUpdate.RawOutput)
	}
}
