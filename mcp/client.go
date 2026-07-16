// Package mcp provides MCP (Model Context Protocol) client functionality.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"

	"github.com/lsongdev/jsonrpc-go/jsonrpc"
	"github.com/lsongdev/jsonrpc-go/jsonrpc/common"
	"github.com/lsongdev/jsonrpc-go/jsonrpc/transports"
	"github.com/lsongdev/miya-agents/openai"
)

// InitializeParams represents the parameters for the initialize method.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
	Meta            map[string]any `json:"_meta,omitempty"`
}

// ClientInfo represents client information.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult represents the result of the initialize method.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    Capabilities   `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
	Meta            map[string]any `json:"_meta,omitempty"`
}

// Capabilities represents server capabilities.
type Capabilities struct {
	Tools   *ToolsCapability   `json:"tools,omitempty"`
	Prompts *PromptsCapability `json:"prompts,omitempty"`
}

// ToolsCapability represents tools capability.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability represents prompts capability.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerInfo represents server information.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool represents an MCP tool.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func (t *Tool) Def() openai.ToolDef {
	var params map[string]any
	if err := json.Unmarshal(t.InputSchema, &params); err != nil {
		params = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	return openai.ToolDef{
		Type: "function",
		Function: openai.FunctionDef{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		},
	}
}

// ToolResult represents the result of calling a tool.
type ToolResult struct {
	Content []Content      `json:"content"`
	IsError bool           `json:"isError,omitempty"`
	Meta    map[string]any `json:"_meta,omitempty"`
}

// Content represents tool result content.
type Content struct {
	Type        string       `json:"type"` // "text", "image", "resource"
	Text        string       `json:"text,omitempty"`
	Data        any          `json:"data,omitempty"`
	MimeType    string       `json:"mimeType,omitempty"`
	Annotations *ContentMeta `json:"annotations,omitempty"`
}

// ContentMeta represents content metadata.
type ContentMeta struct {
	Audience []string `json:"audience,omitempty"`
	Priority float64  `json:"priority,omitempty"`
}

// Client represents an MCP client that communicates with an MCP server.
type Client struct {
	rpc    *jsonrpc.JSONRPCClient
	mu     sync.Mutex
	closed bool
}

// NewClient creates a new MCP client with the specified transport.
func NewClient(transport common.Transport) *Client {
	return &Client{
		rpc: jsonrpc.NewJSONRPCClient(transport),
	}
}

type McpServerConfig struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// NewStdioClient creates a new MCP client that communicates via stdio.
// For example: NewStdioClient("npx", "-y", "12306-mcp")
func NewStdioClient(server *McpServerConfig) (*Client, error) {
	cmd := exec.Command(server.Command, server.Args...)
	configureCommand(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	// Capture stderr for debugging
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	transport := transports.NewStdioTransport(stdin, stdout)
	return NewClient(transport), nil
}

func NewHTTPClient(server *McpServerConfig) (*Client, error) {
	if server.URL == "" {
		return nil, fmt.Errorf("mcp http: url is required")
	}
	transport := transports.NewHTTPTransport(server.URL, &transports.HTTPOptions{
		Headers: server.Headers,
	})
	return NewClient(transport), nil
}

func NewSSEClient(server *McpServerConfig) (*Client, error) {
	if server.URL == "" {
		return nil, fmt.Errorf("mcp sse: url is required")
	}
	transport, err := NewSSETransport(server.URL, server.Headers)
	if err != nil {
		return nil, err
	}
	return NewClient(transport), nil
}

func NewConfiguredClient(server *McpServerConfig) (*Client, error) {
	switch strings.ToLower(server.Type) {
	case "", "stdio":
		if server.URL != "" && server.Command == "" {
			return NewSSEClient(server)
		}
		return NewStdioClient(server)
	case "http":
		return NewHTTPClient(server)
	case "sse":
		return NewSSEClient(server)
	default:
		return nil, fmt.Errorf("unsupported mcp transport type: %s", server.Type)
	}
}

type SSETransport struct {
	sseURL      string
	postURL     string
	headers     map[string]string
	client      *http.Client
	ctx         context.Context
	cancel      context.CancelFunc
	resp        *http.Response
	events      chan []byte
	sendMu      sync.Mutex
	initialized bool
}

func NewSSETransport(sseURL string, headers map[string]string) (*SSETransport, error) {
	ctx, cancel := context.WithCancel(context.Background())
	t := &SSETransport{
		sseURL:  sseURL,
		headers: headers,
		client:  http.DefaultClient,
		ctx:     ctx,
		cancel:  cancel,
		events:  make(chan []byte, 32),
	}
	if err := t.connect(); err != nil {
		cancel()
		return nil, err
	}
	return t, nil
}

func (t *SSETransport) connect() error {
	req, err := http.NewRequestWithContext(t.ctx, http.MethodGet, t.sseURL, nil)
	if err != nil {
		return fmt.Errorf("mcp sse: create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp sse: connect: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("mcp sse: unexpected status %s: %s", resp.Status, string(body))
	}
	t.resp = resp

	endpoint, err := readSSEEndpoint(resp.Body)
	if err != nil {
		resp.Body.Close()
		return err
	}
	t.postURL, err = resolveSSEEndpoint(t.sseURL, endpoint)
	if err != nil {
		resp.Body.Close()
		return err
	}

	go t.readEvents(resp.Body)
	return nil
}

func (t *SSETransport) Send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("mcp sse: marshal message: %w", err)
	}

	t.sendMu.Lock()
	defer t.sendMu.Unlock()

	req, err := http.NewRequestWithContext(t.ctx, http.MethodPost, t.postURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("mcp sse: create post: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp sse: post message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mcp sse: post status %s: %s", resp.Status, string(body))
	}
	return nil
}

