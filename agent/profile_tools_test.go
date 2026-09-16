package agent

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/mcp"
)

func TestUseAgentSkipsUnselectedMCPServers(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {
				Provider:  "openai",
				ModelName: "test",
				Workspace: t.TempDir(),
				Tools:     []string{"web_search"},
			},
		},
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIBase: "http://example.invalid"},
		},
		McpServers: map[string]*mcp.McpServerConfig{
			"unused": {Type: "streamablehttp", URL: server.URL},
		},
	})

	if _, err := m.UseAgent("default"); err != nil {
		t.Fatalf("UseAgent: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("unselected MCP server received %d requests", got)
	}
}
