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
	parent       context.Context
	RequestID    string
	Response     http.ResponseWriter
	Request      *http.Request
	Upstream     *Provider
	Input        *openai.ChatCompletionRequest
	OutputFormat ProviderType
}

func (c *RequestContext) Deadline() (deadline time.Time, ok bool) {
	return c.parent.Deadline()
}

func (c *RequestContext) Done() <-chan struct{} {
	return c.parent.Done()
}

func (c *RequestContext) Err() error {
	return c.parent.Err()
}

func (c *RequestContext) Value(key any) any {
	return c.parent.Value(key)
}

type ResponseContext struct {
	RequestID string
	Request   *http.Request
	Response  http.ResponseWriter
	Input     any
	Output    *openai.ChatCompletionResponse
	Error     error
	Duration  time.Duration
}

type Router struct {
	providers  map[string]*Provider
	onRequest  func(*RequestContext) error
	onResponse func(*ResponseContext)
	client     *http.Client
	reqCounter uint64
}

func NewRouter() *Router {
	return &Router{
		providers: make(map[string]*Provider),
		client: &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
		},
	}
}

func (router *Router) SetHTTPClient(client *http.Client) {
	router.client = client
}

func (router *Router) AddProvider(p *Provider) {
	if len(p.Models) == 0 {
		client, _ := openai.NewClient(&openai.Configuration{
			API:    p.BaseURL,
			APIKey: p.APIKey,
		})
		client.SetHTTPClient(router.client)
		if models, err := client.Models(); err == nil {
			for _, m := range models {
				p.Models = append(p.Models, m.ID)
			}
		} else {
			slog.Warn("failed to fetch models", "provider", p.Name, "error", err)
		}
	}
	router.providers[p.Name] = p
}

func (router *Router) FindProviderForModel(model string) *Provider {
	for _, p := range router.providers {
		for _, m := range p.Models {
			if m == model {
				return p
			}
		}
	}
	return nil
}

func (router *Router) OnRequest(fn func(ctx *RequestContext) error) {
	router.onRequest = fn
}

func (router *Router) OnResponse(fn func(ctx *ResponseContext)) {
	router.onResponse = fn
}

func (router *Router) nextRequestID() string {
	n := atomic.AddUint64(&router.reqCounter, 1)
	return fmt.Sprintf("req_%d_%d", time.Now().Unix(), n)
}

