package protocols

import (
	"github.com/lsongdev/miya-agents/anthropic"
	"github.com/lsongdev/miya-agents/openai"
)

// anthropicResponseToChat converts an Anthropic response into the Chat
// Completions shape that the proxy's response hooks observe.
func anthropicResponseToChat(resp *anthropic.Response) *openai.ChatCompletionResponse {
	message := openai.ChatCompletionMessage{Role: openai.RoleAssistant}
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			message.Content += block.Text
		case "thinking":
			message.ReasoningContent += block.Thinking
		case "tool_use":
			args := string(block.Input)
			if args == "" {
				args = `{}`
			}
			message.ToolCalls = append(message.ToolCalls, openai.ToolCall{
				Index: len(message.ToolCalls),
				ID:    block.ID,
				Type:  "function",
				Function: openai.FunctionCall{
					Name: block.Name, Arguments: args,
				},
			})
		}
	}
	out := openai.NewChatCompletionResponse(resp.ID, resp.Model, message.Content, message.ReasoningContent)
	out.Choices[0].Message = &message
	out.Choices[0].FinishReason = anthropicFinishReason(resp.StopReason)
	out.Usage = &openai.CompletionUsage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	return out
}

// anthropicFinishReason maps Anthropic stop reasons to OpenAI finish reasons.
func anthropicFinishReason(reason string) string {
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
