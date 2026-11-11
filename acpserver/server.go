// Package acpserver exposes the agent runtime over the Agent Client Protocol.
package acpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/agent"
	"github.com/lsongdev/miya-agents/runtime"
	"github.com/lsongdev/miya-agents/session"
)

// Server implements acp.ServerHandler on top of a Runtime. Sessions live in
// the session store; the server holds no conversation state of its own.
type Server struct {
	rt *runtime.Runtime
}

func New(rt *runtime.Runtime) *Server {
	return &Server{rt: rt}
}

func (srv *Server) Initialize(ctx context.Context, req *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	caps := acp.DefaultAgentCapabilities()
	caps.LoadSession = true
	caps.SessionCapabilities = acp.SessionCapabilities{
		List:   &acp.SessionListCapabilities{},
		Resume: &acp.SessionResumeCapabilities{},
		Close:  &acp.SessionCloseCapabilities{},
		Delete: &acp.SessionDeleteCapabilities{},
	}

	return &acp.InitializeResponse{
		ProtocolVersion:   req.ProtocolVersion,
		AgentCapabilities: caps,
		AgentInfo: &acp.Implementation{
			Name:    "miya",
			Version: "0.1.0",
		},
	}, nil
}

func (srv *Server) Authenticate(ctx context.Context, req *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	return &acp.AuthenticateResponse{}, nil
}

func (srv *Server) NewSession(ctx context.Context, req *acp.NewSessionRequest, sender acp.SessionUpdateSender) (*acp.NewSessionResponse, error) {
	requestedProfile, _ := req.Meta[acp.MiyaProfileMetaKey].(string)
	requestedProfile = strings.TrimSpace(requestedProfile)
	var agentName string
	var err error
	if requestedProfile != "" {
		if !srv.rt.HasProfile(requestedProfile) {
			return nil, fmt.Errorf("profile not found: %s", requestedProfile)
		}
		agentName = requestedProfile
	} else {
		agentName, err = srv.rt.DefaultAgent()
	}
	if err != nil {
		return nil, err
	}

	sess, err := session.Create(agentName)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &acp.NewSessionResponse{
		SessionID: acp.SessionID(sess.ID),
		Meta:      acp.Meta{acp.MiyaProfileMetaKey: agentName},
	}, nil
}

func (srv *Server) Prompt(ctx context.Context, req *acp.PromptRequest, sender acp.SessionUpdateSender) (*acp.PromptResponse, error) {
	sess, err := session.Load(string(req.SessionID))
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	agentName, err := srv.rt.ResolveAgent(sess.AgentName)
	if err != nil {
		return nil, err
	}
	if sess.AgentName != agentName {
		sess.AgentName = agentName
	}

	var textParts []string
	for _, block := range req.Prompt {
		if block.Text != "" {
			textParts = append(textParts, block.Text)
		}
	}
	prompt := strings.Join(textParts, "\n")

	ag, err := srv.rt.Agent(agentName)
	if err != nil {
		return nil, fmt.Errorf("use agent: %w", err)
	}
	sink := acpSink(sender)
	// Sent here rather than from NewSession, whose client does not know the
	// session id until that call returns.
	if err := advertiseCommands(sender); err != nil {
		return nil, err
	}

	if instructions, ok := agent.CompactCommand(prompt); ok {
		reply, err := agent.RunCompactCommand(ctx, ag, sess, instructions)
		if err != nil {
			return nil, err
		}
		if err := sender.Send(assistantMessageUpdate(reply)); err != nil {
			return nil, err
		}
		return &acp.PromptResponse{StopReason: acp.StopEndTurn}, nil
	}

	if err := sess.AddUser(prompt); err != nil {
		return nil, fmt.Errorf("save prompt: %w", err)
	}
	if sess.Title == "" {
		if title := sess.DisplayTitle(); title != "" {
			if err := sess.SetTitle(title); err != nil {
				return nil, fmt.Errorf("save title: %w", err)
			}
			if err := sink(agent.Event{Type: agent.EventSessionInfo, Title: title}); err != nil {
				return nil, err
			}
		}
	}
	if err := ag.Run(ctx, sess, sink); err != nil {
		return nil, fmt.Errorf("agent loop: %w", err)
	}

	return &acp.PromptResponse{StopReason: acp.StopEndTurn}, nil
}

func (srv *Server) LoadSession(ctx context.Context, req *acp.LoadSessionRequest, sender acp.SessionUpdateSender) (*acp.LoadSessionResponse, error) {
	sess, err := session.Load(string(req.SessionID))
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if err := replaySession(sess, srv.rt.ContextWindow(sess.AgentName), sender); err != nil {
		return nil, fmt.Errorf("replay session: %w", err)
	}
	if err := advertiseCommands(sender); err != nil {
		return nil, err
	}
	return &acp.LoadSessionResponse{}, nil
}

// commands are the slash commands the runtime handles itself. They arrive as
// ordinary prompt text, so any client can use them by typing.
var commands = []acp.AvailableCommand{{
	Name:        "compact",
	Description: "Summarize the earlier conversation to free up context",
	Input:       &acp.AvailableCommandInput{Hint: "optional instructions for the summary"},
}}

func advertiseCommands(sender acp.SessionUpdateSender) error {
	return sender.Send(acp.SessionUpdate{
		SessionUpdate:     "available_commands_update",
		AvailableCommands: &acp.AvailableCommandsUpdate{AvailableCommands: commands},
	})
}

func (srv *Server) ResumeSession(ctx context.Context, req *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
	if _, err := session.Load(string(req.SessionID)); err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	return &acp.ResumeSessionResponse{}, nil
}

func (srv *Server) CloseSession(ctx context.Context, req *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
	return &acp.CloseSessionResponse{}, nil
}

func (srv *Server) DeleteSession(ctx context.Context, req *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error) {
	if err := session.Delete(string(req.SessionID)); err != nil {
		return nil, fmt.Errorf("delete session: %w", err)
	}
	return &acp.DeleteSessionResponse{}, nil
}

func (srv *Server) ListSessions(ctx context.Context, req *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	sessions, err := session.List()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	infos := make([]acp.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		var title *string
		if value := s.Title; value != "" {
			title = &value
		}
		var updatedAt *string
		if !s.UpdatedAt.IsZero() {
			value := s.UpdatedAt.Format(time.RFC3339)
			updatedAt = &value
		}
		infos = append(infos, acp.SessionInfo{
			SessionID: acp.SessionID(s.ID),
			Title:     title,
			UpdatedAt: updatedAt,
			Meta:      acp.Meta{acp.MiyaProfileMetaKey: s.AgentName},
		})
	}
	return &acp.ListSessionsResponse{Sessions: infos}, nil
}

func (srv *Server) SetSessionMode(ctx context.Context, req *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
	return &acp.SetSessionModeResponse{}, nil
}

func (srv *Server) SetSessionConfigOption(ctx context.Context, req *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	return &acp.SetSessionConfigOptionResponse{}, nil
}

func (srv *Server) Logout(ctx context.Context, req *acp.LogoutRequest) (*acp.LogoutResponse, error) {
	return &acp.LogoutResponse{}, nil
}
