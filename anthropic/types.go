package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Request is an Anthropic Messages API request.
type Request struct {
	Model         string    `json:"model"`
	MaxTokens     int       `json:"max_tokens"`
	Messages      []Message `json:"messages"`
	System        string    `json:"system,omitempty"`
	Stream        bool      `json:"stream,omitempty"`
	Temperature   *float64  `json:"temperature,omitempty"`
	TopP          *float64  `json:"top_p,omitempty"`
	StopSequences []string  `json:"stop_sequences,omitempty"`
}

func (r *Request) UnmarshalJSON(data []byte) error {
	type Alias Request
	aux := &struct {
		System json.RawMessage `json:"system,omitempty"`
		*Alias
	}{Alias: (*Alias)(r)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if len(aux.System) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(aux.System, &s); err == nil {
		r.System = s
		return nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(aux.System, &blocks); err != nil {
		return fmt.Errorf("system must be a string or array of text blocks: %v", err)
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" {
			texts = append(texts, b.Text)
		}
	}
	r.System = strings.Join(texts, "\n")
	return nil
}

// Message is a single message in an Anthropic request.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	type Alias Message
	aux := &struct {
		Content json.RawMessage `json:"content"`
		*Alias
	}{Alias: (*Alias)(m)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if len(aux.Content) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(aux.Content, &s); err == nil {
		m.Content = s
		return nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(aux.Content, &blocks); err != nil {
		return fmt.Errorf("content must be a string or array of content blocks: %v", err)
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" {
			texts = append(texts, b.Text)
		}
	}
	m.Content = strings.Join(texts, "\n")
	return nil
}

// Response is a non-streaming Anthropic Messages API response.
type Response struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	Usage        Usage          `json:"usage"`
	StopReason   string         `json:"stop_reason,omitempty"`
	StopSequence string         `json:"stop_sequence,omitempty"`
}

// ContentBlock is a block of content in an Anthropic response.
type ContentBlock struct {
	Type     string `json:"type"` // "text", "thinking", "redacted_thinking", "tool_use"
	Text     string `json:"text"`
	Thinking string `json:"thinking,omitempty"`
}

// Usage contains token usage from Anthropic.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// APIError represents an error returned by the Anthropic API.
type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Event is a single SSE event in an Anthropic streaming response.
type Event struct {
	Type         string          `json:"type"`
	Index        *int            `json:"index,omitempty"`
	Message      *MessageStart   `json:"message,omitempty"`
	ContentBlock *ContentBlock   `json:"content_block,omitempty"`
	Delta        json.RawMessage `json:"delta,omitempty"`
	Usage        *Usage          `json:"usage,omitempty"`
	Error        *APIError       `json:"error,omitempty"`
}

// MessageStart is sent at the beginning of a streamed message.
type MessageStart struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Role  string `json:"role"`
	Model string `json:"model"`
	Usage Usage  `json:"usage"`
}

// Delta represents the incremental payload inside a content_block_delta event.
type Delta struct {
	Type       string `json:"type"` // "text_delta", "thinking_delta", "signature_delta"
	Text       string `json:"text,omitempty"`
	Thinking   string `json:"thinking,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}
