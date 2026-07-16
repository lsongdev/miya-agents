package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachFileToolInlinesSmallFile(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	tool := &AttachFileTool{Workspace: workspace, MaxInline: 100}

	got := tool.Run(context.Background(), `{"path":"hello.txt"}`)

	var result AttachFileResult
	if err := json.Unmarshal([]byte(got), &result); err != nil {
		t.Fatalf("result is not JSON: %s", got)
	}
	if result.Type != AttachFileEventType {
		t.Fatalf("type = %q", result.Type)
	}
	if result.Data == "" {
		t.Fatal("missing inline data")
	}
	if !strings.HasPrefix(result.URI, "file://") {
		t.Fatalf("uri = %q", result.URI)
	}
	if result.MimeType != "text/plain; charset=utf-8" && result.MimeType != "text/plain" {
		t.Fatalf("mime = %q", result.MimeType)
	}
}

func TestAttachFileToolLinksLargeFile(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "large.bin")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 20)), 0644); err != nil {
		t.Fatal(err)
	}
	tool := &AttachFileTool{Workspace: workspace, MaxInline: 5}

	got := tool.Run(context.Background(), `{"path":"large.bin"}`)

	var result AttachFileResult
	if err := json.Unmarshal([]byte(got), &result); err != nil {
		t.Fatalf("result is not JSON: %s", got)
	}
	if result.Data != "" {
		t.Fatal("large file should not be inlined")
	}
	if !strings.HasPrefix(result.URI, "file://") {
		t.Fatalf("uri = %q", result.URI)
	}
}

func TestAttachFileToolRejectsOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	tool := &AttachFileTool{Workspace: workspace}

	got := tool.Run(context.Background(), `{"path":"`+outside+`"}`)

	if !strings.Contains(got, "outside workspace") {
		t.Fatalf("Run = %q", got)
	}
}
