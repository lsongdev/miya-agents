package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/openai"
)

var conversation = []Message{
	{Role: RoleUser, Content: []Block{
		Text("what is this?"),
		{Type: BlockImage, Media: &Media{MimeType: "image/png", Data: "AAAA"}},
	}},
	{Role: RoleAssistant, Content: []Block{
		Thinking("look at it"),
		Text("Checking."),
		{Type: BlockToolCall, ToolCall: &ToolCall{ID: "c1", Name: "read", Arguments: `{"p":1}`}},
		{Type: BlockToolCall, ToolCall: &ToolCall{ID: "c2", Name: "list"}},
	}},
	ToolResultMessage(ToolResult{ID: "c1", Name: "read", Content: "one"}),
	ToolResultMessage(ToolResult{ID: "c2", Name: "list", Content: "boom", IsError: true}),
	UserText("thanks"),
}

func TestOpenAIRequest(t *testing.T) {
	req := openAIRequest(&Request{
		Model:     "m",
		System:    "sys",
		MaxTokens: 100,
		Messages:  conversation,
		Tools:     []ToolDef{{Name: "read", Description: "read a file", Parameters: map[string]any{"type": "object"}}},
	})
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"model":"m","messages":[` +
		`{"role":"system","content":"sys"},` +
		`{"content":[{"type":"text","text":"what is this?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}],"role":"user"},` +
		`{"role":"assistant","content":"Checking.","reasoning_content":"look at it","tool_calls":[` +
		`{"index":0,"id":"c1","type":"function","function":{"name":"read","arguments":"{\"p\":1}"}},` +
		`{"index":1,"id":"c2","type":"function","function":{"name":"list"}}]},` +
		`{"role":"tool","content":"one","tool_call_id":"c1","name":"read"},` +
		`{"role":"tool","content":"boom","tool_call_id":"c2","name":"list"},` +
		`{"role":"user","content":"thanks"}],` +
		`"tools":[{"type":"function","function":{"name":"read","description":"read a file","parameters":{"type":"object"}}}],` +
		`"max_tokens":100,"stream":true,"stream_options":{"include_usage":true}}`
	if string(data) != want {
		t.Fatalf("request =\n%s\nwant\n%s", data, want)
	}
}

func sseServer(t *testing.T, events ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			fmt.Fprint(w, event+"\n\n")
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func collect(t *testing.T, model Model) (*Builder, error) {
	t.Helper()
	var b Builder
	for delta, err := range model.Stream(context.Background(), &Request{Model: "m", Messages: []Message{UserText("hi")}}) {
		if err != nil {
			return &b, err
		}
		b.Add(delta)
	}
	return &b, nil
}

func openAIModel(t *testing.T, events ...string) Model {
	t.Helper()
	client, err := openai.NewClient(&openai.Configuration{API: sseServer(t, events...).URL})
	if err != nil {
		t.Fatal(err)
	}
	return OpenAI(client)
}

func TestOpenAIStream(t *testing.T) {
	model := openAIModel(t,
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"think"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"p\""}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"list","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":1}"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":7,"total_tokens":27,"prompt_tokens_details":{"cached_tokens":5}}}`,
		`data: [DONE]`,
	)
	b, err := collect(t, model)
	if err != nil {
		t.Fatal(err)
	}
	message := b.Message()
	if message.Thinking() != "think" || message.Text() != "Hello" {
		t.Fatalf("message = %#v", message)
	}
	calls := message.ToolCalls()
	if len(calls) != 2 || calls[0] != (ToolCall{ID: "c1", Name: "read", Arguments: `{"p":1}`}) || calls[1] != (ToolCall{ID: "c2", Name: "list", Arguments: `{}`}) {
		t.Fatalf("calls = %#v", calls)
	}
	if b.StopReason != StopToolUse {
		t.Fatalf("stop reason = %q", b.StopReason)
	}
	if b.Usage == nil || *b.Usage != (Usage{InputTokens: 20, OutputTokens: 7, CacheReadTokens: 5}) {
		t.Fatalf("usage = %#v", b.Usage)
	}
}

// Some compatible servers drop the index, or repeat the id on every fragment.
func TestOpenAIStreamToleratesLooseToolCallFragments(t *testing.T) {
	t.Run("no index after the first fragment", func(t *testing.T) {
		b, err := collect(t, openAIModel(t,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"c1","function":{"name":"read"}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"{\"a\":"}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"1}"}}]}}]}`,
			`data: [DONE]`,
		))
		if err != nil {
			t.Fatal(err)
		}
		calls := b.Message().ToolCalls()
		if len(calls) != 1 || calls[0] != (ToolCall{ID: "c1", Name: "read", Arguments: `{"a":1}`}) {
			t.Fatalf("calls = %#v", calls)
		}
	})
	t.Run("id repeated on every fragment", func(t *testing.T) {
		b, err := collect(t, openAIModel(t,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read"}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"arguments":"{}"}}]}}]}`,
			`data: [DONE]`,
		))
		if err != nil {
			t.Fatal(err)
		}
		calls := b.Message().ToolCalls()
		if len(calls) != 1 || calls[0] != (ToolCall{ID: "c1", Name: "read", Arguments: `{}`}) {
			t.Fatalf("calls = %#v", calls)
		}
	})
}

func TestOpenAIStreamReportsErrors(t *testing.T) {
	t.Run("error chunk", func(t *testing.T) {
		_, err := collect(t, openAIModel(t,
			`data: {"choices":[{"index":0,"delta":{"content":"par"}}]}`,
			`data: {"error":{"message":"overloaded","type":"server_error"}}`,
		))
		if err == nil || !strings.Contains(err.Error(), "overloaded") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("stream cut before done", func(t *testing.T) {
		_, err := collect(t, openAIModel(t, `data: {"choices":[{"index":0,"delta":{"content":"par"}}]}`))
		if err == nil {
			t.Fatal("want an error for a truncated stream")
		}
	})
	t.Run("http failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
		}))
		defer server.Close()
		client, _ := openai.NewClient(&openai.Configuration{API: server.URL})
		if _, err := collect(t, OpenAI(client)); err == nil {
			t.Fatal("want an error for a 401")
		}
	})
}

func TestStreamStopsCleanlyWhenConsumerBreaks(t *testing.T) {
	model := openAIModel(t,
		`data: {"choices":[{"index":0,"delta":{"content":"a"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"b"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"c"}}]}`,
		`data: [DONE]`,
	)
	seen := 0
	for _, err := range model.Stream(context.Background(), &Request{Model: "m"}) {
		if err != nil {
			t.Fatal(err)
		}
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("seen = %d", seen)
	}
}
