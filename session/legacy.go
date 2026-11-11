package session

import (
	"encoding/json"
	"fmt"

	"github.com/lsongdev/miya-agents/llm"
)

// legacyMessage is the OpenAI Chat Completions shape that session files used
// before messages became llm.Message.
type legacyMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ReasoningContent string          `json:"reasoning_content"`
	ToolCalls        []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
}

// decodeMessages reads stored messages in either the current or the legacy
// shape. The two are told apart by content: a block array now, a string (or
// nothing) before.
func decodeMessages(raw []json.RawMessage) ([]llm.Message, error) {
	out := make([]llm.Message, 0, len(raw))
	for i, data := range raw {
		var legacy legacyMessage
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, fmt.Errorf("message %d: %w", i, err)
		}
		if len(legacy.Content) > 0 && legacy.Content[0] == '[' {
			var message llm.Message
			if err := json.Unmarshal(data, &message); err != nil {
				return nil, fmt.Errorf("message %d: %w", i, err)
			}
			out = append(out, message)
			continue
		}
		if message, ok := legacy.convert(); ok {
			out = append(out, message)
		}
	}
	return out, nil
}

// convert reports false for messages with no place in the current format:
// system messages, because the system prompt is now supplied per request.
func (m *legacyMessage) convert() (llm.Message, bool) {
	var text string
	_ = json.Unmarshal(m.Content, &text)

	switch m.Role {
	case "user":
		return llm.UserText(text), true
	case "tool":
		return llm.ToolResultMessage(llm.ToolResult{ID: m.ToolCallID, Name: m.Name, Content: text}), true
	case "assistant":
		message := llm.Message{Role: llm.RoleAssistant}
		if m.ReasoningContent != "" {
			message.Content = append(message.Content, llm.Thinking(m.ReasoningContent))
		}
		if text != "" {
			message.Content = append(message.Content, llm.Text(text))
		}
		for _, call := range m.ToolCalls {
			message.Content = append(message.Content, llm.Block{
				Type:     llm.BlockToolCall,
				ToolCall: &llm.ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments},
			})
		}
		return message, len(message.Content) > 0
	}
	return llm.Message{}, false
}
