package openai

import (
	"context"
	"time"
)

const (
	RoleSystem    = "system"
	RoleAssistant = "assistant"
	RoleUser      = "user"
	RoleTool      = "tool"
)

const (
	GPT3_5_Trubo      = "gpt-3.5-turbo"
	GPT3_5_Trubo_0301 = "gpt-3.5-turbo-0301"
	GPT4              = "gpt-4"
	GPT4o             = "gpt-4o"
	DeepSeekChat      = "deepseek-chat"
)

// Tool represents an executable tool.
type Tool interface {
	Def() ToolDef
	Run(ctx context.Context, args string) string
}

// ToolCall represents a tool invocation by the model.
type ToolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall represents a function call within a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDef defines a tool for the LLM (OpenAI function calling format).
type ToolDef struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

// FunctionDef defines a function that the model can call.
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
}

type ChatCompletionRequest struct {
	Model           string                  `json:"model,omitempty"`
	Messages        []ChatCompletionMessage `json:"messages,omitempty"`
	Tools           []ToolDef               `json:"tools,omitempty"`
	Temperature     *float64                `json:"temperature,omitempty"`
	TopP            *float64                `json:"top_p,omitempty"`
	NumberOfChoices *int                    `json:"n,omitempty"`
	Stream          bool                    `json:"stream,omitempty"`
	Stop            []string                `json:"stop,omitempty"`
	MaxTokens       int                     `json:"max_tokens,omitempty"`
	User            string                  `json:"user,omitempty"`
}

type CompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`

	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens,omitempty"` // deepseek-reasoner
	} `json:"completion_tokens_details,omitempty"`

	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`  // deepseek-reasoner
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"` // deepseek-reasoner
}

type ChatCompletionResponse struct {
	Error             *Error                 `json:"error,omitempty"`
	Usage             *CompletionUsage       `json:"usage,omitempty"`
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Created           int64                  `json:"created"`
	Model             string                 `json:"model"`
	Choices           []ChatCompletionChoice `json:"choices"`
	SystemFingerprint string                 `json:"system_fingerprint,omitempty"` // deepseek-reasoner
}

func NewChatCompletionResponse(id, model, content, reasoning string) (oaiResp *ChatCompletionResponse) {
	oaiResp = &ChatCompletionResponse{
		ID:      id,
		Model:   model,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Choices: []ChatCompletionChoice{
			{
				Index: 0,
				Message: &ChatCompletionMessage{
					Role:             RoleAssistant,
					Content:          content,
					ReasoningContent: reasoning,
				},
			},
		},
	}
	return
}

type Error struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

type Configuration struct {
	API    string
	APIKey string `json:"api_key"`
}
