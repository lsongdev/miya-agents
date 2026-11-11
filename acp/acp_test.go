package acp

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// testHandler is a minimal ACP handler for testing.
type testHandler struct {
	t *testing.T
}

func (h *testHandler) Initialize(ctx context.Context, req *InitializeRequest) (*InitializeResponse, error) {
	return &InitializeResponse{
		ProtocolVersion:   1,
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

func TestClientReceivesNotificationAfterResponse(t *testing.T) {
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()
	client := NewClient(clientWriter, clientReader)
	defer client.Close()
	defer serverReader.Close()
	defer serverWriter.Close()

	notifications := make(chan string, 1)
	client.OnNotification(func(method string, params json.RawMessage) {
		notifications <- method
	})

	go func() {
		var req jsonrpcRequest
		_ = json.NewDecoder(serverReader).Decode(&req)
		_ = json.NewEncoder(serverWriter).Encode(jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"promptCapabilities":{"image":true,"audio":true,"embeddedContext":true},"mcpCapabilities":{"http":true,"sse":true},"sessionCapabilities":{},"auth":{}},"authMethods":[],"agentInfo":{"name":"test","version":"1.0"}}`),
		})
		_ = json.NewEncoder(serverWriter).Encode(jsonrpcNotification{
			JSONRPC: "2.0",
			Method:  "session/update",
			Params:  json.RawMessage(`{"sessionId":"s1","update":{"sessionUpdate":"tool_call"}}`),
		})
	}()

	if _, err := client.Initialize(&InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: DefaultClientCapabilities(),
		ClientInfo:         &Implementation{Name: "test-client", Version: "1.0"},
	}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	select {
	case method := <-notifications:
		if method != "session/update" {
			t.Fatalf("notification method = %q", method)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification after response")
	}
}

func TestSessionUpdateToolCallMarshalShape(t *testing.T) {
	data, err := json.Marshal(SessionUpdate{
		SessionUpdate: "tool_call",
		ToolCall: &ToolCall{
			ToolCallID: "tc-1",
			Title:      "Read",
			Kind:       ToolKindRead,
			Status:     ToolCallInProgress,
			Content: []ToolCallContent{{
				Type:    "content",
				Content: &ContentBlock{Type: "text", Text: `{"path":"README.md"}`},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"content":[{"type":"content","content":{"type":"text","text":"{\"path\":\"README.md\"}"}}],"kind":"read","sessionUpdate":"tool_call","status":"in_progress","title":"Read","toolCallId":"tc-1"}`
	if string(data) != want {
		t.Fatalf("tool_call =\n%s\nwant\n%s", data, want)
	}
}

// Every variant's fields sit next to the discriminator, as the ACP schema
// lays them out, and decode back to the same value.
func TestSessionUpdateWireFormat(t *testing.T) {
	status := ToolCallCompleted
	title := "Renamed"
	for _, tc := range []struct {
		update SessionUpdate
		wire   string
	}{
		{SessionUpdate{SessionUpdate: "agent_message_chunk", Content: ContentBlock{Type: "text", Text: "hi"}},
			`{"content":{"type":"text","text":"hi"},"sessionUpdate":"agent_message_chunk"}`},
		{SessionUpdate{SessionUpdate: "agent_thought_chunk", Content: ContentBlock{Type: "text", Text: "hm"}},
			`{"content":{"type":"text","text":"hm"},"sessionUpdate":"agent_thought_chunk"}`},
		{SessionUpdate{SessionUpdate: "tool_call_update", ToolCallUpdate: &ToolCallUpdate{ToolCallID: "t1", Status: &status}},
			`{"sessionUpdate":"tool_call_update","status":"completed","toolCallId":"t1"}`},
		{SessionUpdate{SessionUpdate: "plan", Plan: &Plan{Entries: []PlanEntry{{Content: "step", Priority: "high", Status: "pending"}}}},
			`{"entries":[{"content":"step","status":"pending","priority":"high"}],"sessionUpdate":"plan"}`},
		{SessionUpdate{SessionUpdate: "available_commands_update", AvailableCommands: &AvailableCommandsUpdate{AvailableCommands: []AvailableCommand{{Name: "compact", Description: "d"}}}},
			`{"availableCommands":[{"name":"compact","description":"d"}],"sessionUpdate":"available_commands_update"}`},
		{SessionUpdate{SessionUpdate: "current_mode_update", CurrentMode: &CurrentModeUpdate{CurrentModeID: "code"}},
			`{"currentModeId":"code","sessionUpdate":"current_mode_update"}`},
		{SessionUpdate{SessionUpdate: "session_info_update", SessionInfo: &SessionInfoUpdate{Title: &title}},
			`{"sessionUpdate":"session_info_update","title":"Renamed"}`},
		{SessionUpdate{SessionUpdate: "usage_update", Usage: &UsageUpdate{Size: 1000, Used: 80}},
			`{"sessionUpdate":"usage_update","size":1000,"used":80}`},
		{SessionUpdate{SessionUpdate: "agent_message_chunk", Content: ContentBlock{Type: "text", Text: "x"}, Meta: Meta{"k": "v"}},
			`{"_meta":{"k":"v"},"content":{"type":"text","text":"x"},"sessionUpdate":"agent_message_chunk"}`},
	} {
		data, err := json.Marshal(tc.update)
		if err != nil {
			t.Fatalf("%s: Marshal: %v", tc.update.SessionUpdate, err)
		}
		if string(data) != tc.wire {
			t.Errorf("%s =\n%s\nwant\n%s", tc.update.SessionUpdate, data, tc.wire)
			continue
		}
		var decoded SessionUpdate
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("%s: Unmarshal: %v", tc.update.SessionUpdate, err)
		}
		again, _ := json.Marshal(decoded)
		if string(again) != tc.wire {
			t.Errorf("%s did not round trip:\n%s", tc.update.SessionUpdate, again)
		}
	}
}

func TestSessionUpdateDecodesUnknownVariant(t *testing.T) {
	var update SessionUpdate
	if err := json.Unmarshal([]byte(`{"sessionUpdate":"future_thing","x":1}`), &update); err != nil {
		t.Fatal(err)
	}
	if update.SessionUpdate != "future_thing" {
		t.Fatalf("update = %#v", update)
	}
}

func TestNotificationHandlerDispatchesSessionUpdateVariants(t *testing.T) {
	receiver := &variantRecorder{}
	handler := NewNotificationHandler(receiver)

	handler("session/update", json.RawMessage(`{
		"sessionId":"s1",
		"update":{
			"sessionUpdate":"agent_message_chunk",
			"content":{"type":"text","text":"hello"}
		}
	}`))

	if receiver.sessionID != "s1" {
		t.Fatalf("sessionID = %q", receiver.sessionID)
	}
	if receiver.text != "hello" {
		t.Fatalf("text = %q", receiver.text)
	}
	if receiver.sessionUpdates != 1 {
		t.Fatalf("sessionUpdates = %d", receiver.sessionUpdates)
	}
}

type variantRecorder struct {
	DefaultNotificationReceiver
	sessionID      SessionID
	text           string
	sessionUpdates int
}

func (r *variantRecorder) SessionUpdate(notification *SessionNotification) {
	r.sessionUpdates++
}

func (r *variantRecorder) AgentMessageChunk(sessionID SessionID, content ContentBlock, messageID *MessageID) {
	r.sessionID = sessionID
	r.text = content.Text
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

type clientCallingHandler struct {
	testHandler
}

func (h *clientCallingHandler) Prompt(ctx context.Context, req *PromptRequest, sender SessionUpdateSender) (*PromptResponse, error) {
	client, ok := ClientFromSender(sender)
	if !ok {
		return nil, context.Canceled
	}
	resp, err := client.ReadTextFile(ctx, &ReadTextFileRequest{
		SessionID: req.SessionID,
		Path:      "/tmp/example.txt",
	})
	if err != nil {
		return nil, err
	}
	if err := sender.Send(SessionUpdate{
		SessionUpdate: "agent_message_chunk",
		Content:       ContentBlock{Type: "text", Text: resp.Content},
	}); err != nil {
		return nil, err
	}
	return &PromptResponse{StopReason: StopEndTurn}, nil
}

type testClientHandler struct {
	DefaultClientHandler
	t *testing.T
}

func (h *testClientHandler) ReadTextFile(ctx context.Context, req *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	if req.SessionID != "test-session-1" {
		h.t.Fatalf("SessionID = %q", req.SessionID)
	}
	if req.Path != "/tmp/example.txt" {
		h.t.Fatalf("Path = %q", req.Path)
	}
	return &ReadTextFileResponse{Content: "file contents"}, nil
}

func TestAgentCanCallClientHandler(t *testing.T) {
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	server := NewServerWithWriter(&clientCallingHandler{}, serverWriter)
	client := NewClient(clientWriter, clientReader)
	client.OnRequest(&testClientHandler{t: t})

	updates := make(chan SessionUpdate, 1)
	client.OnNotification(NewNotificationHandler(updateRecorder{updates: updates}))

	done := make(chan struct{})
	go func() {
		_ = server.ServeFromReader(serverReader)
		close(done)
	}()
	defer func() {
		_ = clientWriter.Close()
		_ = serverWriter.Close()
		_ = serverReader.Close()
		_ = clientReader.Close()
		<-done
	}()

	promptResp, err := client.Prompt(&PromptRequest{
		SessionID: "test-session-1",
		Prompt:    []ContentBlock{{Type: "text", Text: "read"}},
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if promptResp.StopReason != StopEndTurn {
		t.Fatalf("StopReason = %q", promptResp.StopReason)
	}

	select {
	case update := <-updates:
		if update.SessionUpdate != "agent_message_chunk" {
			t.Fatalf("SessionUpdate = %q", update.SessionUpdate)
		}
		if update.Content.Text != "file contents" {
			t.Fatalf("Content.Text = %q", update.Content.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for streamed file content")
	}
}

type updateRecorder struct {
	DefaultNotificationReceiver
	updates chan SessionUpdate
}

func (r updateRecorder) SessionUpdate(notification *SessionNotification) {
	r.updates <- notification.Update
}
