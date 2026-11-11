// Package llm is the neutral model interface used by the agent loop and the
// session format. It expresses what an agent sends to and receives from a
// model as content blocks, so that OpenAI Chat Completions and Anthropic
// Messages both map onto it without loss.
//
// It is not a wire format. The openai and anthropic packages own the wire
// types; the adapters in this package translate to and from them.
package llm

import (
	"context"
	"iter"
	"strings"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	// RoleTool marks a message that carries only tool results.
	RoleTool Role = "tool"
)

type BlockType string

const (
	BlockText       BlockType = "text"
	BlockThinking   BlockType = "thinking"
	BlockImage      BlockType = "image"
	BlockToolCall   BlockType = "tool_call"
	BlockToolResult BlockType = "tool_result"
)

// Block is one piece of message content. Type selects which fields are set.
type Block struct {
	Type BlockType `json:"type"`
	// Text holds the content of text and thinking blocks.
	Text string `json:"text,omitempty"`
	// Signature is the provider's opaque proof for a thinking block. It must
	// be sent back unchanged with the block it came with.
	Signature  string      `json:"signature,omitempty"`
	ToolCall   *ToolCall   `json:"tool_call,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
	Media      *Media      `json:"media,omitempty"`
}

// ToolCall is a model's request to run a tool.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Arguments is the raw JSON object produced by the model.
	Arguments string `json:"arguments,omitempty"`
}

// ToolResult is the outcome of one ToolCall.
type ToolResult struct {
	ID      string `json:"id"` // the ToolCall.ID this answers
	Name    string `json:"name,omitempty"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Media is inline or referenced binary content. Set Data or URL.
type Media struct {
	MimeType string `json:"mime_type,omitempty"`
	Data     string `json:"data,omitempty"` // base64
	URL      string `json:"url,omitempty"`
}

type Message struct {
	Role    Role    `json:"role"`
	Content []Block `json:"content"`
}

func Text(text string) Block     { return Block{Type: BlockText, Text: text} }
func Thinking(text string) Block { return Block{Type: BlockThinking, Text: text} }

// UserText returns a user message with a single text block.
func UserText(text string) Message {
	return Message{Role: RoleUser, Content: []Block{Text(text)}}
}

// AssistantText returns an assistant message with a single text block.
func AssistantText(text string) Message {
	return Message{Role: RoleAssistant, Content: []Block{Text(text)}}
}

// ToolResultMessage returns a tool message answering one tool call.
func ToolResultMessage(result ToolResult) Message {
	return Message{Role: RoleTool, Content: []Block{{Type: BlockToolResult, ToolResult: &result}}}
}

// Text returns the message's text blocks joined together.
func (m Message) Text() string { return m.join(BlockText) }

// Thinking returns the message's thinking blocks joined together.
func (m Message) Thinking() string { return m.join(BlockThinking) }

func (m Message) join(blockType BlockType) string {
	var sb strings.Builder
	for _, block := range m.Content {
		if block.Type == blockType {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}

// ToolCalls returns the tool calls in the message, in order.
func (m Message) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, block := range m.Content {
		if block.Type == BlockToolCall && block.ToolCall != nil {
			calls = append(calls, *block.ToolCall)
		}
	}
	return calls
}

// ToolDef describes a tool the model may call.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
}

// Request is one model call.
type Request struct {
	Model string
	// System is the system prompt. It is a request field, not a message.
	System   string
	Messages []Message
	Tools    []ToolDef
	// MaxTokens limits the response. Zero leaves the choice to the adapter.
	MaxTokens int
}

type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
)

// Usage is the token accounting for one model call.
type Usage struct {
	// InputTokens is the whole prompt, including tokens served from cache.
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// CacheReadTokens is the part of InputTokens served from cache.
	CacheReadTokens int `json:"cache_read_tokens,omitempty"`
	// CacheWriteTokens is the part of InputTokens written to cache.
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// Delta is one increment of a streamed response. Any subset of its fields may
// be set.
type Delta struct {
	Text     string
	Thinking string
	// Signature belongs to the thinking block currently being streamed.
	Signature string
	ToolCall  *ToolCallDelta
	// StopReason and Usage arrive at or near the end of the stream.
	StopReason StopReason
	Usage      *Usage
}

// ToolCallDelta is an increment of one tool call. Index identifies the call
// within the response; ID, Name and Arguments are appended to what has been
// received for that index so far.
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// Model is the only capability the agent loop needs from a provider.
//
// Stream yields the response increments in order. A non-nil error ends the
// stream. Stopping iteration early releases the underlying connection.
type Model interface {
	Stream(ctx context.Context, req *Request) iter.Seq2[Delta, error]
}

// ModelFunc adapts a function to Model.
type ModelFunc func(ctx context.Context, req *Request) iter.Seq2[Delta, error]

func (f ModelFunc) Stream(ctx context.Context, req *Request) iter.Seq2[Delta, error] {
	return f(ctx, req)
}

// Builder assembles streamed deltas into a complete assistant message,
// keeping blocks in the order the model produced them.
type Builder struct {
	blocks     []Block
	calls      map[int]int // ToolCallDelta.Index -> position in blocks
	StopReason StopReason
	Usage      *Usage
}

func (b *Builder) Add(delta Delta) {
	if delta.Thinking != "" {
		b.appendText(BlockThinking, delta.Thinking)
	}
	if delta.Signature != "" {
		if last := len(b.blocks) - 1; last >= 0 && b.blocks[last].Type == BlockThinking {
			b.blocks[last].Signature += delta.Signature
		} else {
			b.blocks = append(b.blocks, Block{Type: BlockThinking, Signature: delta.Signature})
		}
	}
	if delta.Text != "" {
		b.appendText(BlockText, delta.Text)
	}
	if call := delta.ToolCall; call != nil {
		position, ok := b.calls[call.Index]
		if !ok {
			if b.calls == nil {
				b.calls = make(map[int]int)
			}
			position = len(b.blocks)
			b.calls[call.Index] = position
			b.blocks = append(b.blocks, Block{Type: BlockToolCall, ToolCall: &ToolCall{}})
		}
		target := b.blocks[position].ToolCall
		target.ID += call.ID
		target.Name += call.Name
		target.Arguments += call.Arguments
	}
	if delta.StopReason != "" {
		b.StopReason = delta.StopReason
	}
	if delta.Usage != nil {
		b.Usage = delta.Usage
	}
}

func (b *Builder) appendText(blockType BlockType, text string) {
	// A signed thinking block is complete; later thinking starts a new one.
	if last := len(b.blocks) - 1; last >= 0 && b.blocks[last].Type == blockType && b.blocks[last].Signature == "" {
		b.blocks[last].Text += text
		return
	}
	b.blocks = append(b.blocks, Block{Type: blockType, Text: text})
}

// Message returns the assembled assistant message.
func (b *Builder) Message() Message {
	return Message{Role: RoleAssistant, Content: b.blocks}
}
