package llm

import (
	"context"
	"encoding/json"
	"errors"
	"iter"

	"github.com/lsongdev/miya-agents/anthropic"
)

// anthropicDefaultMaxTokens is used when a request sets no limit; the
// Messages API requires one.
const anthropicDefaultMaxTokens = 4096

// Anthropic returns a Model backed by an Anthropic Messages client.
func Anthropic(client *anthropic.Client) Model {
	return ModelFunc(func(ctx context.Context, req *Request) iter.Seq2[Delta, error] {
		return func(yield func(Delta, error) bool) {
			// Cancelling on return stops the client's stream goroutine when
			// the consumer stops early.
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			stream, err := client.CreateMessageStream(ctx, anthropicRequest(req))
			if err != nil {
				yield(Delta{}, err)
				return
			}
			var usage Usage
			for event := range stream.Events {
				delta, ok, err := anthropicDelta(&event, &usage)
				if err != nil {
					yield(Delta{}, err)
					return
				}
				if ok && !yield(delta, nil) {
					return
				}
			}
			if err := ctx.Err(); err != nil {
				yield(Delta{}, err)
			}
		}
	})
}

func anthropicRequest(req *Request) *anthropic.Request {
	out := &anthropic.Request{
		Model:     req.Model,
		System:    req.System,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}
	if out.MaxTokens == 0 {
		out.MaxTokens = anthropicDefaultMaxTokens
	}
	for _, tool := range req.Tools {
		out.Tools = append(out.Tools, anthropic.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.Parameters,
		})
	}
	previous := Role("")
	for _, message := range req.Messages {
		blocks := anthropicBlocks(message)
		if len(blocks) == 0 {
			continue
		}
		// Results of parallel tool calls must share one user message.
		if message.Role == RoleTool && previous == RoleTool {
			last := &out.Messages[len(out.Messages)-1]
			last.Blocks = append(last.Blocks, blocks...)
			continue
		}
		role := "user"
		if message.Role == RoleAssistant {
			role = "assistant"
		}
		out.Messages = append(out.Messages, anthropic.Message{Role: role, Blocks: blocks})
		previous = message.Role
	}
	return out
}

func anthropicBlocks(message Message) []anthropic.ContentBlock {
	var out []anthropic.ContentBlock
	for _, block := range message.Content {
		switch block.Type {
		case BlockText:
			if block.Text != "" {
				out = append(out, anthropic.ContentBlock{Type: "text", Text: block.Text})
			}
		case BlockThinking:
			// Thinking is only accepted back with the signature it came with.
			// Unsigned thinking comes from another provider and is dropped.
			if block.Signature != "" {
				out = append(out, anthropic.ContentBlock{Type: "thinking", Thinking: block.Text, Signature: block.Signature})
			}
		case BlockImage:
			if media := block.Media; media != nil {
				source := &anthropic.Source{Type: "url", URL: media.URL}
				if media.Data != "" {
					source = &anthropic.Source{Type: "base64", MediaType: media.MimeType, Data: media.Data}
				}
				out = append(out, anthropic.ContentBlock{Type: "image", Source: source})
			}
		case BlockToolCall:
			if call := block.ToolCall; call != nil {
				input := json.RawMessage(call.Arguments)
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				out = append(out, anthropic.ContentBlock{Type: "tool_use", ID: call.ID, Name: call.Name, Input: input})
			}
		case BlockToolResult:
			if result := block.ToolResult; result != nil {
				out = append(out, anthropic.ContentBlock{
					Type:      "tool_result",
					ToolUseID: result.ID,
					Content:   result.Content,
					IsError:   result.IsError,
				})
			}
		}
	}
	return out
}

// anthropicDelta converts one stream event. usage accumulates across events,
// because input and output counts arrive separately.
func anthropicDelta(event *anthropic.Event, usage *Usage) (Delta, bool, error) {
	switch event.Type {
	case "error":
		message := "anthropic stream error"
		if event.Error != nil {
			message = event.Error.Message
		}
		return Delta{}, false, errors.New(message)
	case "message_start":
		if event.Message != nil {
			addAnthropicUsage(usage, &event.Message.Usage)
		}
	case "content_block_start":
		if block := event.ContentBlock; block != nil && block.Type == "tool_use" && event.Index != nil {
			return Delta{ToolCall: &ToolCallDelta{Index: *event.Index, ID: block.ID, Name: block.Name}}, true, nil
		}
	case "content_block_delta":
		var delta anthropic.Delta
		if err := json.Unmarshal(event.Delta, &delta); err != nil {
			return Delta{}, false, err
		}
		switch delta.Type {
		case "text_delta":
			return Delta{Text: delta.Text}, delta.Text != "", nil
		case "thinking_delta":
			return Delta{Thinking: delta.Thinking}, delta.Thinking != "", nil
		case "signature_delta":
			return Delta{Signature: delta.Signature}, delta.Signature != "", nil
		case "input_json_delta":
			if event.Index != nil && delta.PartialJSON != "" {
				return Delta{ToolCall: &ToolCallDelta{Index: *event.Index, Arguments: delta.PartialJSON}}, true, nil
			}
		}
	case "message_delta":
		var delta anthropic.Delta
		if err := json.Unmarshal(event.Delta, &delta); err != nil {
			return Delta{}, false, err
		}
		if event.Usage != nil {
			addAnthropicUsage(usage, event.Usage)
		}
		total := *usage
		return Delta{StopReason: anthropicStopReason(delta.StopReason), Usage: &total}, true, nil
	}
	return Delta{}, false, nil
}

// addAnthropicUsage folds one usage report into the running total. Anthropic
// counts cached tokens outside input_tokens; Usage.InputTokens includes them.
func addAnthropicUsage(total *Usage, report *anthropic.Usage) {
	if input := report.InputTokens + report.CacheReadInputTokens + report.CacheCreationInputTokens; input > 0 {
		total.InputTokens = input
		total.CacheReadTokens = report.CacheReadInputTokens
		total.CacheWriteTokens = report.CacheCreationInputTokens
	}
	if report.OutputTokens > 0 {
		total.OutputTokens = report.OutputTokens
	}
}

func anthropicStopReason(reason string) StopReason {
	switch reason {
	case "end_turn", "stop_sequence":
		return StopEndTurn
	case "tool_use":
		return StopToolUse
	case "max_tokens":
		return StopMaxTokens
	}
	return StopReason(reason)
}