func (t *SSETransport) Recv() ([]byte, error) {
	select {
	case data, ok := <-t.events:
		if !ok {
			return nil, fmt.Errorf("mcp sse: stream closed")
		}
		return data, nil
	case <-t.ctx.Done():
		return nil, t.ctx.Err()
	}
}

func (t *SSETransport) Close() error {
	t.cancel()
	if t.resp != nil {
		return t.resp.Body.Close()
	}
	return nil
}

func (t *SSETransport) readEvents(r io.Reader) {
	defer close(t.events)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var event string
	var data []string
	flush := func() bool {
		if len(data) == 0 {
			event = ""
			return true
		}
		if event == "" || event == "message" {
			select {
			case t.events <- []byte(strings.Join(data, "\n")):
			case <-t.ctx.Done():
				return false
			}
		}
		event = ""
		data = nil
		return true
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !flush() {
				return
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func readSSEEndpoint(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var event string
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if event == "endpoint" && len(data) > 0 {
				return strings.Join(data, "\n"), nil
			}
			event = ""
			data = nil
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("mcp sse: read endpoint: %w", err)
	}
	return "", fmt.Errorf("mcp sse: endpoint event not received")
}

func resolveSSEEndpoint(baseURL, endpoint string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("mcp sse: parse base url: %w", err)
	}
	ref, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("mcp sse: parse endpoint: %w", err)
	}
	return base.ResolveReference(ref).String(), nil
}

// Initialize initializes the MCP connection.
func (c *Client) Initialize(clientName, clientVersion string) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
		ClientInfo: ClientInfo{
			Name:    clientName,
			Version: clientVersion,
		},
	}

	var result InitializeResult
	if err := c.rpc.Call("initialize", params, &result); err != nil {
		return nil, err
	}

	// Send initialized notification
	if err := c.rpc.Notify("notifications/initialized", nil); err != nil {
		return nil, err
	}

	return &result, nil
}

// Call sends a JSON-RPC request and waits for the response.
func (c *Client) Call(method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("client is closed")
	}

	return c.rpc.Call(method, params, result)
}

// Notify sends a JSON-RPC notification (no response expected).
func (c *Client) Notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("client is closed")
	}

	return c.rpc.Notify(method, params)
}

// ListTools lists available tools from the MCP server.
func (c *Client) ListTools() ([]Tool, error) {
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.Call("tools/list", nil, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool calls a tool on the MCP server.
func (c *Client) CallTool(name string, arguments map[string]any) (*ToolResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": arguments,
	}

	var result ToolResult
	if err := c.Call("tools/call", params, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Close closes the MCP client.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	return c.rpc.Close()
}
