package router

import (
	"github.com/lsongdev/openai-go/anthropic"
	"github.com/lsongdev/openai-go/openai"
)

// MapStopReasonReverse maps OpenAI finish reasons to Anthropic stop reasons.
func MapStopReasonReverse(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return reason
	}
}

// ToAnthropicResponse converts an OpenAI chat completion response to Anthropic format.
func NewAnthropicResponseFromChatCompletionResponse(oaiResp *openai.ChatCompletionResponse) *anthropic.Response {
	anthResp := &anthropic.Response{
		ID:    oaiResp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: oaiResp.Model,
	}

	if len(oaiResp.Choices) > 0 {
		msg := oaiResp.Choices[0].Message
		if msg.Content != "" {
			anthResp.Content = append(anthResp.Content, anthropic.ContentBlock{
				Type: "text",
				Text: msg.Content,
			})
		}
		if msg.ReasoningContent != "" {
			anthResp.Content = append(anthResp.Content, anthropic.ContentBlock{
				Type:     "thinking",
				Thinking: msg.ReasoningContent,
			})
		}
		anthResp.StopReason = MapStopReasonReverse(oaiResp.Choices[0].FinishReason)
	}

	if oaiResp.Usage != nil && (oaiResp.Usage.PromptTokens > 0 || oaiResp.Usage.CompletionTokens > 0) {
		anthResp.Usage = anthropic.Usage{
			InputTokens:  oaiResp.Usage.PromptTokens,
			OutputTokens: oaiResp.Usage.CompletionTokens,
		}
	}

	return anthResp
}
