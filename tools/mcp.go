package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/lsongdev/miya-agents/agent"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/mcp"
)

// McpServer is a connected MCP server and the tools it offers.
type McpServer struct {
	config *mcp.McpServerConfig
	client *mcp.Client
	// Tools are named as the server names them.
	Tools []*McpTool
}

func NewMcpServer(config *mcp.McpServerConfig) (*McpServer, error) {
	client, err := mcp.NewConfiguredClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	if _, err = client.Initialize("miya", "1.0.0"); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}
	mcpTools, err := client.ListTools()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	server := &McpServer{config: config, client: client}
	for _, tool := range mcpTools {
		server.Tools = append(server.Tools, &McpTool{rpc: client, tool: tool, name: tool.Name})
	}
	return server, nil
}

func (s *McpServer) Close() {
	s.client.Close()
	s.Tools = nil
}

// McpTool is one tool of an MCP server, as an agent.Tool. The model knows it
// by name, which may differ from the name the server uses.
type McpTool struct {
	rpc  *mcp.Client
	tool mcp.Tool
	name string
}

// Def implements [agent.Tool].
func (m *McpTool) Def() llm.ToolDef {
	var params map[string]any
	if err := json.Unmarshal(m.tool.InputSchema, &params); err != nil {
		params = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	return llm.ToolDef{
		Name:        m.name,
		Description: m.tool.Description,
		Parameters:  params,
	}
}

// Run implements [agent.Tool].
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
	Tools    map[string]agent.Tool
	ToolDefs []llm.ToolDef
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
		Tools:    make(map[string]agent.Tool),
		ToolDefs: []llm.ToolDef{},
	}
	for serverName, server := range manager.Servers {
		for _, tool := range server.Tools {
			// Prefix the server name so tools from different servers
			// cannot collide.
			named := *tool
			named.name = fmt.Sprintf("mcp_%s_%s", serverName, tool.tool.Name)
			manager.Tools[named.name] = &named
			manager.ToolDefs = append(manager.ToolDefs, named.Def())
		}
	}
	return
}

func (m *McpManager) Close() {
	for _, server := range m.Servers {
		server.Close()
	}
}
