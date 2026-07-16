package agent

import (
	"testing"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/openai"
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

func TestReplaySessionReplaysToolCalls(t *testing.T) {
	sess := &session.Session{
		Messages: []openai.ChatCompletionMessage{
			openai.UserMessage("read file"),
			openai.AssistantMessageWithTools("", []openai.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: openai.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"README.md"}`,
				},
			}}),
			openai.ToolResultMessage("call-1", "read_file", "hello"),
		},
	}
	sender := &recordingSender{}

	if err := replaySession(sess, sender); err != nil {
		t.Fatalf("replaySession: %v", err)
	}

	var sawToolCall, sawToolDone bool
	for _, update := range sender.updates {
		switch update.SessionUpdate {
		case "tool_call":
			sawToolCall = update.ToolCall != nil && update.ToolCall.ToolCallID == "call-1"
		case "tool_call_update":
			sawToolDone = update.ToolCallUpdate != nil && update.ToolCallUpdate.ToolCallID == "call-1"
		}
	}
	if !sawToolCall {
		t.Fatal("missing tool_call update")
	}
	if !sawToolDone {
		t.Fatal("missing tool_call_update update")
	}
}
