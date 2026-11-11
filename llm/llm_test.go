package llm

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuilderKeepsBlockOrder(t *testing.T) {
	var b Builder
	for _, delta := range []Delta{
		{Thinking: "plan "},
		{Thinking: "it"},
		{Signature: "sig"},
		{Text: "Reading "},
		{Text: "files."},
		{ToolCall: &ToolCallDelta{Index: 2, ID: "a", Name: "read"}},
		{ToolCall: &ToolCallDelta{Index: 3, ID: "b", Name: "list"}},
		{ToolCall: &ToolCallDelta{Index: 2, Arguments: `{"path":`}},
		{ToolCall: &ToolCallDelta{Index: 2, Arguments: `"x"}`}},
		{StopReason: StopToolUse, Usage: &Usage{InputTokens: 10, OutputTokens: 4}},
	} {
		b.Add(delta)
	}

	want := Message{Role: RoleAssistant, Content: []Block{
		{Type: BlockThinking, Text: "plan it", Signature: "sig"},
		{Type: BlockText, Text: "Reading files."},
		{Type: BlockToolCall, ToolCall: &ToolCall{ID: "a", Name: "read", Arguments: `{"path":"x"}`}},
		{Type: BlockToolCall, ToolCall: &ToolCall{ID: "b", Name: "list"}},
	}}
	if got := b.Message(); !reflect.DeepEqual(got, want) {
		t.Fatalf("message = %#v", got)
	}
	if b.StopReason != StopToolUse || b.Usage == nil || b.Usage.InputTokens != 10 {
		t.Fatalf("stop = %q usage = %#v", b.StopReason, b.Usage)
	}
}

func TestBuilderStartsNewThinkingBlockAfterSignature(t *testing.T) {
	var b Builder
	b.Add(Delta{Thinking: "one"})
	b.Add(Delta{Signature: "s1"})
	b.Add(Delta{Thinking: "two"})

	blocks := b.Message().Content
	if len(blocks) != 2 || blocks[0].Signature != "s1" || blocks[1].Text != "two" || blocks[1].Signature != "" {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestMessageJSONRoundTrip(t *testing.T) {
	message := Message{Role: RoleAssistant, Content: []Block{
		Thinking("hm"),
		Text("ok"),
		{Type: BlockToolCall, ToolCall: &ToolCall{ID: "c1", Name: "exec", Arguments: `{"command":"ls"}`}},
	}}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, message) {
		t.Fatalf("round trip = %#v", got)
	}
	if got.Text() != "ok" || got.Thinking() != "hm" || len(got.ToolCalls()) != 1 {
		t.Fatalf("accessors: %q %q %v", got.Text(), got.Thinking(), got.ToolCalls())
	}
}
