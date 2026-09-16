package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/session"
)

func TestUseAgentSelectsProfileTools(t *testing.T) {
	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {
				Provider:    "openai",
				ModelName:   "test",
				Description: "general coordinator",
				Workspace:   t.TempDir(),
				Tools:       []string{"web_search", "delegate"},
			},
			"researcher": {
				Provider:    "openai",
				ModelName:   "test",
				Description: "web research",
			},
		},
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIBase: "http://example.invalid"},
		},
	})

	ag, err := m.UseAgent("default")
	if err != nil {
		t.Fatalf("UseAgent: %v", err)
	}
	if _, ok := ag.tool("web_search"); !ok {
		t.Fatal("web_search not configured")
	}
	delegate, ok := ag.tool("delegate")
	if !ok {
		t.Fatal("delegate not configured")
	}
	if _, ok := ag.tool("exec"); ok {
		t.Fatal("exec should not be configured")
	}

	params := delegate.Def().Function.Parameters
	properties := params["properties"].(map[string]any)
	agent := properties["agent"].(map[string]any)
	names := agent["enum"].([]string)
	if strings.Join(names, ",") != "default,researcher" {
		t.Fatalf("delegate agents = %v", names)
	}
	if !strings.Contains(delegate.Def().Function.Description, "researcher: web research") {
		t.Fatalf("delegate description = %q", delegate.Def().Function.Description)
	}
}

func TestUseAgentRejectsUnknownTool(t *testing.T) {
	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "test", Tools: []string{"missing"}},
		},
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIBase: "http://example.invalid"},
		},
	})

	_, err := m.UseAgent("default")
	if err == nil || !strings.Contains(err.Error(), `unknown tool "missing"`) {
		t.Fatalf("UseAgent error = %v", err)
	}
}

func TestDelegateUsesEphemeralSession(t *testing.T) {
	oldConfigPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = oldConfigPath })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		messages, _ := req["messages"].([]any)
		if len(messages) == 0 {
			t.Fatal("missing delegated task")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"chat_1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"delegated result\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"chat_1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"researcher": {
				Provider:  "openai",
				ModelName: "test",
				Workspace: t.TempDir(),
				Tools:     []string{"web_search"},
			},
		},
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIBase: server.URL},
		},
	})

	result, err := m.Delegate(context.Background(), "researcher", "investigate this")
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if result != "delegated result" {
		t.Fatalf("result = %q", result)
	}
	sessions, err := session.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("Delegate persisted %d sessions, want 0", len(sessions))
	}
}

func TestDelegateLimitsRecursion(t *testing.T) {
	m := NewAgentManager(&config.Config{})
	ctx := context.WithValue(context.Background(), delegateDepthKey{}, maxDelegateDepth)
	_, err := m.Delegate(ctx, "anything", "task")
	if err == nil || !strings.Contains(err.Error(), "maximum delegation depth") {
		t.Fatalf("Delegate error = %v", err)
	}
}
