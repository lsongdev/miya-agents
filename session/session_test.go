package session

import (
	"os"
	"reflect"
	"testing"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
)

func TestDisplayTitle(t *testing.T) {
	sess := &Session{
		Messages: []llm.Message{
			llm.AssistantText("greeting"),
			llm.UserText("  Implement session titles\nwith metadata  "),
		},
	}

	if got := sess.DisplayTitle(); got != "Implement session titles with metadata" {
		t.Fatalf("title = %q", got)
	}

	sess.Title = "Explicit title"
	if got := sess.DisplayTitle(); got != "Explicit title" {
		t.Fatalf("explicit title = %q", got)
	}
}

func TestAppendEvent(t *testing.T) {
	sess := &Session{}
	sess.AppendEvent(acp.SessionUpdate{
		SessionUpdate: "agent_message_chunk",
		Content:       acp.ContentBlock{Type: "text", Text: "hello"},
	})

	if len(sess.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(sess.Events))
	}
	if sess.Events[0].ID != "evt_000001" {
		t.Fatalf("event id = %q", sess.Events[0].ID)
	}
	if sess.Events[0].Update.Content.Text != "hello" {
		t.Fatalf("event text = %q", sess.Events[0].Update.Content.Text)
	}
}

func TestLoadReadsLegacyOpenAIMessages(t *testing.T) {
	oldConfigPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = oldConfigPath })

	legacy := `{
	  "id": "old", "agent_name": "default", "title": "Old chat",
	  "messages": [
	    {"role": "system", "content": "You are helpful."},
	    {"role": "user", "content": "list files"},
	    {"role": "assistant", "reasoning_content": "use exec", "tool_calls": [
	      {"index": 0, "id": "call_1", "type": "function", "function": {"name": "exec", "arguments": "{\"command\":\"ls\"}"}}]},
	    {"role": "tool", "tool_call_id": "call_1", "name": "exec", "content": "a.txt"},
	    {"role": "assistant", "content": "There is a.txt."},
	    {"role": "system", "content": "[context maintenance notice] ~5% context remaining"}
	  ],
	  "events": [], "compactions": []
	}`
	if err := os.MkdirAll(sessionsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath("old"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	sess, err := Load("old")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []llm.Message{
		llm.UserText("list files"),
		{Role: llm.RoleAssistant, Content: []llm.Block{
			llm.Thinking("use exec"),
			{Type: llm.BlockToolCall, ToolCall: &llm.ToolCall{ID: "call_1", Name: "exec", Arguments: `{"command":"ls"}`}},
		}},
		llm.ToolResultMessage(llm.ToolResult{ID: "call_1", Name: "exec", Content: "a.txt"}),
		llm.AssistantText("There is a.txt."),
	}
	if !reflect.DeepEqual(sess.Messages, want) {
		t.Fatalf("messages = %#v", sess.Messages)
	}
	if sess.Title != "Old chat" || sess.AgentName != "default" {
		t.Fatalf("metadata = %q %q", sess.Title, sess.AgentName)
	}

	// Saved in the current shape, it loads back unchanged.
	if err := sess.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := Load("old")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(reloaded.Messages, want) {
		t.Fatalf("reloaded messages = %#v", reloaded.Messages)
	}
}
