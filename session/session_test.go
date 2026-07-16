package session

import (
	"testing"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/openai"
)

func TestDisplayTitle(t *testing.T) {
	sess := &Session{
		Messages: []openai.ChatCompletionMessage{
			openai.SystemMessage("system"),
			openai.UserMessage("  Implement session titles\nwith metadata  "),
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
