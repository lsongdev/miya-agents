package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := NewClient(&Configuration{API: "https://api.anthropic.com", APIKey: "test-key"})
	if c.config.API != "https://api.anthropic.com" {
		t.Errorf("API = %q, want https://api.anthropic.com", c.config.API)
	}
	if c.config.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want test-key", c.config.APIKey)
	}
	if c.client == nil {
		t.Error("HTTP client should not be nil")
	}
}

func TestSetHTTPClient(t *testing.T) {
	c := NewClient(&Configuration{API: "https://api.anthropic.com", APIKey: "test-key"})
	customClient := &http.Client{Timeout: 10 * time.Second}
	c.SetHTTPClient(customClient)
	if c.client != customClient {
		t.Error("HTTP client was not set")
	}
}

func TestCreateMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if key := r.Header.Get("x-api-key"); key != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", key)
		}
		if ver := r.Header.Get("anthropic-version"); ver != "2023-06-01" {
			t.Errorf("anthropic-version = %q, want 2023-06-01", ver)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var req Request
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "claude-3" {
			t.Errorf("model = %q, want claude-3", req.Model)
		}

		json.NewEncoder(w).Encode(Response{
			ID:         "msg-abc123",
			Type:       "message",
			Role:       "assistant",
			Model:      "claude-3",
			Content:    []ContentBlock{{Type: "text", Text: "Hello!"}},
			Usage:      Usage{InputTokens: 10, OutputTokens: 5},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	c := NewClient(&Configuration{API: server.URL, APIKey: "test-key"})
	resp, err := c.CreateMessage(context.Background(), &Request{
		Model:     "claude-3",
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "msg-abc123" {
		t.Errorf("ID = %q, want msg-abc123", resp.ID)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello!" {
		t.Errorf("content = %+v, want Hello!", resp.Content)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("input_tokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("output_tokens = %d, want 5", resp.Usage.OutputTokens)
	}
}

func TestCreateMessage_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	c := NewClient(&Configuration{API: server.URL, APIKey: "bad-key"})
	_, err := c.CreateMessage(context.Background(), &Request{
		Model:     "claude-3",
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %q, want 401", err.Error())
	}
}

func TestCreateMessage_ConnectionError(t *testing.T) {
	c := NewClient(&Configuration{API: "http://127.0.0.1:1", APIKey: "test-key"})
	_, err := c.CreateMessage(context.Background(), &Request{
		Model:     "claude-3",
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateMessage_MultipleContentBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Response{
			ID:    "msg-1",
			Type:  "message",
			Role:  "assistant",
			Model: "claude-3",
			Content: []ContentBlock{
				{Type: "thinking", Thinking: "Let me think..."},
				{Type: "text", Text: "Here is my answer"},
			},
			Usage:      Usage{InputTokens: 20, OutputTokens: 10},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	c := NewClient(&Configuration{API: server.URL, APIKey: "test-key"})
	resp, err := c.CreateMessage(context.Background(), &Request{
		Model:     "claude-3",
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: "explain quantum physics"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(resp.Content))
	}
	if resp.Content[0].Thinking != "Let me think..." {
		t.Errorf("thinking = %q, want 'Let me think...'", resp.Content[0].Thinking)
	}
	if resp.Content[1].Text != "Here is my answer" {
		t.Errorf("text = %q, want 'Here is my answer'", resp.Content[1].Text)
	}
}

func TestCreateMessageStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var req Request
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("expected stream=true in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")

		msgStart, _ := json.Marshal(Event{
			Type: "message_start",
			Message: &MessageStart{
				ID: "msg-1", Type: "message", Role: "assistant", Model: "claude-3",
				Usage: Usage{InputTokens: 5},
			},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)

		deltaData, _ := json.Marshal(Delta{Type: "text_delta", Text: "Hel"})
		cbDelta, _ := json.Marshal(Event{Type: "content_block_delta", Index: 0, Delta: deltaData})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", cbDelta)

		deltaData2, _ := json.Marshal(Delta{Type: "text_delta", Text: "lo"})
		cbDelta2, _ := json.Marshal(Event{Type: "content_block_delta", Index: 0, Delta: deltaData2})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", cbDelta2)

		msgDeltaData, _ := json.Marshal(Delta{StopReason: "end_turn"})
		msgDelta, _ := json.Marshal(Event{Type: "message_delta", Delta: msgDeltaData, Usage: &Usage{OutputTokens: 3}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)

		msgStop, _ := json.Marshal(Event{Type: "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer server.Close()

	c := NewClient(&Configuration{API: server.URL, APIKey: "test-key"})
	ms, err := c.CreateMessageStream(context.Background(), &Request{
		Model:     "claude-3",
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var texts []string
	var eventTypes []string
	for event := range ms.Events {
		eventTypes = append(eventTypes, event.Type)
		if event.Type == "content_block_delta" {
			var delta Delta
			if err := json.Unmarshal(event.Delta, &delta); err != nil {
				continue
			}
			if delta.Text != "" {
				texts = append(texts, delta.Text)
			}
		}
	}

	if len(texts) != 2 {
		t.Fatalf("expected 2 text deltas, got %d", len(texts))
	}
	if texts[0] != "Hel" || texts[1] != "lo" {
		t.Errorf("texts = %v, want [Hel lo]", texts)
	}

	expectedTypes := []string{"message_start", "content_block_delta", "content_block_delta", "message_delta", "message_stop"}
	if len(eventTypes) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d", len(expectedTypes), len(eventTypes))
	}
	for i, et := range expectedTypes {
		if eventTypes[i] != et {
			t.Errorf("event[%d].Type = %q, want %q", i, eventTypes[i], et)
		}
	}
}

func TestCreateMessageStream_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		msgStart, _ := json.Marshal(Event{Type: "message_start"})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := NewClient(&Configuration{API: server.URL, APIKey: "test-key"})
	ms, err := c.CreateMessageStream(ctx, &Request{
		Model:     "claude-3",
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	<-ms.Events
	cancel()

	for range ms.Events {
	}
}

func TestCreateMessageStream_ConnectionError(t *testing.T) {
	c := NewClient(&Configuration{API: "http://127.0.0.1:1", APIKey: "test-key"})
	_, err := c.CreateMessageStream(context.Background(), &Request{
		Model:     "claude-3",
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateMessageStream_JSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: content_block_delta\ndata: {invalid json}\n\n")
		fmt.Fprintf(w, "event: message_stop\ndata: {}\n\n")
	}))
	defer server.Close()

	c := NewClient(&Configuration{API: server.URL, APIKey: "test-key"})
	ms, err := c.CreateMessageStream(context.Background(), &Request{
		Model:     "claude-3",
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int
	for range ms.Events {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 event (message_stop), got %d", count)
	}
}

func TestMakeRequest_JSONError(t *testing.T) {
	c := NewClient(&Configuration{API: "http://localhost", APIKey: "test-key"})
	_, err := c.makeRequest(context.Background(), "POST", "/v1/messages", make(chan int))
	if err == nil {
		t.Fatal("expected error for unmarshalable data")
	}
	if !strings.Contains(err.Error(), "json marshal error") {
		t.Errorf("error = %q, want json marshal error", err.Error())
	}
}
