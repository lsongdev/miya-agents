package acp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeACP(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}

	path, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode not found")
	}

	if !checkHelp(path, "acp") {
		t.Skip("opencode does not support 'acp' subcommand")
	}

	client, err := DialStdio(path, "acp")
	if err != nil {
		t.Fatalf("DialStdio: %v", err)
	}
	defer client.Close()

	testACPFlow(t, client, "OpenCode")
}

func TestMiyaAgentsACP(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}
	if os.Getenv("MIYA_AGENTS_INTEGRATION") != "1" {
		t.Skip("set MIYA_AGENTS_INTEGRATION=1 to run miya-agents ACP integration test")
	}

	binPath := filepath.Join(t.TempDir(), "miya-agents")
	out, err := exec.Command("go", "build", "-o", binPath, "github.com/lsongdev/miya-agents").CombinedOutput()
	if err != nil {
		t.Skipf("build miya-agents: %v\n%s", err, out)
	}

	client, err := DialStdio(binPath, "acp")
	if err != nil {
		t.Fatalf("DialStdio: %v", err)
	}
	defer client.Close()

	testACPFlow(t, client, "miya")
}

func checkHelp(path, subcmd string) bool {
	cmd := exec.Command(path, subcmd, "--help")
	out, _ := cmd.CombinedOutput()
	return len(out) > 0
}

func testACPFlow(t *testing.T, client *Client, agentPrefix string) {
	t.Helper()

	var gotContent string
	client.OnNotification(func(method string, params json.RawMessage) {
		if method != "session/update" {
			return
		}
		var n struct {
			Update struct {
				SessionUpdate string       `json:"sessionUpdate"`
				Content       ContentBlock `json:"content,omitempty"`
			} `json:"update"`
		}
		if err := json.Unmarshal(params, &n); err != nil {
			return
		}
		if n.Update.SessionUpdate == "agent_message_chunk" {
			gotContent += n.Update.Content.Text
		}
	})

	// Initialize
	initResp, err := client.Initialize(&InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: DefaultClientCapabilities(),
		ClientInfo:         &Implementation{Name: "test", Version: "1.0"},
	})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if initResp.AgentInfo == nil {
		t.Fatal("AgentInfo is nil")
	}
	if !strings.HasPrefix(initResp.AgentInfo.Name, agentPrefix) {
		t.Errorf("expected agent name starting with %q, got %q", agentPrefix, initResp.AgentInfo.Name)
	}

	// NewSession
	sessResp, err := client.NewSession(&NewSessionRequest{
		Cwd:        "/tmp",
		McpServers: []McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sessResp.SessionID == "" {
		t.Error("expected non-empty session ID")
	}

	// Prompt
	_, err = client.Prompt(&PromptRequest{
		SessionID: sessResp.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	if gotContent == "" {
		t.Error("expected non-empty content from Prompt streaming, got empty string")
	}
	t.Logf("Agent: %s, received: %q", agentPrefix, gotContent)
}
