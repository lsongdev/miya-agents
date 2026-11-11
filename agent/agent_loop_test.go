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

func TestRunRejectsEmptyStream(t *testing.T) {
	ag, _ := fakeAgent()

	err := ag.Run(context.Background(), session.New("test"), Discard)
	if err == nil || !strings.Contains(err.Error(), "closed without a response") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestRunRejectsInterruptedStream(t *testing.T) {
	ag, _ := fakeAgent([]any{llm.Delta{Text: "partial"}, errors.New("connection reset")})
	sess := session.New("test")

	err := ag.Run(context.Background(), sess, Discard)
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("Run error = %v", err)
	}
	if got := sess.Context(); len(got) != 0 {
		t.Fatalf("interrupted response was saved: %#v", got)
	}
}

func TestRunRejectsToolCallWithoutID(t *testing.T) {
	ag, _ := fakeAgent([]any{llm.Delta{ToolCall: &llm.ToolCallDelta{Name: "read_file", Arguments: `{}`}}})

	err := ag.Run(context.Background(), session.New("test"), Discard)
	if err == nil || !strings.Contains(err.Error(), "missing an id") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestRunDoesNotPersistSession(t *testing.T) {
	oldConfigPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = oldConfigPath })

	ag, _ := fakeAgent([]any{llm.Delta{Text: "done"}})

	if err := ag.Run(context.Background(), session.New("test"), Discard); err != nil {
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
	sess.AddUser("go")

	if err := ag.Run(context.Background(), sess, Discard); err != nil {
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
	if got := sess.Entries[len(sess.Entries)-1].Message.Text(); got != "all done" {
		t.Fatalf("final message = %q", got)
	}
}

type fileTool struct{}

func (fileTool) Def() llm.ToolDef                   { return llm.ToolDef{Name: "attach"} }
func (fileTool) Run(context.Context, string) string { return "attached" }
func (fileTool) RunFiles(context.Context, string) (string, []llm.Media) {
	return "attached", []llm.Media{{Name: "a.png", MimeType: "image/png", Size: 3, Data: "AAAA"}}
}

func TestRunReportsEventsInOrder(t *testing.T) {
	usage := &llm.Usage{InputTokens: 100, OutputTokens: 20}
	ag, _ := fakeAgent(
		[]any{
			llm.Delta{Thinking: "hm"},
			llm.Delta{Text: "On it."},
			llm.Delta{ToolCall: &llm.ToolCallDelta{ID: "c1", Name: "attach", Arguments: `{}`}},
			llm.Delta{Usage: usage},
		},
		[]any{llm.Delta{Text: "Done."}},
	)
	ag.ContextWindow = 1000
	ag.Tools = []Tool{fileTool{}}

	var events []Event
	sink := func(event Event) error {
		events = append(events, event)
		return nil
	}
	sess := session.New("test")
	if err := ag.Run(context.Background(), sess, sink); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var types []string
	for _, event := range events {
		types = append(types, string(event.Type))
	}
	want := "thought text usage tool_start file tool_done text done"
	if got := strings.Join(types, " "); got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
	if got := events[2]; got.Usage != usage || got.ContextWindow != 1000 {
		t.Fatalf("usage event = %#v", got)
	}
	if got := events[4].File; got.Name != "a.png" || got.Data != "AAAA" {
		t.Fatalf("file event = %#v", got)
	}
	if got := events[5].Tool; got.Call.ID != "c1" || got.Result != "attached" || got.Failed {
		t.Fatalf("tool_done event = %#v", got)
	}

	// The log records what the events reported: usage with the assistant
	// message, the file with the tool result.
	entries := sess.Entries
	if got := entries[1]; got.Usage != usage || len(got.Message.ToolCalls()) != 1 {
		t.Fatalf("assistant entry = %#v", got)
	}
	if got := entries[2]; len(got.Files) != 1 || got.Files[0].Name != "a.png" || got.Message.Role != llm.RoleTool {
		t.Fatalf("tool entry = %#v", got)
	}
}

func TestRunStopsWhenSinkFails(t *testing.T) {
	ag, model := fakeAgent([]any{llm.Delta{Text: "a"}, llm.Delta{Text: "b"}})
	stop := errors.New("client went away")

	err := ag.Run(context.Background(), session.New("test"), func(Event) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("Run error = %v", err)
	}
	if len(model.requests) != 1 {
		t.Fatalf("model calls = %d", len(model.requests))
	}
}
