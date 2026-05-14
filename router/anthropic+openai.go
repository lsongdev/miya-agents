package router

import (
	"fmt"
	"strings"

	"github.com/lsongdev/openai-go/anthropic"
	"github.com/lsongdev/openai-go/openai"
)

// ToRequest converts an OpenAI chat completion request to Anthropic messages format.
func NewAnthropicRequestFromChatCompletionRequest(req *openai.ChatCompletionRequest) *anthropic.Request {
	anthropicReq := &anthropic.Request{
		Model:  req.Model,
		Stream: req.Stream,
	}
	if req.MaxTokens > 0 {
		anthropicReq.MaxTokens = req.MaxTokens
	} else {
		anthropicReq.MaxTokens = 4096
	}
	anthropicReq.TopP = req.TopP
	anthropicReq.Temperature = req.Temperature
	anthropicReq.StopSequences = req.Stop

	var systemParts []string
	for _, msg := range req.Messages {
		switch msg.Role {
		case openai.RoleSystem:
			systemParts = append(systemParts, msg.Content)
		case openai.RoleUser:
			anthropicReq.Messages = append(anthropicReq.Messages, anthropic.Message{
				Role:    "user",
				Content: msg.Content,
			})
		case openai.RoleAssistant:
			anthropicReq.Messages = append(anthropicReq.Messages, anthropic.Message{
				Role:    "assistant",
				Content: msg.Content,
			})
		case openai.RoleTool:
			toolCtx := fmt.Sprintf("Tool result (%s): %s", msg.Name, msg.Content)
			anthropicReq.Messages = append(anthropicReq.Messages, anthropic.Message{
				Role:    "user",
				Content: toolCtx,
			})
		}
	}

	if len(systemParts) > 0 {
		anthropicReq.System = strings.Join(systemParts, "\n\n")
	}

	return anthropicReq
}

// ToOpenAIResponse converts an Anthropic message response to OpenAI chat completion format.
func NewChatCompletionResponseFromAnthropicResponse(anthResp *anthropic.Response) *openai.ChatCompletionResponse {
	var content, reasoning string
	for _, block := range anthResp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "thinking":
			reasoning += block.Thinking
		}
	}
	oaiResp := openai.NewChatCompletionResponse(anthResp.ID, anthResp.Model, content, reasoning)
	oaiResp.Choices[0].FinishReason = MapStopReason(anthResp.StopReason)
	oaiResp.Usage = &openai.CompletionUsage{
		PromptTokens:     anthResp.Usage.InputTokens,
		CompletionTokens: anthResp.Usage.OutputTokens,
		TotalTokens:      anthResp.Usage.InputTokens + anthResp.Usage.OutputTokens,
	}
	return oaiResp
}

// ToOpenAIRequest converts an Anthropic messages request to OpenAI chat completion format.
func NewChatCompletionRequestFromAnthropicRequest(req *anthropic.Request) *openai.ChatCompletionRequest {
	oaiReq := &openai.ChatCompletionRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
	}

	oaiReq.TopP = req.TopP
	oaiReq.Stop = req.StopSequences
	oaiReq.MaxTokens = req.MaxTokens
	oaiReq.Temperature = req.Temperature

	if req.System != "" {
		oaiReq.Messages = append(oaiReq.Messages, openai.ChatCompletionMessage{
			Role:    openai.RoleSystem,
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		oaiReq.Messages = append(oaiReq.Messages, openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return oaiReq
}

// MapStopReason maps Anthropic stop reasons to OpenAI finish reasons.
func MapStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}
