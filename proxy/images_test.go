package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providercodex "github.com/lsongdev/miya-agents/proxy/providers/codex"
)

func TestCodexImageGenerations(t *testing.T) {
	var upstreamBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/backend-api/codex/images/generations" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer codex-token" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("chatgpt-account-id"); got != "account-1" {
			t.Errorf("account header = %q", got)
		}
		upstreamBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-codex-imagegen-request-id", "image_req_1")
		io.WriteString(w, `{"created":1,"data":[{"b64_json":"aW1hZ2U="}],"quality":"high","size":"1024x1024"}`)
	}))
	defer upstream.Close()

	p := NewProxy()
	provider := providercodex.Provider(staticBearer{token: "codex-token", headers: map[string]string{"chatgpt-account-id": "account-1"}}, "gpt-5-codex")
	provider.BaseURL = upstream.URL + "/backend-api/codex"
	p.AddProvider(provider)

	var observed *ResponseContext
	p.OnResponse(func(ctx *ResponseContext) { observed = ctx })
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"prompt":"a paper fox","quality":"high","size":"1024x1024"}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("x-codex-imagegen-request-id"); got != "image_req_1" {
		t.Errorf("request id header = %q", got)
	}
	var gotRequest map[string]any
	if err := json.Unmarshal(upstreamBody, &gotRequest); err != nil {
		t.Fatal(err)
	}
	if gotRequest["model"] != "gpt-image-2" || gotRequest["prompt"] != "a paper fox" {
		t.Fatalf("upstream request = %#v", gotRequest)
	}
	if !strings.Contains(w.Body.String(), `"b64_json":"aW1hZ2U="`) {
		t.Fatalf("response = %s", w.Body.String())
	}
	if observed == nil || observed.Input == nil || observed.Input.Model != "gpt-image-2" || observed.Error != nil {
		t.Fatalf("observed = %#v", observed)
	}
}

func TestCodexImageGenerationsPassesThroughUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"image quota exceeded","type":"rate_limit_error"}}`)
	}))
	defer upstream.Close()

	p := NewProxy()
	provider := providercodex.Provider(staticBearer{token: "codex-token"}, "gpt-5-codex")
	provider.BaseURL = upstream.URL + "/backend-api/codex"
	p.AddProvider(provider)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-2","prompt":"a fox"}`)))

	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "image quota exceeded") {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestImageGenerationsValidatesRequest(t *testing.T) {
	p := NewProxy()

	for _, test := range []struct {
		name   string
		method string
		body   string
		status int
	}{
		{name: "method", method: http.MethodGet, status: http.StatusMethodNotAllowed},
		{name: "invalid json", method: http.MethodPost, body: `{`, status: http.StatusBadRequest},
		{name: "missing prompt", method: http.MethodPost, body: `{"model":"gpt-image-2"}`, status: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			p.ServeHTTP(w, httptest.NewRequest(test.method, "/v1/images/generations", strings.NewReader(test.body)))
			if w.Code != test.status {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCodexImageModelsAreListed(t *testing.T) {
	p := NewProxy()
	p.AddProvider(providercodex.Provider(staticBearer{token: "codex-token"}, "gpt-5-codex"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id":"gpt-image-2"`) || !strings.Contains(w.Body.String(), `"id":"gpt-image-1.5"`) {
		t.Fatalf("models = %s", w.Body.String())
	}
}
