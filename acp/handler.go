package acp

import "context"

// SessionUpdateSender allows the agent to send session/update notifications.
type SessionUpdateSender interface {
	Send(update SessionUpdate) error
}

// Handler defines the interface for ACP agent implementations.
type Handler interface {
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
