package agent

import (
	"encoding/json"
	"strings"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/session"
)

// Record returns a Sink that appends each event to sess's replay log before
// passing it to next, and saves sess when the turn is done.
func Record(sess *session.Session, next Sink) Sink {
	return func(event Event) error {
		if update, ok := acpUpdate(event); ok {
			sess.AppendEvent(update)
		}
		if event.Type == EventDone {
			if err := sess.Save(); err != nil {
				return err
			}
		}
		return next(event)
	}
}

func RecordUserMessage(sess *session.Session, text string) {
	if text == "" {
		return
	}
	sess.AppendEvent(userMessageUpdate(text))
}

// acpUpdate maps an event to the ACP session update that represents it. It
// reports false for events with no ACP counterpart.
func acpUpdate(event Event) (acp.SessionUpdate, bool) {
	switch event.Type {
	case EventText:
		return assistantMessageUpdate(event.Text), event.Text != ""
	case EventThought:
		return thoughtUpdate(event.Text), event.Text != ""
	case EventToolStart:
		if event.Tool != nil {
			return toolCallUpdate(*event.Tool), true
		}
	case EventToolDone:
		if event.Tool != nil {
			return toolCallDoneUpdate(*event.Tool), true
		}
	case EventFile:
		if event.File != nil {
			return fileMessageUpdate(*event.File), true
		}
	case EventUsage:
		if event.Usage != nil {
			return usageUpdate(event), true
		}
	case EventSessionInfo:
		return sessionInfoUpdate(event.Title), event.Title != ""
	}
	return acp.SessionUpdate{}, false
}

func userMessageUpdate(text string) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "user_message_chunk",
		Content:       acp.ContentBlock{Type: "text", Text: text},
	}
}

func assistantMessageUpdate(text string) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "agent_message_chunk",
		Content:       acp.ContentBlock{Type: "text", Text: text},
	}
}

func fileMessageUpdate(event File) acp.SessionUpdate {
	contentType := "resource"
	if strings.HasPrefix(event.MimeType, "image/") {
		contentType = "image"
	} else if strings.HasPrefix(event.MimeType, "audio/") {
		contentType = "audio"
	} else if event.Data == "" && event.URI != "" {
		contentType = "resource_link"
	}
	size := event.Size
	content := acp.ContentBlock{
		Type:     contentType,
		Data:     event.Data,
		MimeType: event.MimeType,
		Name:     event.Name,
		Size:     &size,
	}
	if event.URI != "" {
		content.URI = &event.URI
	}
	if contentType == "resource" && event.Data != "" {
		mimeType := event.MimeType
		resource, err := json.Marshal(acp.BlobResourceContents{
			URI:      event.URI,
			Blob:     event.Data,
			MimeType: &mimeType,
		})
		if err == nil {
			content.Resource = resource
		}
	}
	return acp.SessionUpdate{
		SessionUpdate: "agent_message_chunk",
		Content:       content,
	}
}

func thoughtUpdate(text string) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "agent_thought_chunk",
		Thought:       text,
	}
}

func toolCallUpdate(event ToolEvent) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "tool_call",
		ToolCall:      acpToolCall(event),
	}
}

func toolCallDoneUpdate(event ToolEvent) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate:  "tool_call_update",
		ToolCallUpdate: acpToolCallUpdate(event, toolCallStatus(event)),
	}
}

func sessionInfoUpdate(title string) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "session_info_update",
		SessionInfo:   &acp.SessionInfoUpdate{Title: &title},
	}
}

// usageUpdate reports how much of the context window the last model call
// occupied: its whole prompt plus the response it produced.
func usageUpdate(event Event) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "usage_update",
		Usage: &acp.UsageUpdate{
			Size: uint64(event.ContextWindow),
			Used: uint64(event.Usage.InputTokens + event.Usage.OutputTokens),
		},
	}
}

func toolCallStatus(event ToolEvent) acp.ToolCallStatus {
	if event.Failed {
		return acp.ToolCallFailed
	}
	return acp.ToolCallCompleted
}

func toolTitle(event ToolEvent) string {
	if event.Call.Name == "" {
		return "Tool call"
	}
	return event.Call.Name
}

func acpToolCall(event ToolEvent) *acp.ToolCall {
	return &acp.ToolCall{
		ToolCallID: acp.ToolCallID(event.Call.ID),
		Title:      toolTitle(event),
		Kind:       toolKind(event.Call.Name),
		Status:     acp.ToolCallInProgress,
		Content: []acp.ToolCallContent{{
			Type:    "content",
			Content: &acp.ContentBlock{Type: "text", Text: event.Call.Arguments},
		}},
		RawInput: rawJSONValue(event.Call.Arguments),
	}
}

func acpToolCallUpdate(event ToolEvent, status acp.ToolCallStatus) *acp.ToolCallUpdate {
	return &acp.ToolCallUpdate{
		ToolCallID: acp.ToolCallID(event.Call.ID),
		Status:     &status,
		Content: []acp.ToolCallContent{{
			Type:    "content",
			Content: &acp.ContentBlock{Type: "text", Text: event.Result},
		}},
		RawOutput: rawJSONValue(event.Result),
	}
}

func rawJSONValue(value string) json.RawMessage {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return data
}

func toolKind(name string) acp.ToolKind {
	switch {
	case strings.Contains(name, "read"):
		return acp.ToolKindRead
	case strings.Contains(name, "write"), strings.Contains(name, "append"), strings.Contains(name, "edit"):
		return acp.ToolKindEdit
	case strings.Contains(name, "exec"):
		return acp.ToolKindExecute
	case strings.Contains(name, "search"):
		return acp.ToolKindSearch
	case strings.Contains(name, "fetch"):
		return acp.ToolKindFetch
	default:
		return acp.ToolKindOther
	}
}
