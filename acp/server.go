package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// Server is a JSON-RPC stdio server for the ACP protocol.
type Server struct {
	handler Handler
	scanner *bufio.Scanner
	encoder *json.Encoder
	mu      sync.Mutex

	cancelFuncs map[SessionID]context.CancelFunc
	cancelMu    sync.Mutex
}

// NewServer creates a new ACP stdio server writing to os.Stdout.
func NewServer(handler Handler) *Server {
	return NewServerWithWriter(handler, os.Stdout)
}

// NewServerWithWriter creates a new ACP stdio server writing to the given writer.
func NewServerWithWriter(handler Handler, w io.Writer) *Server {
	return &Server{
		handler:     handler,
		encoder:     json.NewEncoder(w),
		cancelFuncs: make(map[SessionID]context.CancelFunc),
	}
}

// Serve starts the ACP server, reading from stdin and writing to stdout.
func (s *Server) Serve() error {
	return s.ServeFromReader(os.Stdin)
}

// ServeFromReader starts the server reading from the given reader.
func (s *Server) ServeFromReader(reader io.Reader) error {
	s.scanner = bufio.NewScanner(reader)
	s.scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		data := make([]byte, len(line))
		copy(data, line)

		s.dispatch(data)
	}

	return s.scanner.Err()
}

func (s *Server) dispatch(data []byte) {
	var raw struct {
		ID     any          `json:"id"`
		Method string       `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		s.writeError(nil, ErrParse, "Parse error")
		return
	}

	if raw.Method == "" {
		s.writeError(nil, ErrInvalidRequest, "Method is required")
		return
	}

	isNotification := raw.ID == nil
	rawID := raw.ID

	if isNotification {
		s.handleNotification(raw.Method, raw.Params)
	} else {
		go s.handleRequest(rawID, raw.Method, raw.Params)
	}
}

func (s *Server) handleRequest(id any, method string, params json.RawMessage) {
	ctx := context.Background()

	switch method {
	case "initialize":
		var req InitializeRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid initialize params")
			return
		}
		resp, err := s.handler.Initialize(ctx, &req)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "authenticate":
		var req AuthenticateRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid authenticate params")
			return
		}
		resp, err := s.handler.Authenticate(ctx, &req)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "session/new":
		var req NewSessionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid session/new params")
			return
		}
		// Use a placeholder sender; session ID is not yet known.
		resp, err := s.handler.NewSession(ctx, &req, s.newSender(""))
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "session/prompt":
		var req PromptRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid session/prompt params")
			return
		}
		ctx, cancel := context.WithCancel(ctx)
		s.setCancel(req.SessionID, cancel)
		defer s.clearCancel(req.SessionID)

		sender := s.newSender(req.SessionID)
		resp, err := s.handler.Prompt(ctx, &req, sender)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "session/load":
		var req LoadSessionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid session/load params")
			return
		}
		sender := s.newSender(req.SessionID)
		resp, err := s.handler.LoadSession(ctx, &req, sender)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "session/resume":
		var req ResumeSessionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid session/resume params")
			return
		}
		resp, err := s.handler.ResumeSession(ctx, &req)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "session/close":
		var req CloseSessionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid session/close params")
			return
		}
		resp, err := s.handler.CloseSession(ctx, &req)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "session/delete":
		var req DeleteSessionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid session/delete params")
			return
		}
		resp, err := s.handler.DeleteSession(ctx, &req)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "session/list":
		var req ListSessionsRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid session/list params")
			return
		}
		resp, err := s.handler.ListSessions(ctx, &req)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "session/set_mode":
		var req SetSessionModeRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid session/set_mode params")
			return
		}
		resp, err := s.handler.SetSessionMode(ctx, &req)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "session/set_config_option":
		var req SetSessionConfigOptionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid session/set_config_option params")
			return
		}
		resp, err := s.handler.SetSessionConfigOption(ctx, &req)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	case "logout":
		var req LogoutRequest
		if err := json.Unmarshal(params, &req); err != nil {
			s.writeError(id, ErrInvalidParams, "Invalid logout params")
			return
		}
		resp, err := s.handler.Logout(ctx, &req)
		if err != nil {
			s.writeError(id, ErrInternal, err.Error())
			return
		}
		s.writeResult(id, resp)

	default:
		// Custom methods starting with "_" are treated as extensions
		if len(method) > 0 && method[0] == '_' {
			s.writeResult(id, map[string]any{"_": "ok"})
			return
		}
		s.writeError(id, ErrMethodNotFound, fmt.Sprintf("Method not found: %s", method))
	}
}

func (s *Server) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "session/cancel":
		var req CancelNotification
		if err := json.Unmarshal(params, &req); err != nil {
			return
		}
		s.cancelMu.Lock()
		cancel, ok := s.cancelFuncs[req.SessionID]
		s.cancelMu.Unlock()
		if ok {
			cancel()
		}
	case "notifications/initialized":
		// MCP-style initialized notification, ignored
	default:
		// Ignore unknown notifications
	}
}

// newSender creates a SessionUpdateSender for the given session.
func (s *Server) newSender(sessionID SessionID) SessionUpdateSender {
	return &notificationSender{
		sessionID: sessionID,
		encoder:   s.encoder,
		mu:        &s.mu,
	}
}

func (s *Server) setCancel(sessionID SessionID, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.cancelFuncs[sessionID] = cancel
}

func (s *Server) clearCancel(sessionID SessionID) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	delete(s.cancelFuncs, sessionID)
}

func (s *Server) writeResult(id any, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.encoder.Encode(resp)
}

func (s *Server) writeError(id any, code int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonrpcError{
			Code:    code,
			Message: message,
		},
	}
	s.encoder.Encode(resp)
}

// notificationSender implements SessionUpdateSender.
type notificationSender struct {
	sessionID SessionID
	encoder   *json.Encoder
	mu        *sync.Mutex
}

func (n *notificationSender) Send(update SessionUpdate) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	notif := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: SessionNotification{
			SessionID: n.sessionID,
			Update:    update,
		},
	}
	return n.encoder.Encode(notif)
}

// jsonrpcResponse is a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      any          `json:"id"`
	Result  any          `json:"result,omitempty"`
	Error   *jsonrpcError `json:"error,omitempty"`
}

// jsonrpcError is a JSON-RPC 2.0 error object.
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// jsonrpcNotification is a JSON-RPC 2.0 notification.
type jsonrpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
