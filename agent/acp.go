package agent

import (
	"encoding/json"
	"strings"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/session"
)

// acpSink returns a Sink that forwards events to an ACP client.
func acpSink(sender acp.SessionUpdateSender) Sink {
	return func(event Event) error {
		if update, ok := acpUpdate(event); ok {
			return sender.Send(update)
		}
		return nil
	}
}

// replaySession sends a client the session's history, as the events that
// would have been reported had it watched the conversation live.
func replaySession(sess *session.Session, contextWindow int, sender acp.SessionUpdateSender) error {
	sink := acpSink(sender)
	var usage *llm.Usage
	for _, entry := range sess.Entries {
		if meta := entry.Meta; meta != nil && meta.Title != "" {
			if err := sink(Event{Type: EventSessionInfo, Title: meta.Title}); err != nil {
				return err
			}
		}
		message := entry.Message
		if message == nil {
			continue
		}
		if entry.Usage != nil {
			usage = entry.Usage
		}
		for _, file := range entry.Files {
			if err := sink(Event{Type: EventFile, File: &file}); err != nil {
				return err
			}
		}
		for _, block := range message.Content {
			var err error
			switch {
			case block.Type == llm.BlockText && message.Role == llm.RoleUser:
				err = sender.Send(userMessageUpdate(block.Text))
			case block.Type == llm.BlockText:
				err = sink(Event{Type: EventText, Text: block.Text})
			case block.Type == llm.BlockThinking:
				err = sink(Event{Type: EventThought, Text: block.Text})
			case block.Type == llm.BlockToolCall && block.ToolCall != nil:
				err = sink(Event{Type: EventToolStart, Tool: &ToolEvent{Call: *block.ToolCall}})
			case block.Type == llm.BlockToolResult && block.ToolResult != nil:
				result := block.ToolResult
				err = sink(Event{Type: EventToolDone, Tool: &ToolEvent{
					Call:   llm.ToolCall{ID: result.ID, Name: result.Name},
					Result: result.Content,
					Failed: result.IsError,
				}})
			}
			if err != nil {
				return err
			}
		}
	}
	// Only the latest usage matters to a client: it is the current fill of
	// the context window.
	if usage != nil {
		return sink(Event{Type: EventUsage, Usage: usage, ContextWindow: contextWindow})
	}
	return nil
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

func fileMessageUpdate(event llm.Media) acp.SessionUpdate {
	contentType := "resource"
	if strings.HasPrefix(event.MimeType, "image/") {
		contentType = "image"
	} else if strings.HasPrefix(event.MimeType, "audio/") {
		contentType = "audio"
	} else if event.Data == "" && event.URL != "" {
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
	if event.URL != "" {
		content.URI = &event.URL
	}
	if contentType == "resource" && event.Data != "" {
		mimeType := event.MimeType
		resource, err := json.Marshal(acp.BlobResourceContents{
			URI:      event.URL,
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
