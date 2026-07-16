package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadStreamableHTTPResponseReturnsJSONResponse(t *testing.T) {
	input := strings.NewReader("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n")

	got, err := readStreamableHTTPResponse(input)
	if err != nil {
		t.Fatalf("readStreamableHTTPResponse: %v", err)
	}
	want := `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
	if string(got) != want {
		t.Fatalf("response = %s, want %s", got, want)
	}
}

func TestReadStreamableHTTPResponseIgnoresNonMessageEvents(t *testing.T) {
	input := strings.NewReader("event: ping\ndata: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"ok\":true}}\n\n")

	got, err := readStreamableHTTPResponse(input)
	if err != nil {
		t.Fatalf("readStreamableHTTPResponse: %v", err)
	}
	want := `{"jsonrpc":"2.0","id":2,"result":{"ok":true}}`
	if string(got) != want {
		t.Fatalf("response = %s, want %s", got, want)
	}
}

func TestStreamableHTTPTransportHeadersAndSession(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got, want := r.Header.Get("Accept"), "application/json, text/event-stream"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("MCP-Protocol-Version"), streamableHTTPProtocolVersion; got != want {
			t.Fatalf("MCP-Protocol-Version = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("X-Test"), "yes"; got != want {
			t.Fatalf("X-Test = %q, want %q", got, want)
		}
		if calls == 1 {
			w.Header().Set("Mcp-Session-Id", "session-1")
		} else if got, want := r.Header.Get("Mcp-Session-Id"), "session-1"; got != want {
			t.Fatalf("Mcp-Session-Id = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"))
	}))
	defer server.Close()

	transport := NewStreamableHTTPTransport(server.URL, map[string]string{"X-Test": "yes"})
	msg := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}
	for i := 0; i < 2; i++ {
		if err := transport.Send(msg); err != nil {
			t.Fatalf("Send %d: %v", i+1, err)
		}
		data, err := transport.Recv()
		if err != nil {
			t.Fatalf("Recv %d: %v", i+1, err)
		}
		var resp struct {
			Result map[string]bool `json:"result"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if !resp.Result["ok"] {
			t.Fatalf("response ok = false")
		}
	}
}
