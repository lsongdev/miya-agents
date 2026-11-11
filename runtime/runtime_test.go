package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lsongdev/miya-agents/agent"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/mcp"
)

func TestResolveAgentNameFallsBackWhenSessionProfileMissing(t *testing.T) {
	m := New(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"coding": {Provider: "openai", ModelName: "gpt-4"},
		},
		Providers: map[string]*config.ProviderConfig{},
	})

	got, err := m.ResolveAgent("default")
	if err != nil {
		t.Fatalf("resolveAgentName: %v", err)
	}
	if got != "coding" {
		t.Fatalf("resolveAgentName = %q, want coding", got)
	}
}

func TestResolveAgentNamePrefersDefault(t *testing.T) {
	m := New(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "gpt-4"},
			"coding":  {Provider: "openai", ModelName: "gpt-4"},
		},
		Providers: map[string]*config.ProviderConfig{},
	})

	got, err := m.ResolveAgent("")
	if err != nil {
		t.Fatalf("resolveAgentName: %v", err)
	}
	if got != "default" {
		t.Fatalf("resolveAgentName = %q, want default", got)
	}
}

func TestResolveAgentNameReportsEmptyProfiles(t *testing.T) {
	m := New(&config.Config{
		Profiles:  map[string]*config.ProfileConfig{},
		Providers: map[string]*config.ProviderConfig{},
	})

	if _, err := m.ResolveAgent("default"); err == nil {
		t.Fatal("resolveAgentName succeeded, want error")
	}
}

func TestUseAgentIncludesConfiguredMCPTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			writeMCPEvent(t, w, req.ID, map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "coffee-test",
					"version": "1.0.0",
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeMCPEvent(t, w, req.ID, map[string]any{
				"tools": []map[string]any{{
					"name":        "queryShopList",
					"description": "query shops",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{},
					},
				}},
			})
		default:
			t.Fatalf("unexpected method: %s", req.Method)
		}
	}))
	defer server.Close()

	m := New(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "gpt-4"},
		},
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIKey: "test-key"},
		},
		McpServers: map[string]*mcp.McpServerConfig{
			"coffee": {Type: "streamablehttp", URL: server.URL},
		},
	})

	ag, err := m.Agent("default")
	if err != nil {
		t.Fatalf("UseAgent: %v", err)
	}
	if findTool(ag, "mcp_coffee_queryShopList") == nil {
		t.Fatalf("missing MCP tool; tools = %#v", ag.Tools)
	}
}

func TestUseAgentUsesAnthropicProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req["model"] != "claude-test" {
			t.Fatalf("model = %#v", req["model"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	m := New(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "claude", ModelName: "claude-test", Workspace: t.TempDir()},
		},
		Providers: map[string]*config.ProviderConfig{
			"claude": {Type: "anthropic", APIBase: server.URL, APIKey: "test-key"},
		},
	})
	ag, err := m.Agent("default")
	if err != nil {
		t.Fatal(err)
	}
	var builder llm.Builder
	request := &llm.Request{Model: ag.ModelName, Messages: []llm.Message{llm.UserText("hi")}}
	for delta, err := range ag.Model.Stream(context.Background(), request) {
		if err != nil {
			t.Fatal(err)
		}
		builder.Add(delta)
	}
	if got := builder.Message().Text(); got != "hello" {
		t.Fatalf("content = %q", got)
	}
}

func writeMCPEvent(t *testing.T, w http.ResponseWriter, id int64, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
}

// findTool returns the agent's tool with the given name, or nil.
func findTool(ag *agent.Agent, name string) agent.Tool {
	for _, tool := range ag.Tools {
		if tool.Def().Name == name {
			return tool
		}
	}
	return nil
}
