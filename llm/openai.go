package llm

import (
	"context"
	"errors"
	"iter"

	"github.com/lsongdev/miya-agents/openai"
)

// OpenAI returns a Model backed by an OpenAI Chat Completions client.
func OpenAI(client *openai.Client) Model {
	return ModelFunc(func(ctx context.Context, req *Request) iter.Seq2[Delta, error] {
		return func(yield func(Delta, error) bool) {
			// Cancelling on return stops the client's stream goroutine when
			// the consumer stops early.
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			chunks, err := client.CreateChatCompletionStream(ctx, openAIRequest(req))
			if err != nil {
				yield(Delta{}, err)
				return
			}
			var slots openAIToolSlots
			for chunk := range chunks {
				if chunk.Error != nil {
					yield(Delta{}, errors.New(chunk.Error.Message))
					return
				}
				for _, delta := range openAIDeltas(&chunk, &slots) {
					if !yield(delta, nil) {
						return
					}
				}
			}
			if err := ctx.Err(); err != nil {
				yield(Delta{}, err)
			}
		}
	})
}

func openAIRequest(req *Request) *openai.ChatCompletionRequest {
	out := &openai.ChatCompletionRequest{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Stream:        true,
		StreamOptions: &openai.StreamOptions{IncludeUsage: true},
	}
	for _, tool := range req.Tools {
		out.Tools = append(out.Tools, openai.ToolDef{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	if req.System != "" {
		out.Messages = append(out.Messages, openai.SystemMessage(req.System))
	}
	for _, message := range req.Messages {
		out.Messages = append(out.Messages, openAIMessages(message)...)
	}
	return out
}

func openAIMessages(message Message) []openai.ChatCompletionMessage {
	switch message.Role {
	case RoleTool:
		var out []openai.ChatCompletionMessage
		for _, block := range message.Content {
			if result := block.ToolResult; block.Type == BlockToolResult && result != nil {
				out = append(out, openai.ToolResultMessage(result.ID, result.Name, result.Content))
			}
		}
		return out
	case RoleAssistant:
		out := openai.ChatCompletionMessage{
			Role:             openai.RoleAssistant,
			Content:          message.Text(),
			ReasoningContent: message.Thinking(),
		}
		for _, call := range message.ToolCalls() {
			out.ToolCalls = append(out.ToolCalls, openai.ToolCall{
				Index:    len(out.ToolCalls),
				ID:       call.ID,
				Type:     "function",
				Function: openai.FunctionCall{Name: call.Name, Arguments: call.Arguments},
			})
		}
		return []openai.ChatCompletionMessage{out}
	default:
		out := openai.ChatCompletionMessage{Role: openai.RoleUser, Content: message.Text()}
		var parts []openai.ContentPart
		hasImage := false
		for _, block := range message.Content {
			switch {
			case block.Type == BlockText:
				parts = append(parts, openai.ContentPart{Type: "text", Text: block.Text})
			case block.Type == BlockImage && block.Media != nil:
				url := block.Media.URL
				if block.Media.Data != "" {
					url = "data:" + block.Media.MimeType + ";base64," + block.Media.Data
				}
				parts = append(parts, openai.ContentPart{Type: "image_url", ImageURL: &openai.ImageURL{URL: url}})
				hasImage = true
			}
		}
		// Plain text stays a string; many compatible servers accept only that.
		if hasImage {
			out.Parts = parts
		}
		return []openai.ChatCompletionMessage{out}
	}
}

func openAIDeltas(chunk *openai.ChatCompletionResponse, slots *openAIToolSlots) []Delta {
	var out []Delta
	for _, choice := range chunk.Choices {
		if message := choice.Delta; message != nil {
			if message.ReasoningContent != "" || message.Content != "" {
				out = append(out, Delta{Thinking: message.ReasoningContent, Text: message.Content})
			}
			for _, call := range message.ToolCalls {
				index := slots.resolve(call)
				out = append(out, Delta{ToolCall: &ToolCallDelta{
					Index:     index,
					ID:        slots.newID,
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				}})
			}
		}
		if choice.FinishReason != "" {
			out = append(out, Delta{StopReason: openAIStopReason(choice.FinishReason)})
		}
	}
	if usage := chunk.Usage; usage != nil {
		cached := usage.PromptTokensDetails.CachedTokens
		if cached == 0 {
			cached = usage.PromptCacheHitTokens
		}
		out = append(out, Delta{Usage: &Usage{
			InputTokens:     usage.PromptTokens,
			OutputTokens:    usage.CompletionTokens,
			CacheReadTokens: cached,
		}})
	}
	return out
}

func openAIStopReason(reason string) StopReason {
	switch reason {
	case "stop":
		return StopEndTurn
	case "tool_calls", "function_call":
		return StopToolUse
	case "length":
		return StopMaxTokens
	}
	return StopReason(reason)
}

// openAIToolSlots assigns each streamed tool call fragment a stable index.
//
// The protocol identifies fragments by index, but compatible servers vary:
// some repeat the id on every fragment, some omit the index, some send
// neither after the first fragment. A fragment is matched by id, then by
// index, then to the most recent call.
type openAIToolSlots struct {
	count   int
	byID    map[string]int
	byIndex map[int]int
	// newID is the id to record for the fragment last resolved: its id when
	// that id opened a new call, empty otherwise.
	newID string
}

func (s *openAIToolSlots) resolve(call openai.ToolCall) int {
	if s.byID == nil {
		s.byID = make(map[string]int)
		s.byIndex = make(map[int]int)
	}
	s.newID = ""
	if call.ID != "" {
		slot, seen := s.byID[call.ID]
		if !seen {
			slot = s.count
			s.count++
			s.byID[call.ID] = slot
			s.newID = call.ID
		}
		s.byIndex[call.Index] = slot
		return slot
	}
	if slot, ok := s.byIndex[call.Index]; ok {
		return slot
	}
	slot := s.count - 1
	if slot < 0 {
		slot = 0
		s.count = 1
	}
	s.byIndex[call.Index] = slot
	return slot
}
