package openai

import "time"

// MessageBuilder accumulates streaming deltas into a complete ChatCompletionMessage.
type MessageBuilder struct {
	message   ChatCompletionMessage
	toolCalls []*ToolCall
	idxMap    map[int]*ToolCall    // stream index -> tool call
	idMap     map[string]*ToolCall // tool call id -> tool call
}

// NewMessageBuilder creates a new MessageBuilder.
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		idxMap: make(map[int]*ToolCall),
		idMap:  make(map[string]*ToolCall),
	}
}

// Update accumulates data from a stream delta.
func (b *MessageBuilder) Update(delta ChatCompletionMessage) {
	if b.message.Role == "" {
		b.message.Role = RoleAssistant
	}

	b.message.ReasoningContent += delta.ReasoningContent
	b.message.Content += delta.Content

	if delta.ToolCalls == nil {
		return
	}
	for _, tc := range delta.ToolCalls {
		if tc.ID != "" {
			if existing, ok := b.idMap[tc.ID]; ok {
				if tc.Type != "" {
					existing.Type = tc.Type
				}
				existing.Function.Name += tc.Function.Name
				existing.Function.Arguments += tc.Function.Arguments
				b.idxMap[tc.Index] = existing
			} else {
				tcCopy := tc
				b.toolCalls = append(b.toolCalls, &tcCopy)
				b.idxMap[tc.Index] = b.toolCalls[len(b.toolCalls)-1]
				b.idMap[tc.ID] = b.toolCalls[len(b.toolCalls)-1]
			}
		} else if existing, ok := b.idxMap[tc.Index]; ok {
			if tc.Type != "" {
				existing.Type = tc.Type
			}
			existing.Function.Name += tc.Function.Name
			existing.Function.Arguments += tc.Function.Arguments
		} else if len(b.toolCalls) > 0 {
			last := b.toolCalls[len(b.toolCalls)-1]
			if tc.Type != "" {
				last.Type = tc.Type
			}
			last.Function.Name += tc.Function.Name
			last.Function.Arguments += tc.Function.Arguments
			b.idxMap[tc.Index] = last
		}
	}
}

// Build returns the final accumulated message.
func (b *MessageBuilder) Build() ChatCompletionMessage {
	if len(b.toolCalls) > 0 {
		b.message.ToolCalls = make([]ToolCall, len(b.toolCalls))
		for i, tc := range b.toolCalls {
			b.message.ToolCalls[i] = *tc
		}
	}
	return b.message
}

// ResponseAssembler accumulates streaming chunks into a complete ChatCompletionResponse.
type ResponseAssembler struct {
	messageBuilders   map[int]*MessageBuilder
	id                string
	model             string
	systemFingerprint string
	finishReasons     map[int]string
	usage             *CompletionUsage
	created           int64
}

// NewResponseAssembler creates a new ResponseAssembler.
func NewResponseAssembler() *ResponseAssembler {
	return &ResponseAssembler{
		messageBuilders: make(map[int]*MessageBuilder),
		finishReasons:   make(map[int]string),
	}
}

// Update processes a streaming chunk.
func (a *ResponseAssembler) Update(chunk ChatCompletionResponse) {
	if a.id == "" && chunk.ID != "" {
		a.id = chunk.ID
	}
	if a.model == "" && chunk.Model != "" {
		a.model = chunk.Model
	}
	if a.created == 0 && chunk.Created != 0 {
		a.created = chunk.Created
	}
	if chunk.SystemFingerprint != "" {
		a.systemFingerprint = chunk.SystemFingerprint
	}
	if chunk.Usage != nil {
		a.usage = chunk.Usage
	}

	for _, choice := range chunk.Choices {
		if choice.Delta != nil {
			builder, ok := a.messageBuilders[choice.Index]
			if !ok {
				builder = NewMessageBuilder()
				a.messageBuilders[choice.Index] = builder
			}
			builder.Update(*choice.Delta)
		}
		if choice.FinishReason != "" {
			a.finishReasons[choice.Index] = choice.FinishReason
		}
	}
}

// Build returns the final assembled ChatCompletionResponse.
func (a *ResponseAssembler) Build() *ChatCompletionResponse {
	if a.created == 0 {
		a.created = time.Now().Unix()
	}

	var choices []ChatCompletionChoice
	for idx, builder := range a.messageBuilders {
		msg := builder.Build()
		choices = append(choices, ChatCompletionChoice{
			Index:        idx,
			Message:      &msg,
			FinishReason: a.finishReasons[idx],
		})
	}

	return &ChatCompletionResponse{
		ID:                a.id,
		Object:            "chat.completion",
		Created:           a.created,
		Model:             a.model,
		Choices:           choices,
		Usage:             a.usage,
		SystemFingerprint: a.systemFingerprint,
	}
}

// AssembleFromChunks consumes a channel of streaming chunks and returns the final ChatCompletionResponse.
func AssembleFromChunks(ch <-chan ChatCompletionResponse) *ChatCompletionResponse {
	assembler := NewResponseAssembler()
	for chunk := range ch {
		assembler.Update(chunk)
	}
	return assembler.Build()
}
