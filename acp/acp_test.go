package acp

import (
	"context"
	"io"
	"testing"
)

// testHandler is a minimal ACP handler for testing.
type testHandler struct {
	t *testing.T
}

func (h *testHandler) Initialize(ctx context.Context, req *InitializeRequest) (*InitializeResponse, error) {
	return &InitializeResponse{
		ProtocolVersion: 1,
		AgentCapabilities: DefaultAgentCapabilities(),
		AuthMethods:       []AuthMethod{},
		AgentInfo: &Implementation{
			Name:    "test-agent",
			Version: "1.0.0",
		},
	}, nil
}

func (h *testHandler) Authenticate(ctx context.Context, req *AuthenticateRequest) (*AuthenticateResponse, error) {
	return &AuthenticateResponse{}, nil
}

func (h *testHandler) NewSession(ctx context.Context, req *NewSessionRequest, sender SessionUpdateSender) (*NewSessionResponse, error) {
	return &NewSessionResponse{
		SessionID: "test-session-1",
	}, nil
}

func (h *testHandler) Prompt(ctx context.Context, req *PromptRequest, sender SessionUpdateSender) (*PromptResponse, error) {
	sender.Send(SessionUpdate{
		SessionUpdate: "agent_message_chunk",
		Content: ContentBlock{
			Type: "text",
			Text: "Hello from test agent!",
		},
	})
	return &PromptResponse{
		StopReason: StopEndTurn,
	}, nil
}

func (h *testHandler) LoadSession(ctx context.Context, req *LoadSessionRequest, sender SessionUpdateSender) (*LoadSessionResponse, error) {
	return &LoadSessionResponse{}, nil
}

func (h *testHandler) ResumeSession(ctx context.Context, req *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	return &ResumeSessionResponse{}, nil
}

func (h *testHandler) CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error) {
	return &CloseSessionResponse{}, nil
}

func (h *testHandler) DeleteSession(ctx context.Context, req *DeleteSessionRequest) (*DeleteSessionResponse, error) {
	return &DeleteSessionResponse{}, nil
}

func (h *testHandler) ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
	return &ListSessionsResponse{
		Sessions: []SessionInfo{},
	}, nil
}

func (h *testHandler) SetSessionMode(ctx context.Context, req *SetSessionModeRequest) (*SetSessionModeResponse, error) {
	return &SetSessionModeResponse{}, nil
}

func (h *testHandler) SetSessionConfigOption(ctx context.Context, req *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error) {
	return &SetSessionConfigOptionResponse{
		ConfigOptions: []SessionConfigOption{},
	}, nil
}

func (h *testHandler) Logout(ctx context.Context, req *LogoutRequest) (*LogoutResponse, error) {
	return &LogoutResponse{}, nil
}

func setupPipeTest(t *testing.T) (*Client, *Server, func()) {
	// io.Pipe returns (*PipeReader, *PipeWriter)
	// client → server direction
	serverReader, clientWriter := io.Pipe()
	// server → client direction
	clientReader, serverWriter := io.Pipe()

	handler := &testHandler{t: t}
	server := NewServerWithWriter(handler, serverWriter)
	client := NewClient(clientWriter, clientReader)

	done := make(chan struct{})
	go func() {
		server.ServeFromReader(serverReader)
		close(done)
	}()

	cleanup := func() {
		clientWriter.Close()
		serverWriter.Close()
		serverReader.Close()
		clientReader.Close()
		<-done
	}

	return client, server, cleanup
}

func TestFullFlow(t *testing.T) {
	client, _, cleanup := setupPipeTest(t)
	defer cleanup()

	// 1. Initialize
	initResp, err := client.Initialize(&InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: DefaultClientCapabilities(),
		ClientInfo: &Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		},
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if initResp.ProtocolVersion != 1 {
		t.Errorf("expected protocol version 1, got %d", initResp.ProtocolVersion)
	}
	if initResp.AgentInfo == nil || initResp.AgentInfo.Name != "test-agent" {
		t.Errorf("unexpected agent info: %+v", initResp.AgentInfo)
	}

	// 2. NewSession
	sessResp, err := client.NewSession(&NewSessionRequest{
		Cwd:        "/tmp",
		McpServers: []McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	if sessResp.SessionID != "test-session-1" {
		t.Errorf("expected session test-session-1, got %s", sessResp.SessionID)
	}

	// 3. Prompt
	promptResp, err := client.Prompt(&PromptRequest{
		SessionID: "test-session-1",
		Prompt: []ContentBlock{
			{Type: "text", Text: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	if promptResp.StopReason != StopEndTurn {
		t.Errorf("expected end_turn, got %s", promptResp.StopReason)
	}

	// 4. ListSessions
	listResp, err := client.ListSessions(&ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if listResp.Sessions == nil {
		t.Errorf("expected non-nil sessions list")
	}
}

func TestCancelNotification(t *testing.T) {
	client, _, cleanup := setupPipeTest(t)
	defer cleanup()

	err := client.CancelSession("test-session-1")
	if err != nil {
		t.Fatalf("CancelSession failed: %v", err)
	}
}

func TestSendRecvNotifications(t *testing.T) {
	client, _, cleanup := setupPipeTest(t)
	defer cleanup()

	_, err := client.Initialize(&InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: DefaultClientCapabilities(),
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	sessResp, err := client.NewSession(&NewSessionRequest{
		Cwd:        "/tmp",
		McpServers: []McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = client.Prompt(&PromptRequest{
		SessionID: sessResp.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
}