func (router *Router) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"message":%q,"type":"invalid_request_error"}}`, message)
}

func (router *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	slog.Info("ServeHTTP", "method", req.Method, "url", req.URL)
	switch req.URL.Path {
	case "/v1/models":
		router.HandleModels(w, req)
	case "/v1/messages":
		router.HandleMessages(w, req)
	case "/v1/chat/completions":
		router.HandleChatCompletions(w, req)
	case "/v1/embeddings":
		router.HandleEmbeddings(w, req)
	default:
		router.writeError(w, http.StatusNotFound, "not found")
	}
}

func (router *Router) HandleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		router.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var models []openai.Model
	for _, p := range router.providers {
		for _, id := range p.Models {
			models = append(models, openai.Model{
				ID:      id,
				Object:  "model",
				OwnedBy: p.Name,
			})
		}
	}
	resp := map[string]any{
		"object": "list",
		"data":   models,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (router *Router) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		router.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var chatReq openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
		router.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	ctx := &RequestContext{
		parent:       r.Context(),
		RequestID:    router.nextRequestID(),
		Request:      r,
		Response:     w,
		Input:        &chatReq,
		OutputFormat: ProviderTypeOpenAI,
	}

	start := time.Now()

	if router.onRequest != nil {
		err := router.onRequest(ctx)
		if err != nil {
			router.writeError(w, http.StatusForbidden, fmt.Sprintf("request rejected: %v", err))
			return
		}
	}
	if ctx.Upstream == nil {
		ctx.Upstream = router.FindProviderForModel(chatReq.Model)
	}
	if ctx.Upstream == nil {
		router.writeError(w, http.StatusBadRequest, fmt.Sprintf("no provider available for model %s", chatReq.Model))
		return
	}

	var respErr error
	var chatResp *openai.ChatCompletionResponse

	switch ctx.Upstream.Type {
	case "", ProviderTypeOpenAI:
		chatResp, respErr = router.handleOpenAI(ctx, w)
	case ProviderTypeAnthropic:
		chatResp, respErr = router.handleAnthropic(ctx, w)
	default:
		respErr = fmt.Errorf("unsupported provider type: %s", ctx.Upstream.Type)
		router.writeError(w, http.StatusInternalServerError, respErr.Error())
	}

	if router.onResponse != nil {
		router.onResponse(&ResponseContext{
			RequestID: ctx.RequestID,
			Request:   r,
			Response:  w,
			Input:     ctx.Input,
			Output:    chatResp,
			Error:     respErr,
			Duration:  time.Since(start),
		})
	}
}

func (router *Router) HandleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		router.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var embReq openai.EmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&embReq); err != nil {
		router.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	ctx := &RequestContext{
		parent:       r.Context(),
		RequestID:    router.nextRequestID(),
		Request:      r,
		Response:     w,
		Input:        &openai.ChatCompletionRequest{Model: embReq.Model},
		OutputFormat: ProviderTypeOpenAI,
	}
	start := time.Now()
	if router.onRequest != nil {
		err := router.onRequest(ctx)
		if err != nil {
			router.writeError(w, http.StatusForbidden, fmt.Sprintf("request rejected: %v", err))
			return
		}
	}
	if ctx.Upstream == nil {
		ctx.Upstream = router.FindProviderForModel(embReq.Model)
	}
	if ctx.Upstream == nil {
		router.writeError(w, http.StatusBadRequest, fmt.Sprintf("no provider available for model %s", embReq.Model))
		return
	}
	client, err := router.newOpenAIClient(ctx.Upstream)
	if err != nil {
		router.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create client: %v", err))
		return
	}
	resp, err := client.CreateEmbeddings(ctx, &embReq)
	if err != nil {
		router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	out, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)

	if router.onResponse != nil {
		router.onResponse(&ResponseContext{
			RequestID: ctx.RequestID,
			Request:   r,
			Response:  w,
			Input:     ctx.Input,
			Error:     nil,
			Duration:  time.Since(start),
		})
	}
}

func (router *Router) HandleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		router.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var anthReq anthropic.Request
	if err := json.NewDecoder(r.Body).Decode(&anthReq); err != nil {
		router.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	chatReq := anthropic.NewChatCompletionRequestFromAnthropicRequest(&anthReq)
	ctx := &RequestContext{
		parent:       r.Context(),
		RequestID:    router.nextRequestID(),
		Request:      r,
		Response:     w,
		Input:        chatReq,
		OutputFormat: ProviderTypeAnthropic,
	}

	start := time.Now()

	if router.onRequest != nil {
		err := router.onRequest(ctx)
		if err != nil {
			router.writeError(w, http.StatusForbidden, fmt.Sprintf("request rejected: %v", err))
			return
		}
	}
	if ctx.Upstream == nil {
		ctx.Upstream = router.FindProviderForModel(chatReq.Model)
	}
	if ctx.Upstream == nil {
		router.writeError(w, http.StatusBadRequest, fmt.Sprintf("no provider available for model %s", chatReq.Model))
		return
	}

	var respErr error
	var chatResp *openai.ChatCompletionResponse

	switch ctx.Upstream.Type {
	case ProviderTypeAnthropic:
		chatResp, respErr = router.handleAnthropic(ctx, w)
	case "", ProviderTypeOpenAI:
		if ctx.Input.Stream {
			chatResp, respErr = router.handleOpenaiToAnthropicStream(ctx, w)
		} else {
			chatResp, respErr = router.handleOpenaiToAnthropicNonStream(ctx, w)
		}
	default:
		respErr = fmt.Errorf("unsupported provider type: %s", ctx.Upstream.Type)
		router.writeError(w, http.StatusInternalServerError, respErr.Error())
	}

	if router.onResponse != nil {
		router.onResponse(&ResponseContext{
			RequestID: ctx.RequestID,
			Request:   r,
			Response:  w,
			Input:     ctx.Input,
			Output:    chatResp,
			Error:     respErr,
			Duration:  time.Since(start),
		})
	}
}

func (router *Router) newOpenAIClient(provider *Provider) (openai.ChatClient, error) {
	client, _ := openai.NewClient(&openai.Configuration{
		API:    provider.BaseURL,
		APIKey: provider.APIKey,
	})
	client.SetHTTPClient(router.client)
	return client, nil
}

func (router *Router) newAnthropicClient(provider *Provider) *anthropic.Client {
	client := anthropic.NewClient(&anthropic.Configuration{
		API:    provider.BaseURL,
		APIKey: provider.APIKey,
	})
	client.SetHTTPClient(router.client)
	return client
}

func (router *Router) handleOpenAI(ctx *RequestContext, w http.ResponseWriter) (*openai.ChatCompletionResponse, error) {
	client, err := router.newOpenAIClient(ctx.Upstream)
	if err != nil {
		router.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create client: %v", err))
		return nil, err
	}
	if ctx.Input.Stream {
		return router.handleOpenaiNonStream(ctx, w, client)
	}
	return router.handleOpenaiStream(ctx, w, client)
}

func (router *Router) handleAnthropic(ctx *RequestContext, w http.ResponseWriter) (*openai.ChatCompletionResponse, error) {
	if ctx.Input.Stream {
		return router.handleAnthropicStream(ctx, w)
	}
	return router.handleAnthropicNonStream(ctx, w)
}

func (router *Router) handleOpenaiToAnthropicStream(ctx *RequestContext, w http.ResponseWriter) (*openai.ChatCompletionResponse, error) {
	client, err := router.newOpenAIClient(ctx.Upstream)
	if err != nil {
		router.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create client: %v", err))
		return nil, err
	}
	chunks, err := client.CreateChatCompletionStream(ctx, ctx.Input)
	if err != nil {
		slog.Warn("stream upstream error", "error", err)
		router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return nil, err
	}

	resp := anthropic.OpenAIStreamToAnthropicStream(chunks, w, nil)
	return resp, nil
}

func (router *Router) handleOpenaiToAnthropicNonStream(ctx *RequestContext, w http.ResponseWriter) (*openai.ChatCompletionResponse, error) {
	client, err := router.newOpenAIClient(ctx.Upstream)
	if err != nil {
		router.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create client: %v", err))
		return nil, err
	}
	resp, err := client.CreateChatCompletion(ctx, ctx.Input)
	if err != nil {
		router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return nil, err
	}
	anthResp := anthropic.NewAnthropicResponseFromChatCompletionResponse(resp)
	out, _ := json.Marshal(anthResp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)

	return resp, nil
}

func (router *Router) handleOpenaiNonStream(ctx *RequestContext, w http.ResponseWriter, client openai.ChatClient) (*openai.ChatCompletionResponse, error) {
	stream := openai.NewResponseWriter(w)
	chunks, err := client.CreateChatCompletionStream(ctx, ctx.Input)
	if err != nil {
		slog.Warn("stream upstream error", "error", err)
		stream.SendError(err)
		return nil, err
	}

	assembler := openai.NewResponseAssembler()
	for chunk := range chunks {
		assembler.Update(chunk)
		stream.Send(chunk)
	}
	stream.Done()

	return assembler.Build(), nil
}

func (router *Router) handleOpenaiStream(ctx *RequestContext, w http.ResponseWriter, client openai.ChatClient) (*openai.ChatCompletionResponse, error) {
	resp, err := client.CreateChatCompletion(ctx, ctx.Input)
	if err != nil {
		router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return nil, err
	}

	out, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)

	return resp, nil
}

func (router *Router) handleAnthropicStream(ctx *RequestContext, w http.ResponseWriter) (*openai.ChatCompletionResponse, error) {
	client := router.newAnthropicClient(ctx.Upstream)
	anthReq := anthropic.NewAnthropicRequestFromChatCompletionRequest(ctx.Input)

	switch ctx.OutputFormat {
	case ProviderTypeOpenAI:
		chatReq := anthropic.NewChatCompletionRequestFromAnthropicRequest(anthReq)
		chunks, err := client.CreateChatCompletionStream(ctx, chatReq)
		if err != nil {
			router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
			return nil, err
		}
		stream := openai.NewResponseWriter(w)
		assembler := openai.NewResponseAssembler()
		for chunk := range chunks {
			assembler.Update(chunk)
			stream.Send(chunk)
		}
		stream.Done()
		return assembler.Build(), nil
	default:
		ms, err := client.CreateMessageStream(ctx, anthReq)
		if err != nil {
			router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
			return nil, err
		}
		anthStream := anthropic.NewResponseWriter(w)
		ch := anthropic.AnthropicStreamToChatCompletionStream(ms, func(event anthropic.Event) {
			anthStream.Send(event)
		})
		resp := openai.AssembleFromChunks(ch)
		anthStream.Done()
		return resp, nil
	}
}

func (router *Router) handleAnthropicNonStream(ctx *RequestContext, w http.ResponseWriter) (*openai.ChatCompletionResponse, error) {
	client := router.newAnthropicClient(ctx.Upstream)
	anthReq := anthropic.NewAnthropicRequestFromChatCompletionRequest(ctx.Input)

	switch ctx.OutputFormat {
	case ProviderTypeAnthropic:
		resp, err := client.CreateMessage(ctx, anthReq)
		if err != nil {
			router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
			return nil, err
		}
		data, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
		chatResp := anthropic.NewChatCompletionResponseFromAnthropicResponse(resp)
		return chatResp, nil
	case ProviderTypeOpenAI:
		chatReq := anthropic.NewChatCompletionRequestFromAnthropicRequest(anthReq)
		resp, err := client.CreateChatCompletion(ctx, chatReq)
		if err != nil {
			router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
			return nil, err
		}
		out, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(out)
		return resp, nil
	default:
		return nil, fmt.Errorf("unsupported output format: %s", ctx.OutputFormat)
	}
}
