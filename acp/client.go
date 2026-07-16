package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// NotificationHandler is called for each JSON-RPC notification received.
type NotificationHandler func(method string, params json.RawMessage)

// Client is an ACP client that communicates with an ACP agent over stdio.
type Client struct {
	stdin          io.WriteCloser
	stdout         *bufio.Reader
	stdoutCloser   io.Closer
	callMu         sync.Mutex
	writeMu        sync.Mutex
	id             atomic.Int64
	onNotification NotificationHandler
}

// OnNotification registers a handler for incoming notifications.
func (c *Client) OnNotification(handler NotificationHandler) {
	c.onNotification = handler
}

// DialStdio launches an ACP agent subprocess and returns a Client connected to it.
// Agent stderr is forwarded to the parent process's stderr for visibility.
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
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start: %w", err)
	}

	return NewClient(stdin, stdout), nil
}

// NewClient creates an ACP client from the given read/writer.
func NewClient(stdin io.WriteCloser, stdout io.Reader) *Client {
	client := &Client{
		stdin: stdin,
		// bufio.Reader.ReadBytes('\n') has no line-size limit, so replayed
		// session/update notifications with large tool outputs won't close
		// the connection the way bufio.Scanner's default 64KB cap did.
		stdout: bufio.NewReaderSize(stdout, 64*1024),
	}
	if closer, ok := stdout.(io.Closer); ok {
		client.stdoutCloser = closer
	}
	return client
}

func (c *Client) sendRecv(method string, params, result any) error {
	c.callMu.Lock()
	defer c.callMu.Unlock()

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

	c.writeMu.Lock()
	_, err = c.stdin.Write(data)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("acp: write request: %w", err)
	}

	// Read response lines until we find one matching our ID
	for {
		line, readErr := c.stdout.ReadBytes('\n')
		if len(line) == 0 {
			if readErr != nil {
				return fmt.Errorf("acp: connection closed")
			}
			continue
		}
		// Trim trailing newline; still process the frame even if EOF followed.
		if line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			if readErr != nil {
				return fmt.Errorf("acp: connection closed")
			}
			continue
		}

		var raw struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result,omitempty"`
			Error  *jsonrpcError   `json:"error,omitempty"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			if readErr != nil {
				return fmt.Errorf("acp: connection closed")
			}
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
			if readErr != nil {
				return fmt.Errorf("acp: connection closed")
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
			if readErr != nil {
				return fmt.Errorf("acp: connection closed")
			}
			continue
		}

		if respID != id {
			if readErr != nil {
				return fmt.Errorf("acp: connection closed")
			}
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
}

// SendNotification sends a JSON-RPC notification (no response expected).
func (c *Client) SendNotification(method string, params any) error {
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

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
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
		for {
			line, err := c.stdout.ReadBytes('\n')
			if len(line) > 0 {
				if line[len(line)-1] == '\n' {
					line = line[:len(line)-1]
				}
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
				if len(line) > 0 {
					var raw struct {
						Method string              `json:"method"`
						Params SessionNotification `json:"params"`
					}
					if json.Unmarshal(line, &raw) == nil && raw.Method == "session/update" {
						ch <- raw.Params.Update
					}
				}
			}
			if err != nil {
				return
			}
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
	var err error
	if c.stdin != nil {
		err = c.stdin.Close()
	}
	if c.stdoutCloser != nil {
		if closeErr := c.stdoutCloser.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}
