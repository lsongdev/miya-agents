package agent

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/session"
)

type recordingSink struct {
	sess *session.Session
	next EventSink
}

func NewRecordingSink(sess *session.Session, next EventSink) EventSink {
	return &recordingSink{sess: sess, next: next}
}

func RecordUserMessage(sess *session.Session, text string) {
	if text == "" {
		return
	}
	sess.AppendEvent(userMessageUpdate(text))
}

func (s *recordingSink) AssistantDelta(text string) error {
	if text == "" {
		return nil
	}
	s.sess.AppendEvent(assistantMessageUpdate(text))
	return s.next.AssistantDelta(text)
}

func (s *recordingSink) ThoughtDelta(text string) error {
	if text == "" {
		return nil
	}
	s.sess.AppendEvent(thoughtUpdate(text))
	return s.next.ThoughtDelta(text)
}

func (s *recordingSink) ToolCallStart(event ToolCallEvent) error {
	s.sess.AppendEvent(toolCallUpdate(event))
	return s.next.ToolCallStart(event)
}

func (s *recordingSink) ToolCallDone(event ToolCallEvent) error {
	s.sess.AppendEvent(toolCallDoneUpdate(event))
	return s.next.ToolCallDone(event)
}

func (s *recordingSink) SessionInfo(event SessionInfoEvent) error {
	if event.Title == "" {
		return nil
	}
	s.sess.AppendEvent(sessionInfoUpdate(event))
	return s.next.SessionInfo(event)
}

func (s *recordingSink) Usage(event UsageEvent) error {
	s.sess.AppendEvent(usageUpdate(event))
	return s.next.Usage(event)
}

func (s *recordingSink) Done() error {
	return s.next.Done()
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

func thoughtUpdate(text string) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "agent_thought_chunk",
		Thought:       text,
	}
}

func toolCallUpdate(event ToolCallEvent) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "tool_call",
		ToolCall:      acpToolCall(event),
	}
}

func toolCallDoneUpdate(event ToolCallEvent) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate:  "tool_call_update",
		ToolCallUpdate: acpToolCallUpdate(event, toolCallStatus(event)),
	}
}

func sessionInfoUpdate(event SessionInfoEvent) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "session_info_update",
		SessionInfo:   &acp.SessionInfoUpdate{Title: &event.Title},
	}
}

func usageUpdate(event UsageEvent) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionUpdate: "usage_update",
		Usage:         &acp.UsageUpdate{},
	}
}

func toolCallStatus(event ToolCallEvent) acp.ToolCallStatus {
	if event.Status == "failed" {
		return acp.ToolCallFailed
	}
	return acp.ToolCallCompleted
}

func toolTitle(event ToolCallEvent) string {
	if event.Name == "" {
		return "Tool call"
	}
	return event.Name
}

func acpToolCall(event ToolCallEvent) *acp.ToolCall {
	return &acp.ToolCall{
		ToolCallID: acp.ToolCallID(event.ID),
		Title:      toolTitle(event),
		Kind:       toolKind(event.Name),
		Status:     acp.ToolCallInProgress,
		Content: []acp.ToolCallContent{{
			Type:    "content",
			Content: &acp.ContentBlock{Type: "text", Text: event.Arguments},
		}},
		RawInput: json.RawMessage(strconv.Quote(event.Arguments)),
	}
}

func acpToolCallUpdate(event ToolCallEvent, status acp.ToolCallStatus) *acp.ToolCallUpdate {
	return &acp.ToolCallUpdate{
		ToolCallID: acp.ToolCallID(event.ID),
		Status:     &status,
		Content: []acp.ToolCallContent{{
			Type:    "content",
			Content: &acp.ContentBlock{Type: "text", Text: event.Result},
		}},
		RawOutput: json.RawMessage(strconv.Quote(event.Result)),
	}
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
