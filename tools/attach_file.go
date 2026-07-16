package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsongdev/miya-agents/openai"
)

const (
	AttachFileEventType      = "miya.attach_file"
	AttachFileDefaultMaxSize = 256 * 1024
)

type AttachFileResult struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Size     int    `json:"size"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
	Path     string `json:"path"`
}

type AttachFileTool struct {
	Workspace string
	MaxInline int
}

func (t *AttachFileTool) Def() openai.ToolDef {
	return openai.ToolDef{
		Type: "function",
		Function: openai.FunctionDef{
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
		},
	}
}

func (t *AttachFileTool) Run(ctx context.Context, args string) string {
	var input struct {
		Path           string `json:"path"`
		MimeType       string `json:"mimeType,omitempty"`
		MaxInlineBytes int    `json:"maxInlineBytes,omitempty"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return fmt.Sprintf("Error: failed to parse arguments: %v", err)
	}
	if strings.TrimSpace(input.Path) == "" {
		return "Error: path is required"
	}

	path := fileResolvePath(input.Path, t.Workspace)
	resolvedPath := fileAbsPath(path)
	if t.Workspace != "" && !isPathWithinWorkspace(resolvedPath, t.Workspace) {
		return fmt.Sprintf("Error: file %q is outside workspace %q", resolvedPath, t.Workspace)
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return fmt.Sprintf("Error: failed to stat file: %s: %v", fileFormatPath(input.Path, resolvedPath), err)
	}
	if info.IsDir() {
		return fmt.Sprintf("Error: path is a directory, not a file: %s", fileFormatPath(input.Path, resolvedPath))
	}
	if info.Size() > int64(^uint(0)>>1) {
		return fmt.Sprintf("Error: file is too large to attach: %s", fileFormatPath(input.Path, resolvedPath))
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

	result := AttachFileResult{
		Type:     AttachFileEventType,
		Name:     filepath.Base(resolvedPath),
		MimeType: mimeType,
		Size:     int(info.Size()),
		URI:      fileURI(resolvedPath),
		Path:     resolvedPath,
	}
	if int(info.Size()) <= maxInline {
		data, err := os.ReadFile(resolvedPath)
		if err != nil {
			return fmt.Sprintf("Error: failed to read file: %s: %v", fileFormatPath(input.Path, resolvedPath), err)
		}
		result.Data = base64.StdEncoding.EncodeToString(data)
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("Error: failed to encode attachment: %v", err)
	}
	return string(payload)
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
