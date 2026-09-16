package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/session"
)

func fakeStream(chunks ...openai.ChatCompletionResponse) StreamFunc {
	return func(context.Context, *openai.ChatCompletionRequest) (<-chan openai.ChatCompletionResponse, error) {
		ch := make(chan openai.ChatCompletionResponse, len(chunks))
		for _, chunk := range chunks {
			ch <- chunk
		}
		close(ch)
		return ch, nil
	}
}

type discardSink struct{}

func (discardSink) AssistantDelta(string) error        { return nil }
func (discardSink) AssistantFile(FileEvent) error      { return nil }
func (discardSink) ThoughtDelta(string) error          { return nil }
func (discardSink) ToolCallStart(ToolCallEvent) error  { return nil }
func (discardSink) ToolCallDone(ToolCallEvent) error   { return nil }
func (discardSink) SessionInfo(SessionInfoEvent) error { return nil }
func (discardSink) Usage(UsageEvent) error             { return nil }
func (discardSink) Done() error                        { return nil }

func TestRunRejectsEmptyStream(t *testing.T) {
	ag := New("test", &config.ProfileConfig{ModelName: "test"}, fakeStream())

	err := ag.Run(context.Background(), session.New("test"), discardSink{})
	if err == nil || !strings.Contains(err.Error(), "closed without a response") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestRunRejectsInterruptedStream(t *testing.T) {
	message := openai.ChatCompletionMessage{Role: openai.RoleAssistant, Content: "partial"}
	ag := New("test", &config.ProfileConfig{ModelName: "test"}, fakeStream(
		openai.ChatCompletionResponse{Choices: []openai.ChatCompletionChoice{{Index: 0, Delta: &message}}},
		openai.ChatCompletionResponse{Error: &openai.Error{Type: "stream_error", Message: "connection reset"}},
	))
	sess := session.New("test")

	err := ag.Run(context.Background(), sess, discardSink{})
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("Run error = %v", err)
	}
	if len(sess.Messages) != 0 {
		t.Fatalf("interrupted response was saved: %#v", sess.Messages)
	}
}

func TestRunRejectsToolCallWithoutID(t *testing.T) {
	message := openai.ChatCompletionMessage{
		Role: openai.RoleAssistant,
		ToolCalls: []openai.ToolCall{{
			Function: openai.FunctionCall{Name: "read_file", Arguments: `{}`},
		}},
	}
	ag := New("test", &config.ProfileConfig{ModelName: "test"}, fakeStream(
		openai.ChatCompletionResponse{Choices: []openai.ChatCompletionChoice{{Index: 0, Delta: &message}}},
	))

	err := ag.Run(context.Background(), session.New("test"), discardSink{})
	if err == nil || !strings.Contains(err.Error(), "missing an id") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestRunDoesNotPersistSession(t *testing.T) {
	oldConfigPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = oldConfigPath })

	message := openai.ChatCompletionMessage{Role: openai.RoleAssistant, Content: "done"}
	ag := New("test", &config.ProfileConfig{ModelName: "test"}, fakeStream(
		openai.ChatCompletionResponse{Choices: []openai.ChatCompletionChoice{{Index: 0, Delta: &message}}},
	))

	if err := ag.Run(context.Background(), session.New("test"), discardSink{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sessions, err := session.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("Run persisted %d sessions, want 0", len(sessions))
	}
}
