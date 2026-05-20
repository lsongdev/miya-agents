package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// NotificationHandler is called for each JSON-RPC notification received.
type NotificationHandler func(method string, params json.RawMessage)

// Client is an ACP client that communicates with an ACP agent over stdio.
type Client struct {
	stdin           io.WriteCloser
	stdout          *bufio.Scanner
	mu              sync.Mutex
	id              atomic.Int64
	onNotification  NotificationHandler
}

// OnNotification registers a handler for incoming notifications.
func (c *Client) OnNotification(handler NotificationHandler) {
	c.onNotification = handler
}

// DialStdio launches an ACP agent subprocess and returns a Client connected to it.
func DialStdio(command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}
	cmd.Stderr = nil // stderr is for agent logs

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start: %w", err)
	}

	return NewClient(stdin, stdout), nil
}

// NewClient creates an ACP client from the given read/writer.
func NewClient(stdin io.WriteCloser, stdout io.Reader) *Client {
	return &Client{
		stdin:  stdin,
		stdout: bufio.NewScanner(stdout),
	}
}

func (c *Client) sendRecv(method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.id.Add(1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("acp: marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("acp: write request: %w", err)
	}

	// Read response lines until we find one matching our ID
	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		if len(line) == 0 {
			continue
		}

		var raw struct {
			ID     any              `json:"id"`
			Method string           `json:"method"`
			Result json.RawMessage  `json:"result,omitempty"`
			Error  *jsonrpcError    `json:"error,omitempty"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}

		// Handle notifications (no ID)
		if raw.ID == nil {
			if c.onNotification != nil {
				// Re-extract params for notification
				var notifRaw struct {
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if json.Unmarshal(line, &notifRaw) == nil {
					c.onNotification(notifRaw.Method, notifRaw.Params)
				}
			}
			continue
		}

		// Convert id to match
		var respID int64
		switch v := raw.ID.(type) {
		case float64:
			respID = int64(v)
		case int64:
			respID = v
		default:
			continue
		}

		if respID != id {
			continue
		}

		if raw.Error != nil {
			return fmt.Errorf("acp: %s", raw.Error.Message)
		}

		if result != nil && raw.Result != nil {
			if err := json.Unmarshal(raw.Result, result); err != nil {
				return fmt.Errorf("acp: unmarshal result: %w", err)
			}
		}
		return nil
	}

	return fmt.Errorf("acp: connection closed")
}

// SendNotification sends a JSON-RPC notification (no response expected).
func (c *Client) SendNotification(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	notif := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("acp: marshal notification: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("acp: write notification: %w", err)
	}
	return nil
}

// Call sends a JSON-RPC request and waits for the response.
func (c *Client) Call(method string, params, result any) error {
	return c.sendRecv(method, params, result)
}

// Initialize sends an initialize request.
func (c *Client) Initialize(req *InitializeRequest) (*InitializeResponse, error) {
	var resp InitializeResponse
	if err := c.sendRecv("initialize", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Authenticate sends an authenticate request.
func (c *Client) Authenticate(req *AuthenticateRequest) (*AuthenticateResponse, error) {
	var resp AuthenticateResponse
	if err := c.sendRecv("authenticate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// NewSession creates a new session.
func (c *Client) NewSession(req *NewSessionRequest) (*NewSessionResponse, error) {
	var resp NewSessionResponse
	if err := c.sendRecv("session/new", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Prompt sends a prompt request and returns the response.
func (c *Client) Prompt(req *PromptRequest) (*PromptResponse, error) {
	var resp PromptResponse
	if err := c.sendRecv("session/prompt", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LoadSession loads an existing session.
func (c *Client) LoadSession(req *LoadSessionRequest) (*LoadSessionResponse, error) {
	var resp LoadSessionResponse
	if err := c.sendRecv("session/load", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ResumeSession resumes an existing session.
func (c *Client) ResumeSession(req *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	var resp ResumeSessionResponse
	if err := c.sendRecv("session/resume", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CloseSession closes a session.
func (c *Client) CloseSession(req *CloseSessionRequest) (*CloseSessionResponse, error) {
	var resp CloseSessionResponse
	if err := c.sendRecv("session/close", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteSession deletes a session.
func (c *Client) DeleteSession(req *DeleteSessionRequest) (*DeleteSessionResponse, error) {
	var resp DeleteSessionResponse
	if err := c.sendRecv("session/delete", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListSessions lists sessions.
func (c *Client) ListSessions(req *ListSessionsRequest) (*ListSessionsResponse, error) {
	var resp ListSessionsResponse
	if err := c.sendRecv("session/list", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetSessionMode sets the session mode.
func (c *Client) SetSessionMode(req *SetSessionModeRequest) (*SetSessionModeResponse, error) {
	var resp SetSessionModeResponse
	if err := c.sendRecv("session/set_mode", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetSessionConfigOption sets a session config option.
func (c *Client) SetSessionConfigOption(req *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error) {
	var resp SetSessionConfigOptionResponse
	if err := c.sendRecv("session/set_config_option", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Logout sends a logout request.
func (c *Client) Logout(req *LogoutRequest) (*LogoutResponse, error) {
	var resp LogoutResponse
	if err := c.sendRecv("logout", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelSession sends a session/cancel notification.
func (c *Client) CancelSession(sessionID SessionID) error {
	req := CancelNotification{
		SessionID: sessionID,
	}
	return c.SendNotification("session/cancel", req)
}

// ReceiveSessionUpdates begins reading session/update notifications from the
// agent's stdout. It returns a channel that receives SessionUpdate values.
// The channel is closed when the reader encounters an error or EOF.
func (c *Client) ReceiveSessionUpdates(ctx any) <-chan SessionUpdate {
	ch := make(chan SessionUpdate, 64)
	go func() {
		defer close(ch)
		for c.stdout.Scan() {
			line := c.stdout.Bytes()
			if len(line) == 0 {
				continue
			}
			var raw struct {
				Method string              `json:"method"`
				Params SessionNotification `json:"params"`
			}
			if err := json.Unmarshal(line, &raw); err != nil {
				continue
			}
			if raw.Method != "session/update" {
				continue
			}
			ch <- raw.Params.Update
		}
	}()
	return ch
}

// jsonrpcRequest is a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Close closes the client's stdin, signaling EOF to the agent.
func (c *Client) Close() error {
	if c.stdin != nil {
		return c.stdin.Close()
	}
	return nil
}
