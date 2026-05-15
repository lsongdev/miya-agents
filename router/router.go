package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lsongdev/openai-go/anthropic"
	"github.com/lsongdev/openai-go/openai"
)

type ProviderType string

const (
	ProviderTypeOpenAI    ProviderType = "openai"
	ProviderTypeAnthropic ProviderType = "anthropic"
)

type Provider struct {
	Name             string
	Type             ProviderType
	BaseURL          string
	APIKey           string
	Headers          map[string]string
	DefaultMaxTokens int
	Models           []string
}

type RequestContext struct {
	RequestID string
	Stream    bool
}

type Router struct {
	providers  map[string]*Provider
	onRequest  func(RequestContext, *openai.ChatCompletionRequest) (string, error)
	client     *http.Client
	reqCounter uint64
}

func NewRouter() *Router {
	return &Router{
		providers: make(map[string]*Provider),
		client:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (r *Router) SetHTTPClient(client *http.Client) {
	r.client = client
}

func (r *Router) AddProvider(p *Provider) {
	if len(p.Models) == 0 {
		client, _ := openai.NewClient(&openai.Configuration{
			API:    p.BaseURL,
			APIKey: p.APIKey,
		})
		client.SetHTTPClient(r.client)
		if models, err := client.Models(); err == nil {
			for _, m := range models {
				p.Models = append(p.Models, m.ID)
			}
		} else {
			slog.Warn("failed to fetch models", "provider", p.Name, "error", err)
		}
	}
	r.providers[p.Name] = p
}

func (r *Router) OnRequest(fn func(ctx RequestContext, chatReq *openai.ChatCompletionRequest) (string, error)) {
	r.onRequest = fn
}

func (r *Router) nextRequestID() string {
	n := atomic.AddUint64(&r.reqCounter, 1)
	return fmt.Sprintf("req_%d_%d", time.Now().Unix(), n)
}

func (r *Router) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"message":%q,"type":"invalid_request_error"}}`, message)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	slog.Info("ServeHTTP", "method", req.Method, "url", req.URL)
	switch req.URL.Path {
	case "/v1/models":
		if req.Method != http.MethodGet {
			r.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		r.HandleModels(w, req)
	case "/v1/chat/completions":
		if req.Method != http.MethodPost {
			r.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		r.HandleChatCompletions(w, req)
	case "/v1/messages":
		if req.Method != http.MethodPost {
			r.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		r.HandleMessages(w, req)
	default:
		r.writeError(w, http.StatusNotFound, "not found")
	}
}

func (r *Router) HandleModels(w http.ResponseWriter, req *http.Request) {
	var data []openai.Model
	for _, p := range r.providers {
		for _, id := range p.Models {
			data = append(data, openai.Model{
				ID:      id,
				Object:  "model",
				OwnedBy: p.Name,
			})
		}
	}
	resp := map[string]any{
		"object": "list",
		"data":   data,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (r *Router) HandleChatCompletions(w http.ResponseWriter, req *http.Request) {
	var chatReq openai.ChatCompletionRequest
	if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
		r.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	// for _, m := range chatReq.Messages {
	// 	d, _ := json.Marshal(m)
	// 	log.Println("HandleChatCompletions 请求 Messages:", string(d))
	// }

	// Sanitize messages to avoid upstream 400 errors caused by malformed tool
	// calls or tool messages missing required tool_call_id.
	var sanitized []openai.ChatCompletionMessage
	for _, m := range chatReq.Messages {
		if m.Role == openai.RoleTool && m.ToolCallID == "" {
			continue // skip tool messages without tool_call_id
		}
		if m.Role == openai.RoleAssistant && len(m.ToolCalls) > 0 {
			valid := make([]openai.ToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					valid = append(valid, tc)
				}
			}
			m.ToolCalls = valid
		}
		sanitized = append(sanitized, m)
	}
	chatReq.Messages = sanitized

	ctx := RequestContext{
		RequestID: r.nextRequestID(),
		Stream:    chatReq.Stream,
	}

	providerName := ""
	if r.onRequest != nil {
		var err error
		providerName, err = r.onRequest(ctx, &chatReq)
		if err != nil {
			r.writeError(w, http.StatusForbidden, fmt.Sprintf("request rejected: %v", err))
			return
		}
	}
	provider := r.providers[providerName]

	if provider == nil {
		r.writeError(w, http.StatusBadRequest, "no provider available")
		return
	}

	switch provider.Type {
	case "", ProviderTypeOpenAI:
		r.handleOpenAI(w, provider, &chatReq, req.Context())
	case ProviderTypeAnthropic:
		anthropicReq := NewAnthropicRequestFromChatCompletionRequest(&chatReq)
		r.handleAnthropic(w, provider, anthropicReq, req.Context(), ProviderTypeOpenAI)
	default:
		r.writeError(w, http.StatusInternalServerError, fmt.Sprintf("unsupported provider type: %s", provider.Type))
	}
}

func (r *Router) HandleMessages(w http.ResponseWriter, req *http.Request) {
	var anthReq anthropic.Request
	if err := json.NewDecoder(req.Body).Decode(&anthReq); err != nil {
		r.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	ctx := RequestContext{
		RequestID: r.nextRequestID(),
		Stream:    anthReq.Stream,
	}
	chatReq := NewChatCompletionRequestFromAnthropicRequest(&anthReq)
	providerName := ""
	if r.onRequest != nil {
		var err error
		providerName, err = r.onRequest(ctx, chatReq)
		if err != nil {
			r.writeError(w, http.StatusForbidden, fmt.Sprintf("request rejected: %v", err))
			return
		}
	}

	provider := r.providers[providerName]
	if provider == nil {
		r.writeError(w, http.StatusBadRequest, "no provider available")
		return
	}

	switch provider.Type {
	case ProviderTypeAnthropic:
		r.handleAnthropic(w, provider, &anthReq, req.Context(), ProviderTypeAnthropic)
	case "", ProviderTypeOpenAI:
		client, err := r.newOpenAIClient(provider)
		if err != nil {
			r.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create client: %v", err))
			return
		}

		if chatReq.Stream {
			r.handleStreamOpenAI2Anthropic(w, client, chatReq, req.Context())
		} else {
			r.handleNonStreamOpenAI2Anthropic(w, client, chatReq, req.Context())
		}
	default:
		r.writeError(w, http.StatusInternalServerError, fmt.Sprintf("unsupported provider type: %s", provider.Type))
	}
}

func (r *Router) newOpenAIClient(provider *Provider) (*openai.Client, error) {
	client, _ := openai.NewClient(&openai.Configuration{
		API:    provider.BaseURL,
		APIKey: provider.APIKey,
	})
	client.SetHTTPClient(r.client)
	return client, nil
}

func (r *Router) newAnthropicClient(provider *Provider) *anthropic.Client {
	client := anthropic.NewClient(&anthropic.Configuration{
		API:    provider.BaseURL,
		APIKey: provider.APIKey,
	})
	client.SetHTTPClient(r.client)
	return client
}

func (r *Router) handleOpenAI(w http.ResponseWriter, provider *Provider, chatReq *openai.ChatCompletionRequest, ctx context.Context) {
	client, err := r.newOpenAIClient(provider)
	if err != nil {
		r.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create client: %v", err))
		return
	}

	if chatReq.Stream {
		r.streamOpenAI(w, client, chatReq, ctx)
	} else {
		r.handleNonStreamOpenAI(w, client, chatReq, ctx)
	}
}

func (r *Router) handleAnthropic(w http.ResponseWriter, provider *Provider, anthropicReq *anthropic.Request, ctx context.Context, clientFormat ProviderType) {
	client := r.newAnthropicClient(provider)

	if anthropicReq.Stream {
		r.streamAnthropic(w, client, anthropicReq, ctx, clientFormat)
	} else {
		r.handleNonStreamAnthropic(w, client, anthropicReq, ctx, clientFormat)
	}
}

func (r *Router) handleStreamOpenAI2Anthropic(w http.ResponseWriter, client *openai.Client, chatReq *openai.ChatCompletionRequest, ctx context.Context) {
	chunks, err := client.CreateChatCompletionStream(ctx, chatReq)
	if err != nil {
		slog.Warn("stream upstream error", "error", err)
		r.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}

	anthStream := anthropic.NewStream(w)

	var (
		messageID        string
		model            string
		outputTokens     int
		stopReason       string
		hasContent       bool
		sentMessageStart bool
		notedFinish      bool
	)

	sendMessageStart := func() {
		if sentMessageStart {
			return
		}
		anthStream.Send(anthropic.Event{
			Type: "message_start",
			Message: &anthropic.MessageStart{
				ID:    messageID,
				Type:  "message",
				Role:  "assistant",
				Model: model,
			},
		})
		sentMessageStart = true
	}

	sendContentBlockStart := func() {
		anthStream.Send(anthropic.Event{
			Type:         "content_block_start",
			Index:        0,
			ContentBlock: &anthropic.ContentBlock{Type: "text"},
		})
		hasContent = true
	}

	sendFinish := func() {
		if notedFinish {
			return
		}
		notedFinish = true
		if hasContent {
			anthStream.Send(anthropic.Event{
				Type: "content_block_stop", Index: 0,
			})
		}
		deltaData, _ := json.Marshal(map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		})
		var usageOut *anthropic.Usage
		if outputTokens > 0 {
			usageOut = &anthropic.Usage{OutputTokens: outputTokens}
		}
		anthStream.Send(anthropic.Event{
			Type: "message_delta", Delta: deltaData, Usage: usageOut,
		})
		anthStream.Send(anthropic.Event{Type: "message_stop"})
	}

	for chunk := range chunks {
		if chunk.ID != "" && messageID == "" {
			messageID = chunk.ID
		}
		if chunk.Model != "" && model == "" {
			model = chunk.Model
		}
		if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
			outputTokens = chunk.Usage.CompletionTokens
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

		if delta.Content != "" {
			sendMessageStart()
			if !hasContent {
				sendContentBlockStart()
			}
			deltaBytes, _ := json.Marshal(anthropic.Delta{
				Type: "text_delta", Text: delta.Content,
			})
			anthStream.Send(anthropic.Event{
				Type: "content_block_delta", Index: 0, Delta: deltaBytes,
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
}

func (r *Router) handleNonStreamOpenAI2Anthropic(w http.ResponseWriter, client *openai.Client, chatReq *openai.ChatCompletionRequest, ctx context.Context) {
	resp, err := client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		r.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	anthResp := NewAnthropicResponseFromChatCompletionResponse(&resp)

	out, _ := json.Marshal(anthResp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

func (r *Router) streamOpenAI(w http.ResponseWriter, client *openai.Client, chatReq *openai.ChatCompletionRequest, ctx context.Context) {
	stream := openai.NewStream(w)
	chunks, err := client.CreateChatCompletionStream(ctx, chatReq)
	if err != nil {
		slog.Warn("stream upstream error", "error", err)
		stream.SendError(err)
		return
	}
	for chunk := range chunks {
		// d, _ := json.Marshal(chunk)
		// log.Println("====>", string(d))
		stream.Send(chunk)
	}
	stream.Done()
}

func (r *Router) handleNonStreamOpenAI(w http.ResponseWriter, client *openai.Client, chatReq *openai.ChatCompletionRequest, ctx context.Context) {
	resp, err := client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		r.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}

	out, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

func (r *Router) streamAnthropic(w http.ResponseWriter, client *anthropic.Client, anthropicReq *anthropic.Request, ctx context.Context, clientFormat ProviderType) {
	ms, err := client.CreateMessageStream(ctx, anthropicReq)
	if err != nil {
		r.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}

	switch clientFormat {
	case ProviderTypeOpenAI:
		oaiStream := openai.NewStream(w)
		var (
			messageID    string
			model        string
			inputTokens  int
			outputTokens int
			stopReason   string
			sentFirst    bool
		)
		for event := range ms.Events {
			switch event.Type {
			case "message_start":
				if event.Message != nil {
					messageID = event.Message.ID
					model = event.Message.Model
					inputTokens = event.Message.Usage.InputTokens
				}

			case "content_block_delta":
				var delta anthropic.Delta
				if err := json.Unmarshal(event.Delta, &delta); err != nil {
					continue
				}
				oaiDelta := &openai.ChatCompletionMessage{}
				switch delta.Type {
				case "text_delta":
					oaiDelta.Content = delta.Text
				case "thinking_delta":
					oaiDelta.ReasoningContent = delta.Thinking
				}
				if oaiDelta.Content == "" && oaiDelta.ReasoningContent == "" {
					continue
				}
				if !sentFirst {
					oaiDelta.Role = openai.RoleAssistant
					sentFirst = true
				}
				oaiStream.SendChatCompletionChunk(messageID, model, oaiDelta)

			case "message_delta":
				var delta anthropic.Delta
				if err := json.Unmarshal(event.Delta, &delta); err == nil {
					if delta.StopReason != "" {
						stopReason = MapStopReason(delta.StopReason)
					}
				}
				if event.Usage != nil {
					outputTokens = event.Usage.OutputTokens
				}

			case "message_stop":
				usage := &openai.CompletionUsage{
					PromptTokens:     inputTokens,
					CompletionTokens: outputTokens,
					TotalTokens:      inputTokens + outputTokens,
				}
				oaiStream.Stop(messageID, model, stopReason, usage)
			case "error":
				oaiStream.Stop(messageID, model, "stop", nil)
			}
		}
		oaiStream.Done()

	default:
		anthStream := anthropic.NewStream(w)
		for event := range ms.Events {
			anthStream.Send(event)
		}
		anthStream.Done()
	}
}

func (r *Router) handleNonStreamAnthropic(w http.ResponseWriter, client *anthropic.Client, anthropicReq *anthropic.Request, ctx context.Context, clientFormat ProviderType) {
	resp, err := client.CreateMessage(ctx, anthropicReq)
	if err != nil {
		r.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}

	switch clientFormat {
	case ProviderTypeAnthropic:
		data, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	default:
		oaiResp := NewChatCompletionResponseFromAnthropicResponse(resp)
		out, _ := json.Marshal(oaiResp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(out)
	}
}
