package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/session"
)

type fakeAgentRunner struct {
	calls []string
}

func (r *fakeAgentRunner) RunAgent(ctx context.Context, name, prompt string) (string, error) {
	r.calls = append(r.calls, name)
	if name == defaultSummarizerAgent {
		return "", errFakeMissingAgent
	}
	return "Goal: keep the important context\nOpen Tasks: continue", nil
}

type fakeMissingAgentError string

func (e fakeMissingAgentError) Error() string { return string(e) }

const errFakeMissingAgent = fakeMissingAgentError("agent not found")

func withTempConfigPath(t *testing.T) {
	t.Helper()
	old := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() {
		config.ConfigPath = old
	})
}

func TestRenameSessionToolUpdatesTitleAndEvent(t *testing.T) {
	withTempConfigPath(t)
	sess := session.New("default")
	tool := NewSessionTools(sess, nil)[0]

	got := tool.Run(context.Background(), `{"title":"  **Build Miya**  "}`)
	if got != "Session title updated." {
		t.Fatalf("Run = %q", got)
	}
	if sess.Title != "Build Miya" {
		t.Fatalf("title = %q", sess.Title)
	}
	if len(sess.Events) != 0 {
		t.Fatalf("rename tool should not write replay events directly: %+v", sess.Events)
	}
}

func TestCompactContextKeepsEventsAndFallsBackToSessionAgent(t *testing.T) {
	withTempConfigPath(t)
	runner := &fakeAgentRunner{}
	sess := session.New("default")
	sess.Messages = []openai.ChatCompletionMessage{
		openai.SystemMessage("system"),
		openai.UserMessage("old 1"),
		openai.AssistantMessage("old 2"),
		openai.UserMessage("old 3"),
		openai.AssistantMessage("old 4"),
		openai.UserMessage("old 5"),
		openai.AssistantMessage("old 6"),
		openai.UserMessage("old 7"),
		openai.AssistantMessage("old 8"),
		openai.UserMessage("old 9"),
		openai.AssistantMessage("old 10"),
		openai.UserMessage("recent 1"),
		openai.AssistantMessage("recent 2"),
		openai.UserMessage("recent 3"),
		openai.AssistantMessageWithTools("", []openai.ToolCall{{ID: "call-1", Function: openai.FunctionCall{Name: "compact_context"}}}),
	}
	sess.AppendEvent(acp.SessionUpdate{SessionUpdate: "agent_message_chunk", Content: acp.ContentBlock{Type: "text", Text: "visible"}})
	tool := &CompactContextTool{toolset: &SessionToolset{Session: sess, Runner: runner}}

	got := tool.Run(context.Background(), `{"keep_recent":8}`)
	if !strings.HasPrefix(got, "Context compacted.") {
		t.Fatalf("Run = %q", got)
	}
	if len(runner.calls) != 2 || runner.calls[0] != defaultSummarizerAgent || runner.calls[1] != "default" {
		t.Fatalf("runner calls = %+v", runner.calls)
	}
	if len(sess.Events) != 1 {
		t.Fatalf("events changed: %+v", sess.Events)
	}
	if len(sess.Compactions) != 1 {
		t.Fatalf("compactions = %d", len(sess.Compactions))
	}
	if len(sess.Messages) < 2 || !strings.HasPrefix(sess.Messages[1].Content, compactionSummaryMark) {
		t.Fatalf("missing compaction summary message: %+v", sess.Messages)
	}
	if got := sess.Messages[len(sess.Messages)-1]; len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "call-1" {
		t.Fatalf("active tool call was not preserved: %+v", got)
	}
}
