package main

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/lsongdev/miya-agents/acp"
)

// echoAgent is a simple ACP agent that echoes back user messages.
type echoAgent struct{}

func (a *echoAgent) Initialize(ctx context.Context, req *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	log.Printf("[agent] Initialize received (protocol v%d)", req.ProtocolVersion)
	return &acp.InitializeResponse{
		ProtocolVersion:    1,
		AgentCapabilities:  acp.DefaultAgentCapabilities(),
		AuthMethods:        []acp.AuthMethod{},
		AgentInfo:          &acp.Implementation{Name: "echo-agent", Version: "0.1.0"},
	}, nil
}

func (a *echoAgent) Authenticate(ctx context.Context, req *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	return &acp.AuthenticateResponse{}, nil
}

func (a *echoAgent) NewSession(ctx context.Context, req *acp.NewSessionRequest, sender acp.SessionUpdateSender) (*acp.NewSessionResponse, error) {
	log.Printf("[agent] NewSession: cwd=%s, mcpServers=%d", req.Cwd, len(req.McpServers))
	return &acp.NewSessionResponse{
		SessionID:    "session-1",
		ConfigOptions: nil,
	}, nil
}

func (a *echoAgent) Prompt(ctx context.Context, req *acp.PromptRequest, sender acp.SessionUpdateSender) (*acp.PromptResponse, error) {
	// Collect user input from the prompt
	var userText string
	for _, block := range req.Prompt {
		if block.Type == "text" {
			userText += block.Text
		}
	}
	log.Printf("[agent] Prompt received: %q", userText)

	// Stream agent message chunk
	sender.Send(acp.SessionUpdate{
		SessionUpdate: "agent_message_chunk",
		Content: acp.ContentBlock{
			Type: "text",
			Text: "You said: " + userText,
		},
	})

	// Stream a tool call
	sender.Send(acp.SessionUpdate{
		SessionUpdate: "tool_call",
		ToolCall: &acp.ToolCall{
			ToolCallID: "tc-1",
			Title:      "Echo tool",
			Kind:       acp.ToolKindOther,
			Status:     acp.ToolCallCompleted,
			Content: []acp.ToolCallContent{
				{Type: "content", Content: &acp.ContentBlock{Type: "text", Text: userText}},
			},
		},
	})

	return &acp.PromptResponse{StopReason: acp.StopEndTurn}, nil
}

func (a *echoAgent) LoadSession(ctx context.Context, req *acp.LoadSessionRequest, sender acp.SessionUpdateSender) (*acp.LoadSessionResponse, error) {
	return &acp.LoadSessionResponse{}, nil
}

func (a *echoAgent) ResumeSession(ctx context.Context, req *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
	return &acp.ResumeSessionResponse{}, nil
}

func (a *echoAgent) CloseSession(ctx context.Context, req *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
	log.Printf("[agent] CloseSession: %s", req.SessionID)
	return &acp.CloseSessionResponse{}, nil
}

func (a *echoAgent) DeleteSession(ctx context.Context, req *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error) {
	return &acp.DeleteSessionResponse{}, nil
}

func (a *echoAgent) ListSessions(ctx context.Context, req *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	return &acp.ListSessionsResponse{Sessions: []acp.SessionInfo{}}, nil
}

func (a *echoAgent) SetSessionMode(ctx context.Context, req *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
	return &acp.SetSessionModeResponse{}, nil
}

func (a *echoAgent) SetSessionConfigOption(ctx context.Context, req *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	return &acp.SetSessionConfigOptionResponse{ConfigOptions: []acp.SessionConfigOption{}}, nil
}

func (a *echoAgent) Logout(ctx context.Context, req *acp.LogoutRequest) (*acp.LogoutResponse, error) {
	return &acp.LogoutResponse{}, nil
}

func main() {
	// Create pipes for in-process client-server communication
	// io.Pipe returns (*PipeReader, *PipeWriter)
	serverReader, clientWriter := io.Pipe() // client writes → server reads
	clientReader, serverWriter := io.Pipe() // server writes → client reads

	// Start the agent server in a goroutine
	agent := &echoAgent{}
	server := acp.NewServerWithWriter(agent, serverWriter)
	go func() {
		if err := server.ServeFromReader(serverReader); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Create the client connected to the server
	client := acp.NewClient(clientWriter, clientReader)
	defer client.Close()

	fmt.Println("=== ACP Example: Echo Agent ===")

	// 1. Initialize
	fmt.Println("\n[client] Sending initialize...")
	initResp, err := client.Initialize(&acp.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: acp.DefaultClientCapabilities(),
		ClientInfo:         &acp.Implementation{Name: "example-client", Version: "1.0.0"},
	})
	if err != nil {
		log.Fatalf("initialize: %v", err)
	}
	fmt.Printf("[client] Agent: %s v%s\n",
		initResp.AgentInfo.Name, initResp.AgentInfo.Version)

	// 2. New Session
	fmt.Println("\n[client] Creating new session...")
	sessResp, err := client.NewSession(&acp.NewSessionRequest{
		Cwd:        "/tmp",
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		log.Fatalf("new session: %v", err)
	}
	fmt.Printf("[client] Session: %s\n", sessResp.SessionID)

	// 3. Prompt
	fmt.Println("\n[client] Sending prompt: \"Hello, ACP!\"...")
	promptResp, err := client.Prompt(&acp.PromptRequest{
		SessionID: sessResp.SessionID,
		Prompt: []acp.ContentBlock{
			{Type: "text", Text: "Hello, ACP!"},
		},
	})
	if err != nil {
		log.Fatalf("prompt: %v", err)
	}
	fmt.Printf("[client] Stop reason: %s\n", promptResp.StopReason)

	// 4. List Sessions
	fmt.Println("\n[client] Listing sessions...")
	listResp, err := client.ListSessions(&acp.ListSessionsRequest{})
	if err != nil {
		log.Fatalf("list sessions: %v", err)
	}
	fmt.Printf("[client] Sessions count: %d\n", len(listResp.Sessions))

	// 5. Close Session
	fmt.Println("\n[client] Closing session...")
	_, err = client.CloseSession(&acp.CloseSessionRequest{
		SessionID: sessResp.SessionID,
	})
	if err != nil {
		log.Fatalf("close session: %v", err)
	}

	fmt.Println("\n=== Done ===")
}
