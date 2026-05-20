package router

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/lsongdev/miya-agents/anthropic"
	"github.com/lsongdev/miya-agents/openai"
)

// HandleResponses serves the OpenAI /v1/responses API endpoint. It converts
// the request to a ChatCompletionRequest under the hood, dispatches to the
// configured provider (OpenAI or Anthropic), and renders the response back
// in Responses API format.
func (router *Router) HandleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		router.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var respReq openai.ResponseRequest
	if err := json.NewDecoder(r.Body).Decode(&respReq); err != nil {
		router.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	chatReq := openai.NewChatCompletionRequestFromResponseRequest(&respReq)
	ctx := &RequestContext{
		RequestID:    router.nextRequestID(),
		Request:      r,
		Response:     w,
		Input:        chatReq,
		OutputFormat: OutputFormatResponses,
	}

	start := time.Now()

	if router.onRequest != nil {
		if err := router.onRequest(ctx); err != nil {
			if reqErr, ok := err.(*RequestError); ok {
				router.writeError(w, reqErr.Status, reqErr.Message)
			} else {
				router.writeError(w, http.StatusForbidden, fmt.Sprintf("request rejected: %v", err))
			}
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

	var (
		chatResp *openai.ChatCompletionResponse
		respErr  error
	)

	switch ctx.Upstream.Type {
	case "", ProviderTypeOpenAI:
		chatResp, respErr = router.handleResponsesViaOpenAI(ctx, &respReq, w)
	case ProviderTypeAnthropic:
		chatResp, respErr = router.handleResponsesViaAnthropic(ctx, &respReq, w)
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

func (router *Router) handleResponsesViaOpenAI(ctx *RequestContext, respReq *openai.ResponseRequest, w http.ResponseWriter) (*openai.ChatCompletionResponse, error) {
	client, err := router.newOpenAIClient(ctx.Upstream)
	if err != nil {
		router.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create client: %v", err))
		return nil, err
	}

	if ctx.Input.Stream {
		chunks, err := client.CreateChatCompletionStream(ctx.Request.Context(), ctx.Input)
		if err != nil {
			slog.Warn("stream upstream error", "error", err)
			router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
			return nil, err
		}
		chatResp := openai.ConvertChatCompletionStreamToResponsesStream(respReq, chunks, w)
		return chatResp, nil
	}

	chatResp, err := client.CreateChatCompletion(ctx.Request.Context(), ctx.Input)
	if err != nil {
		router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return nil, err
	}
	respObj := openai.NewResponseObjectFromChatCompletionResponse(respReq, chatResp)
	out, _ := json.Marshal(respObj)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)
	return chatResp, nil
}

func (router *Router) handleResponsesViaAnthropic(ctx *RequestContext, respReq *openai.ResponseRequest, w http.ResponseWriter) (*openai.ChatCompletionResponse, error) {
	client := router.newAnthropicClient(ctx.Upstream)
	anthReq := anthropic.NewAnthropicRequestFromChatCompletionRequest(ctx.Input)

	if ctx.Input.Stream {
		anthReq.Stream = true
		ms, err := client.CreateMessageStream(ctx.Request.Context(), anthReq)
		if err != nil {
			router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
			return nil, err
		}
		chunks := anthropic.AnthropicStreamToChatCompletionStream(ms, nil)
		chatResp := openai.ConvertChatCompletionStreamToResponsesStream(respReq, chunks, w)
		return chatResp, nil
	}

	anthResp, err := client.CreateMessage(ctx.Request.Context(), anthReq)
	if err != nil {
		router.writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return nil, err
	}
	chatResp := anthropic.NewChatCompletionResponseFromAnthropicResponse(anthResp)
	respObj := openai.NewResponseObjectFromChatCompletionResponse(respReq, chatResp)
	out, _ := json.Marshal(respObj)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)
	return chatResp, nil
}
