package acp

import (
	"context"
	"encoding/json"
)

// NotificationHandler receives raw JSON-RPC notifications.
type NotificationHandler func(method string, params json.RawMessage)

// NotificationReceiver receives parsed ACP notifications.
type NotificationReceiver interface {
	SessionUpdate(notification *SessionNotification)
	UserMessageChunk(sessionID SessionID, content ContentBlock, messageID *MessageID)
	AgentMessageChunk(sessionID SessionID, content ContentBlock, messageID *MessageID)
	AgentThoughtChunk(sessionID SessionID, thought string, content ContentBlock)
	ToolCall(sessionID SessionID, toolCall *ToolCall)
	ToolCallUpdate(sessionID SessionID, update *ToolCallUpdate)
	Plan(sessionID SessionID, plan *Plan)
	AvailableCommandsUpdate(sessionID SessionID, update *AvailableCommandsUpdate)
	CurrentModeUpdate(sessionID SessionID, update *CurrentModeUpdate)
	ConfigOptionUpdate(sessionID SessionID, update *ConfigOptionUpdate)
	SessionInfoUpdate(sessionID SessionID, update *SessionInfoUpdate)
	UsageUpdate(sessionID SessionID, update *UsageUpdate)
	UnknownSessionUpdate(notification *SessionNotification)
	UnknownNotification(method string, params json.RawMessage)
	InvalidNotification(method string, params json.RawMessage, err error)
}

// SessionUpdateSender allows the agent to send session/update notifications.
type SessionUpdateSender interface {
	Send(update SessionUpdate) error
}

// ClientCaller allows an agent handler to call client-side ACP methods.
type ClientCaller interface {
	RequestPermission(ctx context.Context, req *RequestPermissionRequest) (*RequestPermissionResponse, error)
	ReadTextFile(ctx context.Context, req *ReadTextFileRequest) (*ReadTextFileResponse, error)
	WriteTextFile(ctx context.Context, req *WriteTextFileRequest) (*WriteTextFileResponse, error)
	CreateTerminal(ctx context.Context, req *CreateTerminalRequest) (*CreateTerminalResponse, error)
	TerminalOutput(ctx context.Context, req *TerminalOutputRequest) (*TerminalOutputResponse, error)
	ReleaseTerminal(ctx context.Context, req *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error)
	WaitForTerminalExit(ctx context.Context, req *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error)
	KillTerminal(ctx context.Context, req *KillTerminalRequest) (*KillTerminalResponse, error)
}

// ClientHandler handles ACP requests sent by an agent to its client.
type ClientHandler interface {
	RequestPermission(ctx context.Context, req *RequestPermissionRequest) (*RequestPermissionResponse, error)
	ReadTextFile(ctx context.Context, req *ReadTextFileRequest) (*ReadTextFileResponse, error)
	WriteTextFile(ctx context.Context, req *WriteTextFileRequest) (*WriteTextFileResponse, error)
	CreateTerminal(ctx context.Context, req *CreateTerminalRequest) (*CreateTerminalResponse, error)
	TerminalOutput(ctx context.Context, req *TerminalOutputRequest) (*TerminalOutputResponse, error)
	ReleaseTerminal(ctx context.Context, req *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error)
	WaitForTerminalExit(ctx context.Context, req *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error)
	KillTerminal(ctx context.Context, req *KillTerminalRequest) (*KillTerminalResponse, error)
}

// ClientFromSender returns the client caller attached to a server-provided sender.
func ClientFromSender(sender SessionUpdateSender) (ClientCaller, bool) {
	type clientSender interface {
		Client() ClientCaller
	}
	s, ok := sender.(clientSender)
	if !ok {
		return nil, false
	}
	return s.Client(), true
}

// ServerHandler defines the interface for ACP agent implementations.
type ServerHandler interface {
	Initialize(ctx context.Context, req *InitializeRequest) (*InitializeResponse, error)
	Authenticate(ctx context.Context, req *AuthenticateRequest) (*AuthenticateResponse, error)
	NewSession(ctx context.Context, req *NewSessionRequest, sender SessionUpdateSender) (*NewSessionResponse, error)
	Prompt(ctx context.Context, req *PromptRequest, sender SessionUpdateSender) (*PromptResponse, error)
	LoadSession(ctx context.Context, req *LoadSessionRequest, sender SessionUpdateSender) (*LoadSessionResponse, error)
	ResumeSession(ctx context.Context, req *ResumeSessionRequest) (*ResumeSessionResponse, error)
	CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error)
	DeleteSession(ctx context.Context, req *DeleteSessionRequest) (*DeleteSessionResponse, error)
	ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error)
	SetSessionMode(ctx context.Context, req *SetSessionModeRequest) (*SetSessionModeResponse, error)
	SetSessionConfigOption(ctx context.Context, req *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
	Logout(ctx context.Context, req *LogoutRequest) (*LogoutResponse, error)
}
