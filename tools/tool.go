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

// FileTool is a Tool whose result includes files for the user, in addition to
// the text returned to the model.
type FileTool interface {
	Tool
	RunFiles(ctx context.Context, args string) (string, []llm.Media)
}
