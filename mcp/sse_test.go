package mcp

import (
	"strings"
	"testing"
)

func TestReadSSEEndpoint(t *testing.T) {
	endpoint, err := readSSEEndpoint(strings.NewReader("event: endpoint\ndata: /messages?session=abc\n\n"))
	if err != nil {
		t.Fatalf("readSSEEndpoint error: %v", err)
	}
	if endpoint != "/messages?session=abc" {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestResolveSSEEndpoint(t *testing.T) {
	got, err := resolveSSEEndpoint("https://example.com/sse", "/messages?session=abc")
	if err != nil {
		t.Fatalf("resolveSSEEndpoint error: %v", err)
	}
	if got != "https://example.com/messages?session=abc" {
		t.Fatalf("url = %q", got)
	}
}

func TestResolveRelativeSSEEndpoint(t *testing.T) {
	got, err := resolveSSEEndpoint("https://example.com/mcp/sse", "messages?session=abc")
	if err != nil {
		t.Fatalf("resolveSSEEndpoint error: %v", err)
	}
	if got != "https://example.com/mcp/messages?session=abc" {
		t.Fatalf("url = %q", got)
	}
}
