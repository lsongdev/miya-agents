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
	stdoutCloser   io.Closer
	writeMu        sync.Mutex
	id             atomic.Int64
	onNotification NotificationHandler
	pendingMu      sync.Mutex
	pending        map[int64]chan clientResponse
	closed         chan struct{}
	closeOnce      sync.Once
}

type clientResponse struct {
	result json.RawMessage
	err    error
}

// OnNotification registers a handler for incoming notifications.
func (c *Client) OnNotification(handler NotificationHandler) {
	c.onNotification = handler
}

// DialStdio launches an ACP agent subprocess and returns a Client connected to it.
// Agent stderr is forwarded to the parent process's stderr for visibility.
func DialStdio(command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	configureCommand(cmd)
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
		stdin:   stdin,
		pending: make(map[int64]chan clientResponse),
		closed:  make(chan struct{}),
	}
	if closer, ok := stdout.(io.Closer); ok {
		client.stdoutCloser = closer
	}
	go client.readLoop(stdout)
	return client
}

func (c *Client) sendRecv(method string, params, result any) error {
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

	respCh := make(chan clientResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()
	defer c.removePending(id)

	c.writeMu.Lock()
	_, err = c.stdin.Write(data)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("acp: write request: %w", err)
	}

	select {
	case resp := <-respCh:
		if resp.err != nil {
			return resp.err
		}
		if result != nil && resp.result != nil {
			if err := json.Unmarshal(resp.result, result); err != nil {
				return fmt.Errorf("acp: unmarshal result: %w", err)
			}
		}
		return nil
	case <-c.closed:
		return fmt.Errorf("acp: connection closed")
	}
}

func (c *Client) readLoop(stdout io.Reader) {
	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			c.handleFrame(trimFrame(line))
		}
		if readErr != nil {
			c.closePending(fmt.Errorf("acp: connection closed"))
			return
		}
	}
}

func trimFrame(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line
}

func (c *Client) handleFrame(line []byte) {
	if len(line) == 0 {
		return
	}
	var raw struct {
		ID     any             `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  *jsonrpcError   `json:"error,omitempty"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return
	}
	if raw.ID == nil {
		if c.onNotification != nil {
			c.onNotification(raw.Method, raw.Params)
		}
		return
	}
	respID, ok := responseID(raw.ID)
	if !ok {
		return
	}
	resp := clientResponse{result: raw.Result}
	if raw.Error != nil {
		resp.err = fmt.Errorf("acp: %s", raw.Error.Message)
	}
	c.pendingMu.Lock()
	ch := c.pending[respID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- resp
	}
}

func responseID(id any) (int64, bool) {
	switch v := id.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func (c *Client) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *Client) closePending(err error) {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.pendingMu.Lock()
		defer c.pendingMu.Unlock()
		for id, ch := range c.pending {
			ch <- clientResponse{err: err}
			delete(c.pending, id)
		}
	})
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
	c.OnNotification(func(method string, params json.RawMessage) {
		if method != "session/update" {
			return
		}
		var n SessionNotification
		if json.Unmarshal(params, &n) == nil {
			select {
			case ch <- n.Update:
			default:
			}
		}
	})
	go func() {
		<-c.closed
		close(ch)
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
	c.closePending(fmt.Errorf("acp: connection closed"))
	return err
}
