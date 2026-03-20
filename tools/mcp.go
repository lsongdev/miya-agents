package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/lsongdev/openai-go/mcp"
	"github.com/lsongdev/openai-go/openai"
)

// mcpServer represents a registered MCP server.
type McpServer struct {
	config *mcp.McpServerConfig
	client *mcp.Client
	Tools  []openai.Tool
}

func NewMcpServer(config *mcp.McpServerConfig) (server *McpServer, err error) {
	client, err := mcp.NewStdioClient(config)
	if err != nil {
		err = fmt.Errorf("failed to create client: %w", err)
		return
	}
	// Initialize the MCP client
	if _, err = client.Initialize("openai-go", "1.0.0"); err != nil {
		client.Close()
		err = fmt.Errorf("failed to initialize: %w", err)
		return
	}
	// List available tools
	mcpTools, err := client.ListTools()
	if err != nil {
		client.Close()
		err = fmt.Errorf("failed to list tools: %w", err)
		return
	}
	var tools []openai.Tool
	for _, mcpTool := range mcpTools {
		tool := NewMcpTool(client, &mcpTool)
		tools = append(tools, tool)
	}
	server = &McpServer{
		config: config,
		client: client,
		Tools:  tools,
	}
	return
}

func (s *McpServer) Close() {
	s.client.Close()
	s.Tools = []openai.Tool{}
}

// McpTool is a wrapper that implements the openai.Tool interface.
// It delegates to the McpManager for actual tool execution.
type McpTool struct {
	rpc  *mcp.Client
	tool *mcp.Tool
}

// NewMcpTool creates a tool wrapper for a specific tool.
// Deprecated: Use McpManager directly for multi-server support.
func NewMcpTool(rpc *mcp.Client, tool *mcp.Tool) openai.Tool {
	return &McpTool{
		rpc:  rpc,
		tool: tool,
	}
}

// Def implements [openai.Tool].
func (m *McpTool) Def() openai.ToolDef {
	return m.tool.Def()
}

// Run implements [openai.Tool].
func (m *McpTool) Run(ctx context.Context, args string) string {
	var arguments map[string]any
	if err := json.Unmarshal([]byte(args), &arguments); err != nil {
		return fmt.Sprintf("Error: failed to parse arguments: %v", err)
	}
	result, err := m.rpc.CallTool(m.tool.Name, arguments)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if result.IsError {
		return formatToolResultError(result)
	}
	return formatToolResult(result)
}

func formatToolResult(result *mcp.ToolResult) string {
	var output string
	for _, content := range result.Content {
		switch content.Type {
		case "text":
			output += content.Text
		case "image":
			output += fmt.Sprintf("[Image: %s]", content.MimeType)
		case "resource":
			if data, ok := content.Data.(string); ok {
				output += data
			} else {
				dataBytes, _ := json.Marshal(content.Data)
				output += string(dataBytes)
			}
		default:
			if content.Text != "" {
				output += content.Text
			} else {
				dataBytes, _ := json.Marshal(content.Data)
				output += string(dataBytes)
			}
		}
	}
	return output
}

func formatToolResultError(result *mcp.ToolResult) string {
	var output string
	for _, content := range result.Content {
		if content.Text != "" {
			output += content.Text
		} else {
			dataBytes, _ := json.Marshal(content.Data)
			output += string(dataBytes)
		}
	}
	if output == "" {
		return "Error: tool execution failed with no error message"
	}
	return fmt.Sprintf("Error: %s", output)
}

type McpManager struct {
	Servers  map[string]McpServer
	Tools    map[string]openai.Tool
	ToolDefs []openai.ToolDef
}

func NewMcpManager(config map[string]*mcp.McpServerConfig) (manager *McpManager) {
	mcpServers := map[string]McpServer{}
	for name, mcpServerConfig := range config {
		mcpServer, err := NewMcpServer(mcpServerConfig)
		if err != nil {
			log.Println(err)
			continue
		}
		mcpServers[name] = *mcpServer
	}
	manager = &McpManager{
		Servers:  mcpServers,
		Tools:    make(map[string]openai.Tool),
		ToolDefs: []openai.ToolDef{},
	}
	for serverName, server := range manager.Servers {
		for _, tool := range server.Tools {
			d := tool.Def()
			toolName := fmt.Sprintf("mcp_%s_%s", serverName, d.Function.Name)
			d.Function.Name = toolName
			manager.Tools[toolName] = tool
			manager.ToolDefs = append(manager.ToolDefs, d)
		}
	}
	return
}

func (m *McpManager) Close() {
	for _, server := range m.Servers {
		server.Close()
	}
}
