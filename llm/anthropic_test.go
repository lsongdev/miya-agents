package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/anthropic"
)

func TestAnthropicRequest(t *testing.T) {
	messages := append([]Message(nil), conversation...)
	// A signed thinking block is sent back; the unsigned one above is not.
	messages = append(messages, Message{Role: RoleAssistant, Content: []Block{
		{Type: BlockThinking, Text: "signed", Signature: "sig"},
		Text("welcome"),
	}})

	req := anthropicRequest(&Request{
		Model:    "claude",
		System:   "sys",
		Messages: messages,
		Tools:    []ToolDef{{Name: "read", Description: "read a file", Parameters: map[string]any{"type": "object"}}},
	})
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"model":"claude","max_tokens":4096,"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"what is this?"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]},` +
		`{"role":"assistant","content":[{"type":"text","text":"Checking."},{"type":"tool_use","id":"c1","name":"read","input":{"p":1}},{"type":"tool_use","id":"c2","name":"list","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"one"},{"type":"tool_result","tool_use_id":"c2","content":"boom","is_error":true}]},` +
		`{"role":"user","content":[{"type":"text","text":"thanks"}]},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"signed","signature":"sig"},{"type":"text","text":"welcome"}]}],` +
		`"system":"sys","tools":[{"name":"read","description":"read a file","input_schema":{"type":"object"}}],"stream":true}`
	if string(data) != want {
		t.Fatalf("request =\n%s\nwant\n%s", data, want)
	}
}

func anthropicModel(t *testing.T, events ...string) Model {
	t.Helper()
	return Anthropic(anthropic.NewClient(&anthropic.Configuration{API: sseServer(t, events...).URL}))
}

func TestAnthropicStream(t *testing.T) {
	model := anthropicModel(t,
		"event: message_start\n"+`data: {"type":"message_start","message":{"id":"m1","role":"assistant","model":"claude","usage":{"input_tokens":10,"cache_read_input_tokens":30,"cache_creation_input_tokens":5,"output_tokens":1}}}`,
		"event: content_block_start\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		"event: content_block_delta\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`,
		"event: content_block_delta\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`,
		"event: content_block_stop\n"+`data: {"type":"content_block_stop","index":0}`,
		"event: content_block_start\n"+`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		"event: content_block_delta\n"+`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hi"}}`,
		"event: content_block_start\n"+`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"t1","name":"read","input":{}}}`,
		"event: content_block_delta\n"+`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"p\":"}}`,
		"event: content_block_delta\n"+`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"1}"}}`,
		"event: message_delta\n"+`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
		"event: message_stop\n"+`data: {"type":"message_stop"}`,
	)
	b, err := collect(t, model)
	if err != nil {
		t.Fatal(err)
	}
	blocks := b.Message().Content
	if len(blocks) != 3 ||
		blocks[0] != (Block{Type: BlockThinking, Text: "hmm", Signature: "sig"}) ||
		blocks[1] != (Block{Type: BlockText, Text: "Hi"}) ||
		*blocks[2].ToolCall != (ToolCall{ID: "t1", Name: "read", Arguments: `{"p":1}`}) {
		t.Fatalf("blocks = %#v", blocks)
	}
	if b.StopReason != StopToolUse {
		t.Fatalf("stop reason = %q", b.StopReason)
	}
	want := Usage{InputTokens: 45, OutputTokens: 9, CacheReadTokens: 30, CacheWriteTokens: 5}
	if b.Usage == nil || *b.Usage != want {
		t.Fatalf("usage = %#v, want %#v", b.Usage, want)
	}
}

func TestAnthropicStreamReportsErrors(t *testing.T) {
	t.Run("error event", func(t *testing.T) {
		_, err := collect(t, anthropicModel(t,
			"event: error\n"+`data: {"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`,
		))
		if err == nil || !strings.Contains(err.Error(), "overloaded") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("stream cut before message_stop", func(t *testing.T) {
		_, err := collect(t, anthropicModel(t,
			"event: content_block_delta\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"par"}}`,
		))
		if err == nil {
			t.Fatal("want an error for a truncated stream")
		}
	})
}
