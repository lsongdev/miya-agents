package agent

import (
	"context"
	"encoding/json"
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
	Tools     []Tool
}

func (a *Agent) tool(name string) (Tool, bool) {
	for _, tool := range a.Tools {
		if tool.Def().Name == name {
			return tool, true
		}
	}
	return nil, false
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
func (a *Agent) Run(ctx context.Context, sess *session.Session, sink EventSink) error {
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
			if err := sink.Usage(UsageEvent{}); err != nil {
				return err
			}
			return sink.Done()
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
func (a *Agent) respond(ctx context.Context, sess *session.Session, sink EventSink) (llm.Message, error) {
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
			if err := sink.ThoughtDelta(delta.Thinking); err != nil {
				return llm.Message{}, err
			}
		}
		if delta.Text != "" {
			if err := sink.AssistantDelta(delta.Text); err != nil {
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
	return response, nil
}

// call runs one tool call and reports it to sink. A tool failure is returned
// to the model as the result; only a sink failure is returned as an error.
func (a *Agent) call(ctx context.Context, call llm.ToolCall, sink EventSink) (llm.ToolResult, error) {
	event := ToolCallEvent{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: "in_progress", Call: call}
	if err := sink.ToolCallStart(event); err != nil {
		return llm.ToolResult{}, err
	}

	result := llm.ToolResult{ID: call.ID, Name: call.Name}
	event.Status = "completed"
	if tool, ok := a.tool(call.Name); ok {
		result.Content = tool.Run(ctx, call.Arguments)
		if call.Name == "attach_file" {
			attachmentResult, emitted, err := emitAttachedFileResult(sink, result.Content)
			if err != nil {
				result.Content = fmt.Sprintf("Error: %v", err)
			} else if emitted {
				result.Content = attachmentResult
			}
		}
	} else {
		event.Status = "failed"
		result.Content = fmt.Sprintf("Error: unknown tool '%s'", call.Name)
		result.IsError = true
	}

	event.Result = result.Content
	if err := sink.ToolCallDone(event); err != nil {
		return llm.ToolResult{}, err
	}
	return result, nil
}

func emitAttachedFileResult(sink EventSink, result string) (string, bool, error) {
	var attachment tools.AttachFileResult
	if err := json.Unmarshal([]byte(result), &attachment); err != nil {
		return result, false, nil
	}
	if attachment.Type != tools.AttachFileEventType {
		return result, false, nil
	}
	if attachment.Data == "" && attachment.URI == "" {
		return "", false, fmt.Errorf("attachment has neither inline data nor URI")
	}
	if err := sink.AssistantFile(FileEvent{
		Name:     attachment.Name,
		MimeType: attachment.MimeType,
		Size:     attachment.Size,
		Data:     attachment.Data,
		URI:      attachment.URI,
	}); err != nil {
		return "", false, err
	}
	if attachment.Data != "" {
		return fmt.Sprintf("Attached %s (%s, %d bytes) inline.", attachment.Name, attachment.MimeType, attachment.Size), true, nil
	}
	return fmt.Sprintf("Attached %s (%s, %d bytes) as %s.", attachment.Name, attachment.MimeType, attachment.Size, attachment.URI), true, nil
}
