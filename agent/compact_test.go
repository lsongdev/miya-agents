package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/session"
)

// longSession is user, assistant+call, tool, assistant, user, assistant.
func longSession(t *testing.T) *session.Session {
	t.Helper()
	sess := session.New("test")
	call := llm.Message{Role: llm.RoleAssistant, Content: []llm.Block{
		llm.Text("Let me look."),
		{Type: llm.BlockToolCall, ToolCall: &llm.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"plan.md"}`}},
	}}
	if err := sess.AddMessage(
		llm.UserText("Help me ship the parser."),
		call,
		llm.ToolResultMessage(llm.ToolResult{ID: "c1", Name: "read_file", Content: "step one: tokenizer"}),
		llm.AssistantText("The plan starts with the tokenizer."),
		llm.UserText("Do the tokenizer first."),
		llm.AssistantText("Tokenizer is done."),
	); err != nil {
		t.Fatal(err)
	}
	return sess
}

func contextTexts(sess *session.Session) []string {
	var texts []string
	for _, message := range sess.Context() {
		text := message.Text()
		if result := message.Content[0].ToolResult; result != nil {
			text = "result:" + result.Content
		}
		texts = append(texts, text)
	}
	return texts
}

func TestCompactSummarizesOlderMessagesAndKeepsRecent(t *testing.T) {
	ag, model := fakeAgent([]any{llm.Delta{Text: "  Goal: ship the parser. Tokenizer planned.  "}})
	ag.Compaction.KeepTokens = 1 // keep as little as allowed
	sess := longSession(t)
	entriesBefore := len(sess.Entries)

	var events []Event
	err := ag.Compact(context.Background(), sess, "keep file names", func(e Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// The summarizer got one document, no tools, and the extra instructions.
	request := model.requests[0]
	if len(request.Tools) != 0 || len(request.Messages) != 1 {
		t.Fatalf("summary request = %#v", request)
	}
	if !strings.Contains(request.System, "Additional instructions for this summary:\nkeep file names") {
		t.Fatalf("system = %q", request.System)
	}
	document := request.Messages[0].Text()
	for _, want := range []string{
		"[User]\nHelp me ship the parser.",
		"[Assistant called tool read_file]\n{\"path\":\"plan.md\"}",
		"[Tool result]\nstep one: tokenizer",
		"[User]\nDo the tokenizer first.",
	} {
		if !strings.Contains(document, want) {
			t.Fatalf("transcript is missing %q:\n%s", want, document)
		}
	}
	if strings.Contains(document, "Tokenizer is done.") {
		t.Fatalf("transcript includes the message that is kept:\n%s", document)
	}

	want := []string{session.SummaryPrefix + "Goal: ship the parser. Tokenizer planned.", "Tokenizer is done."}
	if got := contextTexts(sess); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("context = %q", got)
	}
	if len(sess.Entries) != entriesBefore+1 || sess.Entries[entriesBefore].Compaction == nil {
		t.Fatalf("log should gain exactly one compaction entry, has %d (was %d)", len(sess.Entries), entriesBefore)
	}
	if len(events) != 1 || events[0].Type != EventCompaction || events[0].Compaction.TokensAfter >= events[0].Compaction.TokensBefore {
		t.Fatalf("events = %#v", events)
	}
}

func TestCompactCarriesThePreviousSummaryForward(t *testing.T) {
	ag, model := fakeAgent(
		[]any{llm.Delta{Text: "first summary"}},
		[]any{llm.Delta{Text: "second summary"}},
	)
	ag.Compaction.KeepTokens = 1
	sess := longSession(t)
	if err := ag.Compact(context.Background(), sess, "", Discard); err != nil {
		t.Fatal(err)
	}
	sess.AddMessage(llm.UserText("Now the parser."), llm.AssistantText("Parser is done."))
	if err := ag.Compact(context.Background(), sess, "", Discard); err != nil {
		t.Fatal(err)
	}

	document := model.requests[1].Messages[0].Text()
	if !strings.Contains(document, "[Summary of the conversation before this point]\nfirst summary") {
		t.Fatalf("second transcript lacks the first summary:\n%s", document)
	}
	if strings.Contains(document, "Help me ship the parser.") {
		t.Fatalf("second transcript re-reads messages already summarized:\n%s", document)
	}
	want := session.SummaryPrefix + "second summary|Parser is done."
	if got := strings.Join(contextTexts(sess), "|"); got != want {
		t.Fatalf("context = %q", got)
	}
}

func TestCompactNeverStartsTheContextWithAToolResult(t *testing.T) {
	ag, _ := fakeAgent([]any{llm.Delta{Text: "summary"}})
	sess := session.New("test")
	call := llm.Message{Role: llm.RoleAssistant, Content: []llm.Block{
		{Type: llm.BlockToolCall, ToolCall: &llm.ToolCall{ID: "c1", Name: "exec", Arguments: `{}`}},
	}}
	big := strings.Repeat("x", 400) // about 100 tokens
	sess.AddMessage(
		llm.UserText("run it"),
		llm.AssistantText("ok"),
		call,
		llm.ToolResultMessage(llm.ToolResult{ID: "c1", Name: "exec", Content: big}),
	)
	// 50 tokens to keep is reached inside the tool result, but the cut moves
	// back to the assistant message that made the call.
	ag.Compaction.KeepTokens = 50
	if err := ag.Compact(context.Background(), sess, "", Discard); err != nil {
		t.Fatal(err)
	}
	context := sess.Context()
	if len(context) != 3 || len(context[1].ToolCalls()) != 1 || context[2].Role != llm.RoleTool {
		t.Fatalf("context = %#v", context)
	}
}

func TestCompactReportsWhenThereIsNothingToCompact(t *testing.T) {
	ag, model := fakeAgent()
	sess := session.New("test")
	sess.AddMessage(llm.UserText("hi"), llm.AssistantText("hello"))

	err := ag.Compact(context.Background(), sess, "", Discard)
	if !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("err = %v", err)
	}
	if len(model.requests) != 0 || len(sess.Entries) != 3 {
		t.Fatalf("a no-op compaction called the model or changed the log")
	}
}

func TestCompactLeavesTheLogAloneWhenSummarizingFails(t *testing.T) {
	for name, response := range map[string][]any{
		"stream error":  {errors.New("overloaded")},
		"empty summary": {llm.Delta{Text: "   "}},
	} {
		t.Run(name, func(t *testing.T) {
			ag, _ := fakeAgent(response)
			ag.Compaction.KeepTokens = 1
			sess := longSession(t)
			before := len(sess.Entries)
			if err := ag.Compact(context.Background(), sess, "", Discard); err == nil {
				t.Fatal("Compact succeeded")
			}
			if len(sess.Entries) != before {
				t.Fatal("a failed compaction changed the log")
			}
		})
	}
}

func TestRunCompactsWhenTheContextFillsUp(t *testing.T) {
	ag, model := fakeAgent(
		// The first call reports a prompt that nearly fills the window.
		[]any{
			llm.Delta{ToolCall: &llm.ToolCallDelta{ID: "c9", Name: "echo", Arguments: `{}`}},
			llm.Delta{Usage: &llm.Usage{InputTokens: 850, OutputTokens: 10}},
		},
		[]any{llm.Delta{Text: "summary of it all"}},
		[]any{llm.Delta{Text: "Finished."}, llm.Delta{Usage: &llm.Usage{InputTokens: 60, OutputTokens: 5}}},
	)
	ag.ContextWindow = 1000
	ag.Compaction.KeepTokens = 1
	ag.Tools = []Tool{echoTool{}}
	sess := longSession(t)
	sess.AddUser("Now check it.")

	var types []string
	err := ag.Run(context.Background(), sess, func(e Event) error {
		types = append(types, string(e.Type))
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(model.requests) != 3 {
		t.Fatalf("model calls = %d, want 3 (turn, summary, turn)", len(model.requests))
	}
	if got := model.requests[0]; len(got.Messages) != 7 || len(got.Tools) != 1 {
		t.Fatalf("first call should see the whole conversation: %d messages", len(got.Messages))
	}
	if got := model.requests[1]; len(got.Tools) != 0 || !strings.Contains(got.Messages[0].Text(), "Help me ship the parser.") {
		t.Fatalf("second call should be the summary request: %#v", got)
	}
	// After compaction the model sees the summary and the call still open.
	final := model.requests[2].Messages
	if len(final) != 3 || final[0].Text() != session.SummaryPrefix+"summary of it all" ||
		len(final[1].ToolCalls()) != 1 || final[2].Content[0].ToolResult.ID != "c9" {
		t.Fatalf("final call context = %#v", final)
	}
	if got := strings.Join(types, " "); got != "usage tool_start tool_done compaction text usage done" {
		t.Fatalf("events = %q", got)
	}
	// The fresh usage is now below the threshold: no further compaction.
	if ag.needsCompaction(sess) {
		t.Fatal("still over the threshold after compaction")
	}
}

func TestRunDoesNotCompactBelowTheThresholdOrWithoutAWindow(t *testing.T) {
	for name, window := range map[string]int{"below threshold": 100000, "unknown window": 0} {
		t.Run(name, func(t *testing.T) {
			ag, model := fakeAgent([]any{llm.Delta{Text: "ok"}})
			ag.ContextWindow = window
			ag.Compaction.KeepTokens = 1
			if err := ag.Run(context.Background(), longSession(t), Discard); err != nil {
				t.Fatal(err)
			}
			if len(model.requests) != 1 {
				t.Fatalf("model calls = %d, want 1", len(model.requests))
			}
		})
	}
}

func TestRunRecoversFromAContextOverflowError(t *testing.T) {
	overflow := errors.New(`sse unexpected status 400 Bad Request: {"error":{"code":"context_length_exceeded"}}`)
	ag, model := fakeAgent(
		[]any{overflow},
		[]any{llm.Delta{Text: "summary"}},
		[]any{llm.Delta{Text: "Recovered."}},
	)
	ag.Compaction.KeepTokens = 1 // window unknown: only the provider's error triggers it
	sess := longSession(t)

	if err := ag.Run(context.Background(), sess, Discard); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(model.requests) != 3 || len(model.requests[2].Messages) != 2 {
		t.Fatalf("calls = %d, retried context = %d messages", len(model.requests), len(model.requests[2].Messages))
	}
	if got := sess.Entries[len(sess.Entries)-1].Message.Text(); got != "Recovered." {
		t.Fatalf("final = %q", got)
	}
}

func TestRunGivesUpAfterOneOverflowRecovery(t *testing.T) {
	overflow := errors.New("prompt is too long: 250000 tokens > 200000 maximum")
	ag, model := fakeAgent([]any{overflow}, []any{llm.Delta{Text: "summary"}}, []any{overflow})
	ag.Compaction.KeepTokens = 1

	err := ag.Run(context.Background(), longSession(t), Discard)
	if err == nil || !strings.Contains(err.Error(), "prompt is too long") {
		t.Fatalf("Run error = %v", err)
	}
	if len(model.requests) != 3 {
		t.Fatalf("model calls = %d, want 3", len(model.requests))
	}
}

func TestRunContinuesWhenAutomaticCompactionFails(t *testing.T) {
	ag, _ := fakeAgent(
		[]any{errors.New("summarizer overloaded")},
		[]any{llm.Delta{Text: "Answered anyway."}},
	)
	ag.ContextWindow = 10 // far too small: every turn is over the threshold
	ag.Compaction.KeepTokens = 1

	var failure string
	err := ag.Run(context.Background(), longSession(t), func(e Event) error {
		if e.Type == EventCompaction && e.Compaction == nil {
			failure = e.Text
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(failure, "summarizer overloaded") {
		t.Fatalf("failure was not reported: %q", failure)
	}
}

func TestCompactCommand(t *testing.T) {
	for prompt, want := range map[string]string{
		"/compact":                     "",
		"  /compact  ":                 "",
		"/compact keep the file names": "keep the file names",
		"/compact\nfocus on decisions": "focus on decisions",
	} {
		if got, ok := CompactCommand(prompt); !ok || got != want {
			t.Errorf("CompactCommand(%q) = %q, %v", prompt, got, ok)
		}
	}
	for _, prompt := range []string{"/compaction", "compact", "please /compact", "/compactly now"} {
		if _, ok := CompactCommand(prompt); ok {
			t.Errorf("CompactCommand(%q) matched", prompt)
		}
	}
}

func TestTruncateKeepsBothEndsAndValidUTF8(t *testing.T) {
	text := strings.Repeat("头", 500) + "MIDDLE" + strings.Repeat("尾", 500)
	got := truncate(text, 100)
	if len(got) > 200 || !strings.HasPrefix(got, "头") || !strings.HasSuffix(got, "尾") ||
		strings.Contains(got, "MIDDLE") || strings.ContainsRune(got, '\uFFFD') || strings.ToValidUTF8(got, "") != got {
		t.Fatalf("truncate = %q", got)
	}
	if truncate("short", 100) != "short" {
		t.Fatal("short text was changed")
	}
}
