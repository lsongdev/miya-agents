package tools

import (
	"context"
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

	text, files := tool.RunFiles(context.Background(), `{"path":"hello.txt"}`)

	if len(files) != 1 {
		t.Fatalf("files = %d, text = %q", len(files), text)
	}
	file := files[0]
	if file.Name != "hello.txt" || file.Size != 5 || file.Data != "aGVsbG8=" {
		t.Fatalf("file = %#v", file)
	}
	if !strings.HasPrefix(file.URI, "file://") {
		t.Fatalf("uri = %q", file.URI)
	}
	if file.MimeType != "text/plain; charset=utf-8" && file.MimeType != "text/plain" {
		t.Fatalf("mime = %q", file.MimeType)
	}
	// The model sees a description, never the file's bytes.
	if !strings.Contains(text, "Attached hello.txt") || strings.Contains(text, file.Data) {
		t.Fatalf("text = %q", text)
	}
}

func TestAttachFileToolLinksLargeFile(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "large.bin")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 20)), 0644); err != nil {
		t.Fatal(err)
	}
	tool := &AttachFileTool{Workspace: workspace, MaxInline: 5}

	text, files := tool.RunFiles(context.Background(), `{"path":"large.bin"}`)

	if len(files) != 1 || files[0].Data != "" {
		t.Fatalf("large file should be linked, not inlined: %#v", files)
	}
	if !strings.HasPrefix(files[0].URI, "file://") || !strings.Contains(text, files[0].URI) {
		t.Fatalf("uri = %q, text = %q", files[0].URI, text)
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
