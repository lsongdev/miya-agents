package agent

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/session"
)

// fakeModel replays one scripted response per call. A step that is an error
// ends that response's stream with it.
type fakeModel struct {
	responses [][]any // llm.Delta or error
	requests  []*llm.Request
}

func (m *fakeModel) Stream(_ context.Context, req *llm.Request) iter.Seq2[llm.Delta, error] {
	copied := *req
	copied.Messages = append([]llm.Message(nil), req.Messages...)
	m.requests = append(m.requests, &copied)
	var steps []any
	if len(m.responses) > 0 {
		steps, m.responses = m.responses[0], m.responses[1:]
	}
	return func(yield func(llm.Delta, error) bool) {
		for _, step := range steps {
			switch step := step.(type) {
			case llm.Delta:
				if !yield(step, nil) {
					return
				}
			case error:
				yield(llm.Delta{}, step)
				return
			}
		}
	}
}

func fakeAgent(responses ...[]any) (*Agent, *fakeModel) {
	model := &fakeModel{responses: responses}
	return &Agent{Name: "test", Model: model, ModelName: "test"}, model
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
	ag, _ := fakeAgent()

	err := ag.Run(context.Background(), session.New("test"), discardSink{})
	if err == nil || !strings.Contains(err.Error(), "closed without a response") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestRunRejectsInterruptedStream(t *testing.T) {
	ag, _ := fakeAgent([]any{llm.Delta{Text: "partial"}, errors.New("connection reset")})
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
	ag, _ := fakeAgent([]any{llm.Delta{ToolCall: &llm.ToolCallDelta{Name: "read_file", Arguments: `{}`}}})

	err := ag.Run(context.Background(), session.New("test"), discardSink{})
	if err == nil || !strings.Contains(err.Error(), "missing an id") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestRunDoesNotPersistSession(t *testing.T) {
	oldConfigPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = oldConfigPath })

	ag, _ := fakeAgent([]any{llm.Delta{Text: "done"}})

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

type echoTool struct{}

func (echoTool) Def() llm.ToolDef { return llm.ToolDef{Name: "echo"} }
func (echoTool) Run(_ context.Context, args string) string {
	return "echo:" + args
}

func TestRunExecutesToolCallsAndContinues(t *testing.T) {
	ag, model := fakeAgent(
		[]any{
			llm.Delta{Thinking: "need tools"},
			llm.Delta{ToolCall: &llm.ToolCallDelta{Index: 0, ID: "call_1", Name: "echo"}},
			llm.Delta{ToolCall: &llm.ToolCallDelta{Index: 0, Arguments: `{"a":1}`}},
			llm.Delta{ToolCall: &llm.ToolCallDelta{Index: 1, ID: "call_2", Name: "missing", Arguments: `{}`}},
		},
		[]any{llm.Delta{Text: "all done"}},
	)
	ag.System = "be brief"
	ag.Tools = []Tool{echoTool{}}
	sess := session.New("test")
	sess.AppendRequest("go")

	if err := ag.Run(context.Background(), sess, discardSink{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(model.requests) != 2 {
		t.Fatalf("model calls = %d, want 2", len(model.requests))
	}
	first, second := model.requests[0], model.requests[1]
	if first.System != "be brief" || len(first.Tools) != 1 || len(first.Messages) != 1 {
		t.Fatalf("first request = %#v", first)
	}
	// user, assistant with two calls, two tool results
	if len(second.Messages) != 4 {
		t.Fatalf("second request has %d messages, want 4", len(second.Messages))
	}
	assistant := second.Messages[1]
	if assistant.Thinking() != "need tools" || len(assistant.ToolCalls()) != 2 {
		t.Fatalf("assistant message = %#v", assistant)
	}
	ok := second.Messages[2].Content[0].ToolResult
	if ok.ID != "call_1" || ok.Content != `echo:{"a":1}` || ok.IsError {
		t.Fatalf("tool result = %#v", ok)
	}
	unknown := second.Messages[3].Content[0].ToolResult
	if unknown.ID != "call_2" || !unknown.IsError || !strings.Contains(unknown.Content, "unknown tool") {
		t.Fatalf("unknown tool result = %#v", unknown)
	}
	if got := sess.Messages[len(sess.Messages)-1].Text(); got != "all done" {
		t.Fatalf("final message = %q", got)
	}
}
