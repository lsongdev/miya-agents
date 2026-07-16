package session

import (
	"testing"

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
