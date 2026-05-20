package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/lsongdev/miya-agents/acp"
)

type echoAgent struct{}

func (a *echoAgent) Initialize(ctx context.Context, req *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{
		ProtocolVersion:   1,
		AgentCapabilities: acp.DefaultAgentCapabilities(),
		AuthMethods:       []acp.AuthMethod{},
		AgentInfo:         &acp.Implementation{Name: "echo-agent", Version: "0.1.0"},
	}, nil
}

func (a *echoAgent) Authenticate(ctx context.Context, req *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	return &acp.AuthenticateResponse{}, nil
}

func (a *echoAgent) NewSession(ctx context.Context, req *acp.NewSessionRequest, sender acp.SessionUpdateSender) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "session-1"}, nil
}

func (a *echoAgent) Prompt(ctx context.Context, req *acp.PromptRequest, sender acp.SessionUpdateSender) (*acp.PromptResponse, error) {
	var text string
	for _, b := range req.Prompt {
		if b.Type == "text" {
			text += b.Text
		}
	}
	sender.Send(acp.SessionUpdate{
		SessionUpdate: "agent_message_chunk",
		Content:       acp.ContentBlock{Type: "text", Text: "echo: " + text},
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
	log.SetOutput(os.Stderr)
	log.SetPrefix("[agent] ")
	log.Println("starting ACP agent on stdio")

	handler := &echoAgent{}
	server := acp.NewServer(handler)
	if err := server.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
