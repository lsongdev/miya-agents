package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

type Client struct {
	config *Configuration
	client *http.Client
}

func NewClient(config *Configuration) (*Client, error) {
	return &Client{config, http.DefaultClient}, nil
}

func (c *Client) SetHTTPClient(client *http.Client) {
	c.client = client
}

func (client *Client) MakeRequest(ctx context.Context, path string, data interface{}) (io.ReadCloser, error) {
	var req *http.Request
	var err error

	if data != nil {
		payload, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("json error: %v", err)
		}
		req, err = http.NewRequestWithContext(ctx, "POST", client.config.API+path, bytes.NewBuffer(payload))
		if err != nil {
			return nil, fmt.Errorf("invalid request: %v", err)
		}
		req.Header.Add("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, "GET", client.config.API+path, nil)
		if err != nil {
			return nil, fmt.Errorf("invalid request: %v", err)
		}
	}

	req.Header.Add("Authorization", "Bearer "+client.config.APIKey)
	res, err := client.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot make request: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(res.Body)
		log.Println(string(data))
		return nil, fmt.Errorf("invalid status code: %s", res.Status)
	}
	return res.Body, nil
}

// Model represents a model in the  API format
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// Models fetches the list of available models from the API
func (client *Client) Models() (models []Model, err error) {
	body, err := client.MakeRequest(context.Background(), "/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %v", err)
	}
	defer body.Close()

	var response struct {
		Data []Model `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}
	return response.Data, nil
}

// MessageBuilder helps accumulate streaming response data.
type MessageBuilder struct {
	message     ChatCompletionMessage
	toolCallMap map[int]*ToolCall
}

// CreateMessageBuilder creates a new message builder.
func CreateMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		toolCallMap: make(map[int]*ToolCall),
	}
}

// Update accumulates data from a stream chunk.
func (b *MessageBuilder) Update(chunk ChatCompletionMessage) {
	// Set role on first update
	if b.message.Role == "" {
		b.message.Role = RoleAssistant
	}

	b.message.ReasoningContent += chunk.ReasoningContent
	b.message.Content += chunk.Content

	// Accumulate tool calls by index
	if chunk.ToolCalls == nil {
		return
	}
	for _, tc := range chunk.ToolCalls {
		idx := tc.Index
		if existing, ok := b.toolCallMap[idx]; ok {
			// Merge into existing tool call
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Type != "" {
				existing.Type = tc.Type
			}
			existing.Function.Name += tc.Function.Name
			existing.Function.Arguments += tc.Function.Arguments
		} else {
			// New tool call
			tcCopy := tc
			b.toolCallMap[idx] = &tcCopy
		}
	}
}

// Build returns the final accumulated message.
func (b *MessageBuilder) Build() ChatCompletionMessage {
	// Convert tool call map to sorted slice
	if len(b.toolCallMap) > 0 {
		indices := make([]int, 0, len(b.toolCallMap))
		for idx := range b.toolCallMap {
			indices = append(indices, idx)
		}
		// Sort indices
		for i := 0; i < len(indices)-1; i++ {
			for j := i + 1; j < len(indices); j++ {
				if indices[i] > indices[j] {
					indices[i], indices[j] = indices[j], indices[i]
				}
			}
		}
		if b.message.ToolCalls == nil {
			b.message.ToolCalls = []ToolCall{}
		}
		for _, idx := range indices {
			tc := *b.toolCallMap[idx]
			tc.Index = 0 // Reset index
			b.message.ToolCalls = append(b.message.ToolCalls, tc)
		}
	}
	return b.message
}

func (resp *ChatCompletionResponse) GetFirstChoice() *ChatCompletionChoice {
	for _, choice := range resp.Choices {
		return &choice
	}
	return nil
}

func (resp *ChatCompletionResponse) GetMessage() *ChatCompletionMessage {
	choice := resp.GetFirstChoice()
	if choice == nil {
		return nil
	}
	if !choice.Delta.IsEmpty() {
		return choice.Delta
	}
	return choice.Message
}

type ChatCompletionChoice struct {
	Index   int                    `json:"index"`
	Message *ChatCompletionMessage `json:"message,omitzero"`
	Delta   *ChatCompletionMessage `json:"delta,omitzero"` // for streaming responses

	LogProbs     any    `json:"logprobs,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	} `json:"image_url,omitempty"`
}

