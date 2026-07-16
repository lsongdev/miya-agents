package agent

import (
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/session"
)

func TestAppendContextMaintenanceNotice(t *testing.T) {
	ag := &Agent{Config: &config.ProfileConfig{ContextWindowTokens: 20, ContextWarnRatio: 0.90}}
	sess := &session.Session{
		ID: "session-1",
		Messages: []openai.ChatCompletionMessage{
			openai.SystemMessage("system"),
			openai.UserMessage(strings.Repeat("x", 80)),
			openai.AssistantMessage(strings.Repeat("y", 80)),
		},
	}

	ag.AppendContextMaintenanceNotice(sess)
	ag.AppendContextMaintenanceNotice(sess)

	count := 0
	for _, msg := range sess.Messages {
		if strings.HasPrefix(msg.Content, maintenanceNoticePrefix) {
			count++
			if !strings.Contains(msg.Content, "session-1") {
				t.Fatalf("notice missing session id: %q", msg.Content)
			}
			if !strings.Contains(msg.Content, "~/.miya/sessions/session-1.json") {
				t.Fatalf("notice missing session path: %q", msg.Content)
			}
			if strings.Contains(msg.Content, "session"+"-"+"maintenance") {
				t.Fatalf("notice should not mention named maintenance skills: %q", msg.Content)
			}
		}
	}
	if count != 1 {
		t.Fatalf("notice count = %d, want 1", count)
	}
}

func TestAppendContextMaintenanceNoticeSkipsWhenThereIsRoom(t *testing.T) {
	ag := &Agent{Config: &config.ProfileConfig{ContextWindowTokens: 1000, ContextWarnRatio: 0.90}}
	sess := &session.Session{
		ID:       "session-1",
		Messages: []openai.ChatCompletionMessage{openai.UserMessage("short")},
	}

	ag.AppendContextMaintenanceNotice(sess)

	if len(sess.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(sess.Messages))
	}
}
