package session

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/lsongdev/miya-agents/llm"
)

// legacySession is the single-JSON-file format that preceded the log. Its
// event list, a recording of ACP updates for replay, has no counterpart: the
// log is replayed from its messages.
type legacySession struct {
	AgentName string            `json:"agent_name"`
	Title     string            `json:"title"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Messages  []json.RawMessage `json:"messages"`
}

// migrateLegacy converts <id>.json to a log, once. The original is kept
// beside it with a .bak suffix. It does nothing when there is no legacy file
// or the log already exists.
func migrateLegacy(id string) error {
	old, path := legacyPath(id), sessionPath(id)
	data, err := os.ReadFile(old)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	var legacy legacySession
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	messages, err := decodeMessages(legacy.Messages)
	if err != nil {
		return err
	}

	// The old format kept no per-message times. Entries take the creation
	// time, and the last takes the update time so ordering by recency holds.
	created := legacy.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	entries := []Entry{{Time: created, Type: EntryMeta, Meta: &Meta{AgentName: legacy.AgentName, Title: legacy.Title}}}
	for _, message := range messages {
		entries = append(entries, Entry{Time: created, Type: EntryMessage, Message: &message})
	}
	if !legacy.UpdatedAt.IsZero() {
		entries[len(entries)-1].Time = legacy.UpdatedAt
	}

	// Write the whole log to a temporary file first, so that an interrupted
	// conversion is retried rather than leaving a partial log.
	temp := path + ".tmp"
	os.Remove(temp)
	for i := range entries {
		entries[i].ID = i + 1
		line, err := json.Marshal(entries[i])
		if err != nil {
			return err
		}
		if err := appendLine(temp, line); err != nil {
			return err
		}
	}
	if err := os.Rename(temp, path); err != nil {
		return err
	}
	return os.Rename(old, old+backupExt)
}

// legacyMessage is the OpenAI Chat Completions shape that the legacy format
// used for messages before they became llm.Message.
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
