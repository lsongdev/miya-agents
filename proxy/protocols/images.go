package protocols

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/proxy/providers"
)

const defaultImageModel = "gpt-image-2"

// ImageGenerations serves the OpenAI-compatible /v1/images/generations API.
// Codex providers forward it to the ChatGPT subscription-backed image service.
func (env *Env) ImageGenerations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorForProtocol(w, providers.ProtocolOpenAIImages, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	body, ok := readRequestBody(w, r, providers.ProtocolOpenAIImages)
	if !ok {
		return
	}
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		WriteErrorForProtocol(w, providers.ProtocolOpenAIImages, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	prompt, _ := request["prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		WriteErrorForProtocol(w, providers.ProtocolOpenAIImages, http.StatusBadRequest, "prompt is required")
		return
	}
	model, _ := request["model"].(string)
	if strings.TrimSpace(model) == "" {
		model = defaultImageModel
		request["model"] = model
		body, _ = json.Marshal(request)
	}

	ctx := &providers.RequestContext{
		RequestID:   env.NextRequestID(),
		Request:     r,
		Response:    w,
		Input:       &openai.ChatCompletionRequest{Model: model},
		RawInput:    body,
		InputFormat: providers.ProtocolOpenAIImages,
	}
	start := time.Now()
	if !env.begin(ctx, w) {
		return
	}
	if ctx.Upstream == nil {
		ctx.Upstream = env.imageProvider(model)
	}
	if ctx.Upstream == nil {
		WriteErrorForProtocol(w, providers.ProtocolOpenAIImages, http.StatusBadRequest, fmt.Sprintf("no image provider available for model %s", model))
		env.end(ctx, r, nil, fmt.Errorf("no image provider available for model %s", model), start)
		return
	}

	err := env.forwardImageGeneration(ctx, body, w)
	env.end(ctx, r, nil, err, start)
}

func (env *Env) imageProvider(model string) *providers.Provider {
	if env.ListProviders == nil {
		return nil
	}
	for _, provider := range env.ListProviders() {
		if provider.SupportsImageModel(model) {
			return provider
		}
	}
	return nil
}

func (env *Env) forwardImageGeneration(ctx *providers.RequestContext, body []byte, w http.ResponseWriter) error {
	request, err := ctx.Upstream.NewEndpointRequest(ctx.Request.Context(), http.MethodPost, "images/generations", body)
	if err != nil {
		WriteErrorForProtocol(w, providers.ProtocolOpenAIImages, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return err
	}
	copyForwardHeaders(request.Header, ctx.Request.Header)
	response, err := ctx.Upstream.Do(env.HTTPClient, request)
	if err != nil {
		WriteErrorForProtocol(w, providers.ProtocolOpenAIImages, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return err
	}
	defer response.Body.Close()
	copyResponseHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	if err := copyResponseBody(w, response.Body); err != nil {
		return fmt.Errorf("copy upstream response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("upstream returned %s", response.Status)
	}
	return nil
}
