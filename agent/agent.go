package agent

import (
	"context"
	"fmt"

	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/session"
	"github.com/lsongdev/miya-agents/tools"
)

// Tool is something the model can call.
type Tool = tools.Tool

// Agent runs a model in a loop, executing the tools it calls until it
// finishes its turn. It holds no per-conversation state; that lives in the
// session passed to Run.
type Agent struct {
	Name string
	// Model streams responses; ModelName is the model it is asked for.
	Model     llm.Model
	ModelName string
	// System is the system prompt sent with every request.
	System string
	// MaxTokens limits each response. Zero leaves the choice to the model adapter.
	MaxTokens int
	// ContextWindow is the model's context size in tokens, or zero if unknown.
	ContextWindow int
	Tools         []Tool
}

// tool returns the named tool, or nil if the agent has none by that name.
func (a *Agent) tool(name string) Tool {
	for _, tool := range a.Tools {
		if tool.Def().Name == name {
			return tool
		}
	}
	return nil
}

func (a *Agent) toolDefs() []llm.ToolDef {
	defs := make([]llm.ToolDef, len(a.Tools))
	for i, tool := range a.Tools {
		defs[i] = tool.Def()
	}
	return defs
}

// Run advances sess until the agent finishes its turn. Persistence is owned by
// the caller; the same loop can therefore run persistent user sessions or
// ephemeral delegated tasks.
func (a *Agent) Run(ctx context.Context, sess *session.Session, sink Sink) error {
	if a.Model == nil {
		return fmt.Errorf("agent has no model")
	}
	for {
		response, err := a.respond(ctx, sess, sink)
		if err != nil {
			return err
		}
		sess.Append(response)

		calls := response.ToolCalls()
		if len(calls) == 0 {
			return sink(Event{Type: EventDone})
		}
		for _, call := range calls {
			result, err := a.call(ctx, call, sink)
			if err != nil {
				return err
			}
			sess.Append(llm.ToolResultMessage(result))
		}
	}
}

// respond makes one model call and returns the complete assistant message,
// forwarding text and thinking to sink as they arrive.
func (a *Agent) respond(ctx context.Context, sess *session.Session, sink Sink) (llm.Message, error) {
	req := &llm.Request{
		Model:     a.ModelName,
		System:    a.System,
		Messages:  sess.Messages,
		Tools:     a.toolDefs(),
		MaxTokens: a.MaxTokens,
	}
	var builder llm.Builder
	received := false
	for delta, err := range a.Model.Stream(ctx, req) {
		if err != nil {
			return llm.Message{}, fmt.Errorf("model stream: %w", err)
		}
		received = true
		builder.Add(delta)
		if delta.Thinking != "" {
			if err := sink(Event{Type: EventThought, Text: delta.Thinking}); err != nil {
				return llm.Message{}, err
			}
		}
		if delta.Text != "" {
			if err := sink(Event{Type: EventText, Text: delta.Text}); err != nil {
				return llm.Message{}, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return llm.Message{}, err
	}
	if !received {
		return llm.Message{}, fmt.Errorf("model stream closed without a response")
	}
	response := builder.Message()
	for _, call := range response.ToolCalls() {
		if call.ID == "" {
			return llm.Message{}, fmt.Errorf("tool call %q is missing an id", call.Name)
		}
	}
	if builder.Usage != nil {
		if err := sink(Event{Type: EventUsage, Usage: builder.Usage, ContextWindow: a.ContextWindow}); err != nil {
			return llm.Message{}, err
		}
	}
	return response, nil
}

// call runs one tool call and reports it to sink. A tool failure is returned
// to the model as the result; only a sink failure is returned as an error.
func (a *Agent) call(ctx context.Context, call llm.ToolCall, sink Sink) (llm.ToolResult, error) {
	if err := sink(Event{Type: EventToolStart, Tool: &ToolEvent{Call: call}}); err != nil {
		return llm.ToolResult{}, err
	}

	result := llm.ToolResult{ID: call.ID, Name: call.Name}
	var files []File
	switch tool := a.tool(call.Name).(type) {
	case tools.FileTool:
		result.Content, files = tool.RunFiles(ctx, call.Arguments)
	case Tool:
		result.Content = tool.Run(ctx, call.Arguments)
	default:
		result.Content = fmt.Sprintf("Error: unknown tool '%s'", call.Name)
		result.IsError = true
	}

	for _, file := range files {
		if err := sink(Event{Type: EventFile, File: &file}); err != nil {
			return llm.ToolResult{}, err
		}
	}
	done := &ToolEvent{Call: call, Result: result.Content, Failed: result.IsError}
	if err := sink(Event{Type: EventToolDone, Tool: done}); err != nil {
		return llm.ToolResult{}, err
	}
	return result, nil
}
