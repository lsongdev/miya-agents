package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsongdev/miya-agents/llm"
)

const AttachFileDefaultMaxSize = 256 * 1024

type AttachFileTool struct {
	Workspace string
	MaxInline int
}

func (t *AttachFileTool) Def() llm.ToolDef {
	return llm.ToolDef{
		Name:        "attach_file",
		Description: "Attach a file to the ACP conversation. Small files are sent inline as base64; larger files are sent as a file URL.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file. Relative paths are resolved from the workspace root.",
				},
				"mimeType": map[string]any{
					"type":        "string",
					"description": "Optional MIME type override, e.g. image/png or application/pdf.",
				},
				"maxInlineBytes": map[string]any{
					"type":        "integer",
					"description": "Optional inline limit. Defaults to 262144 bytes.",
				},
			},
			"required": []string{"path"},
		},
	}
}

// Run attaches the file and returns only the text shown to the model.
func (t *AttachFileTool) Run(ctx context.Context, args string) string {
	text, _ := t.RunFiles(ctx, args)
	return text
}

// RunFiles implements FileTool.
func (t *AttachFileTool) RunFiles(ctx context.Context, args string) (string, []llm.Media) {
	file, err := t.attach(args)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	if file.Data != "" {
		return fmt.Sprintf("Attached %s (%s, %d bytes) inline.", file.Name, file.MimeType, file.Size), []llm.Media{file}
	}
	return fmt.Sprintf("Attached %s (%s, %d bytes) as %s.", file.Name, file.MimeType, file.Size, file.URL), []llm.Media{file}
}

func (t *AttachFileTool) attach(args string) (llm.Media, error) {
	var input struct {
		Path           string `json:"path"`
		MimeType       string `json:"mimeType,omitempty"`
		MaxInlineBytes int    `json:"maxInlineBytes,omitempty"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return llm.Media{}, fmt.Errorf("failed to parse arguments: %v", err)
	}
	if strings.TrimSpace(input.Path) == "" {
		return llm.Media{}, errors.New("path is required")
	}

	path := fileResolvePath(input.Path, t.Workspace)
	resolvedPath := fileAbsPath(path)
	if t.Workspace != "" && !isPathWithinWorkspace(resolvedPath, t.Workspace) {
		return llm.Media{}, fmt.Errorf("file %q is outside workspace %q", resolvedPath, t.Workspace)
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return llm.Media{}, fmt.Errorf("failed to stat file: %s: %v", fileFormatPath(input.Path, resolvedPath), err)
	}
	if info.IsDir() {
		return llm.Media{}, fmt.Errorf("path is a directory, not a file: %s", fileFormatPath(input.Path, resolvedPath))
	}
	if info.Size() > int64(^uint(0)>>1) {
		return llm.Media{}, fmt.Errorf("file is too large to attach: %s", fileFormatPath(input.Path, resolvedPath))
	}

	maxInline := input.MaxInlineBytes
	if maxInline <= 0 {
		maxInline = t.MaxInline
	}
	if maxInline <= 0 {
		maxInline = AttachFileDefaultMaxSize
	}
	mimeType := strings.TrimSpace(input.MimeType)
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(resolvedPath)))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	file := llm.Media{
		Name:     filepath.Base(resolvedPath),
		MimeType: mimeType,
		Size:     int(info.Size()),
		URL:      fileURI(resolvedPath),
	}
	if file.Size <= maxInline {
		data, err := os.ReadFile(resolvedPath)
		if err != nil {
			return llm.Media{}, fmt.Errorf("failed to read file: %s: %v", fileFormatPath(input.Path, resolvedPath), err)
		}
		file.Data = base64.StdEncoding.EncodeToString(data)
	}
	return file, nil
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
