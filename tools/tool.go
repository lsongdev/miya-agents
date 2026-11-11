package tools

import (
	"context"

	"github.com/lsongdev/miya-agents/llm"
)

// Tool is something a model can call: a definition it is shown, and a
// function that runs with the JSON arguments it produced.
type Tool interface {
	Def() llm.ToolDef
	Run(ctx context.Context, args string) string
}

// File is a file a tool hands to the user. Set Data (base64) or URI.
type File struct {
	Name     string
	MimeType string
	Size     int
	Data     string
	URI      string
}

// FileTool is a Tool whose result includes files for the user, in addition to
// the text returned to the model.
type FileTool interface {
	Tool
	RunFiles(ctx context.Context, args string) (string, []File)
}
