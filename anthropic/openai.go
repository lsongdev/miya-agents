package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lsongdev/openai-go/openai"
)

// ToRequest converts an OpenAI chat completion request to Anthropic messages format.
func NewAnthropicRequestFromChatCompletionRequest(req *openai.ChatCompletionRequest) *Request {
	anthropicReq := &Request{
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
			anthropicReq.Messages = append(anthropicReq.Messages, Message{
				Role:    "user",
				Content: msg.Content,
			})
		case openai.RoleAssistant:
			anthropicReq.Messages = append(anthropicReq.Messages, Message{
				Role:    "assistant",
				Content: msg.Content,
			})
		case openai.RoleTool:
			toolCtx := fmt.Sprintf("Tool result (%s): %s", msg.Name, msg.Content)
			anthropicReq.Messages = append(anthropicReq.Messages, Message{
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

// ToAnthropicResponse converts an OpenAI chat completion response to Anthropic format.
func NewAnthropicResponseFromChatCompletionResponse(oaiResp *openai.ChatCompletionResponse) *Response {
	anthResp := &Response{
		ID:    oaiResp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: oaiResp.Model,
	}

	if len(oaiResp.Choices) > 0 {
		msg := oaiResp.Choices[0].Message
		if msg.Content != "" {
			anthResp.Content = append(anthResp.Content, ContentBlock{
				Type: "text",
				Text: msg.Content,
			})
		}
		if msg.ReasoningContent != "" {
			anthResp.Content = append(anthResp.Content, ContentBlock{
				Type:     "thinking",
				Thinking: msg.ReasoningContent,
			})
		}
		anthResp.StopReason = MapStopReasonReverse(oaiResp.Choices[0].FinishReason)
	}

	if oaiResp.Usage != nil && (oaiResp.Usage.PromptTokens > 0 || oaiResp.Usage.CompletionTokens > 0) {
		anthResp.Usage = Usage{
			InputTokens:  oaiResp.Usage.PromptTokens,
			OutputTokens: oaiResp.Usage.CompletionTokens,
		}
	}

	return anthResp
}

// ToOpenAIResponse converts an Anthropic message response to OpenAI chat completion format.
func NewChatCompletionResponseFromAnthropicResponse(anthResp *Response) *openai.ChatCompletionResponse {
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
func NewChatCompletionRequestFromAnthropicRequest(req *Request) *openai.ChatCompletionRequest {
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

func (c *Client) CreateChatCompletion(ctx context.Context, req *openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error) {
	anthReq := NewAnthropicRequestFromChatCompletionRequest(req)
	resp, err := c.CreateMessage(ctx, anthReq)
	if err != nil {
		return nil, err
	}
	return NewChatCompletionResponseFromAnthropicResponse(resp), nil
}

func (c *Client) CreateChatCompletionStream(ctx context.Context, req *openai.ChatCompletionRequest) (<-chan openai.ChatCompletionResponse, error) {
	anthReq := NewAnthropicRequestFromChatCompletionRequest(req)
	anthReq.Stream = true
	stream, err := c.CreateMessageStream(ctx, anthReq)
	if err != nil {
		return nil, err
	}
	return AnthropicStreamToChatCompletionStream(stream, nil), nil
}

// AnthropicStreamToChatCompletionStream converts an Anthropic message stream to OpenAI chat completion chunks.
// If onEvent is provided, each event is passed to it before conversion.
func AnthropicStreamToChatCompletionStream(stream *MessageStream, onEvent func(Event)) <-chan openai.ChatCompletionResponse {
	ch := make(chan openai.ChatCompletionResponse)
	go func() {
		defer close(ch)
		var messageID, model string
		var sentFirst bool
		var inputTokens int
		for event := range stream.Events {
			if onEvent != nil {
				onEvent(event)
			}
			switch event.Type {
			case "message_start":
				if event.Message != nil {
					messageID = event.Message.ID
					model = event.Message.Model
					inputTokens = event.Message.Usage.InputTokens
				}
			case "content_block_delta":
				var delta Delta
				if err := json.Unmarshal(event.Delta, &delta); err != nil {
					continue
				}
				msg := &openai.ChatCompletionMessage{}
				switch delta.Type {
				case "text_delta":
					msg.Content = delta.Text
				case "thinking_delta":
					msg.ReasoningContent = delta.Thinking
				}
				if msg.Content == "" && msg.ReasoningContent == "" {
					continue
				}
				if !sentFirst {
					msg.Role = openai.RoleAssistant
					sentFirst = true
				}
				ch <- openai.ChatCompletionResponse{
					ID:      messageID,
					Model:   model,
					Object:  "chat.completion.chunk",
					Choices: []openai.ChatCompletionChoice{{Index: 0, Delta: msg}},
				}
			case "message_delta":
				var delta Delta
				if err := json.Unmarshal(event.Delta, &delta); err == nil && delta.StopReason != "" {
					outputTokens := 0
					if event.Usage != nil {
						outputTokens = event.Usage.OutputTokens
					}
					ch <- openai.ChatCompletionResponse{
						ID:     messageID,
						Model:  model,
						Object: "chat.completion.chunk",
						Choices: []openai.ChatCompletionChoice{{
							Index:        0,
							FinishReason: MapStopReason(delta.StopReason),
						}},
						Usage: &openai.CompletionUsage{
							PromptTokens:     inputTokens,
							CompletionTokens: outputTokens,
							TotalTokens:      inputTokens + outputTokens,
						},
					}
				}
			case "message_stop":
				return
			}
		}
	}()
	return ch
}

// OpenAIStreamToAnthropicStream converts an OpenAI chat completion stream to Anthropic SSE events.
// The onChunk callback receives each chunk before it is processed (useful for assembling the final response).
// Returns the final assembled ChatCompletionResponse after the stream completes.
func OpenAIStreamToAnthropicStream(chunks <-chan openai.ChatCompletionResponse, w http.ResponseWriter, onChunk func(openai.ChatCompletionResponse)) *openai.ChatCompletionResponse {
	anthStream := NewResponseWriter(w)

	var (
		messageID        string
		model            string
		stopReason       string
		hasContent       bool
		sentMessageStart bool
		notedFinish      bool
	)

	assembler := openai.NewResponseAssembler()

	sendMessageStart := func() {
		if sentMessageStart {
			return
		}
		anthStream.SendMessageStart(messageID, "message", "assistant", model)
		sentMessageStart = true
	}

	sendContentBlockStart := func() {
		anthStream.SendContentBlockStart(0, "text")
		hasContent = true
	}

	sendFinish := func() {
		if notedFinish {
			return
		}
		notedFinish = true
		if hasContent {
			anthStream.SendContentBlockStop(0)
		}
		resp := assembler.Build()
		var usageOut *Usage
		if resp.Usage != nil {
			usageOut = &Usage{OutputTokens: resp.Usage.CompletionTokens}
		}
		anthStream.SendMessageDelta(stopReason, nil, usageOut)
		anthStream.SendMessageStop()
	}

	for chunk := range chunks {
		assembler.Update(chunk)
		if onChunk != nil {
			onChunk(chunk)
		}

		if chunk.ID != "" && messageID == "" {
			messageID = chunk.ID
		}
		if chunk.Model != "" && model == "" {
			model = chunk.Model
		}

		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 && !notedFinish {
				if stopReason == "" {
					stopReason = "end_turn"
				}
				sendMessageStart()
				sendFinish()
			}
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		if delta != nil && delta.Content != "" {
			sendMessageStart()
			if !hasContent {
				sendContentBlockStart()
			}
			anthStream.SendContentBlockDelta(0, Delta{
				Type: "text_delta", Text: delta.Content,
			})
		}

		if choice.FinishReason != "" {
			stopReason = MapStopReasonReverse(choice.FinishReason)
			sendMessageStart()
			sendFinish()
		}
	}

	if !notedFinish {
		if stopReason == "" {
			stopReason = "end_turn"
		}
		if !sentMessageStart {
			sendMessageStart()
		}
		sendFinish()
	}

	return assembler.Build()
}

func (c *Client) CreateEmbeddings(ctx context.Context, req *openai.EmbeddingRequest) (*openai.EmbeddingResponse, error) {
	return nil, fmt.Errorf("embeddings not supported by anthropic client")
}

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