type ChatCompletionMessage struct {
	Role             string `json:"role,omitempty"`              // system, user, assistant, tool
	Content          string `json:"content,omitempty"`           // text content (string or array of parts)
	ReasoningContent string `json:"reasoning_content,omitempty"` // deepseek-reasoner

	// tools calls request
	ToolCalls []ToolCall `json:"tool_calls,omitempty"` // for assistant messages
	// tools results
	ToolCallID string `json:"tool_call_id,omitempty"` // for tool result messages
	Name       string `json:"name,omitempty"`         // tool name for tool results
}

func (m *ChatCompletionMessage) UnmarshalJSON(data []byte) error {
	type Alias ChatCompletionMessage
	aux := &struct {
		Content json.RawMessage `json:"content,omitempty"`
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
	var parts []ContentPart
	if err := json.Unmarshal(aux.Content, &parts); err != nil {
		return fmt.Errorf("content must be a string or array of content parts: %v", err)
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	m.Content = strings.Join(texts, "\n")
	return nil
}

func (m *ChatCompletionMessage) IsEmpty() bool {
	return m.Role == "" && m.Content == "" && m.ReasoningContent == "" && (len(m.ToolCalls) == 0)
}

func (m *ChatCompletionMessage) HasToolCall() bool {
	return len(m.ToolCalls) > 0
}

// UserMessage creates a user message.
func UserMessage(content string) ChatCompletionMessage {
	return ChatCompletionMessage{Role: "user", Content: content}
}

// SystemMessage creates a system message.
func SystemMessage(content string) ChatCompletionMessage {
	return ChatCompletionMessage{Role: "system", Content: content}
}

// AssistantMessage creates an assistant message.
func AssistantMessage(content string) ChatCompletionMessage {
	return ChatCompletionMessage{Role: "assistant", Content: content}
}

// AssistantMessageWithTools creates an assistant message with tool calls.
func AssistantMessageWithTools(content string, toolCalls []ToolCall) ChatCompletionMessage {
	return ChatCompletionMessage{Role: "assistant", Content: content, ToolCalls: toolCalls}
}

// ToolResultMessage creates a tool result message.
func ToolResultMessage(toolCallID, name, content string) ChatCompletionMessage {
	return ChatCompletionMessage{Role: "tool", ToolCallID: toolCallID, Name: name, Content: content}
}

// CreateChatCompletion sends a non-streaming chat completion request.
func (c *Client) CreateChatCompletion(ctx context.Context, request *ChatCompletionRequest) (resp ChatCompletionResponse, err error) {
	body, err := c.MakeRequest(ctx, "/chat/completions", request)
	if err != nil {
		return
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return resp, err
	}
	// log.Println(string(data))
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return resp, err
	}
	if resp.Error != nil && resp.Error.Code != "" {
		err = errors.New(resp.Error.Message)
	}
	return resp, err
}

// ChatCompletionStream represents a streaming response channel
type ChatCompletionStream struct {
	Error    chan error
	Response chan ChatCompletionResponse
}

// Close closes the stream channels
func (stream *ChatCompletionStream) Close() {
	close(stream.Error)
	close(stream.Response)
}

// CreateChatCompletionStream creates a streaming chat completion
func (c *Client) CreateChatCompletionStream(ctx context.Context, request *ChatCompletionRequest) (resp chan ChatCompletionResponse, err error) {
	resp = make(chan ChatCompletionResponse)
	data, err := c.MakeRequest(ctx, "/chat/completions", request)
	if err != nil {
		return
	}
	go func() {
		defer data.Close()
		defer close(resp)
		reader := bufio.NewReader(data)
		for {
			line, err := reader.ReadString('\n')

			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if after, ok := strings.CutPrefix(line, "data: "); ok {
				line = after
			}
			if line == "[DONE]" {
				return
			}
			var chunk ChatCompletionResponse
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				continue
			}
			resp <- chunk
		}
	}()
	return resp, nil
}
