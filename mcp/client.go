// Package mcp provides MCP (Model Context Protocol) client functionality.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/lsongdev/jsonrpc-go/jsonrpc"
	"github.com/lsongdev/jsonrpc-go/jsonrpc/common"
	"github.com/lsongdev/jsonrpc-go/jsonrpc/transports"
	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/process"
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
	cmd.Env = environmentWithOverrides(os.Environ(), server.Env)
	process.ConfigureCommand(cmd)
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

func environmentWithOverrides(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return nil
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	overridden := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		overridden[key] = struct{}{}
	}
	environment := make([]string, 0, len(base)+len(keys))
	for _, entry := range base {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, exists := overridden[key]; exists {
				continue
			}
		}
		environment = append(environment, entry)
	}
	for _, key := range keys {
		environment = append(environment, key+"="+overrides[key])
	}
	return environment
}

func NewHTTPClient(server *McpServerConfig) (*Client, error) {
	if server.URL == "" {
		return nil, fmt.Errorf("mcp http: url is required")
	}
	transport := transports.NewStreamableHTTPTransport(server.URL, &transports.StreamableHTTPOptions{Headers: server.Headers})
	return NewClient(transport), nil
}

func NewSSEClient(server *McpServerConfig) (*Client, error) {
	if server.URL == "" {
		return nil, fmt.Errorf("mcp sse: url is required")
	}
	transport, err := transports.NewSSETransport(context.Background(), server.URL, transports.SSEOptions{Headers: server.Headers})
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
	case "streamable", "streamable-http", "streamable_http", "streamablehttp":
		return NewHTTPClient(server)
	case "sse":
		return NewSSEClient(server)
	default:
		return nil, fmt.Errorf("unsupported mcp transport type: %s", server.Type)
	}
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
	if err := c.rpc.Call(context.Background(), "initialize", params, &result); err != nil {
		return nil, err
	}

	// Send initialized notification
	if err := c.rpc.Notify(context.Background(), "notifications/initialized", nil); err != nil {
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

	return c.rpc.Call(context.Background(), method, params, result)
}

// Notify sends a JSON-RPC notification (no response expected).
func (c *Client) Notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("client is closed")
	}

	return c.rpc.Notify(context.Background(), method, params)
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
