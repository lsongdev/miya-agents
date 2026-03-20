package main

import (
	"context"
	"log"
	"os"

	"github.com/lsongdev/openai-go/mcp"
	"github.com/lsongdev/openai-go/openai"
	"github.com/lsongdev/openai-go/tools"
)

func main() {
	// Create MCP manager with multiple servers
	mcpServerConfigs := map[string]*mcp.McpServerConfig{
		"filesystem": {
			Command: "npx",
			Args: []string{
				"-y",
				"@modelcontextprotocol/server-filesystem",
				"/Users/Lsong/Projects",
			},
		},
		"time": {
			Command: "uvx",
			Args: []string{
				"mcp-server-time",
				"--local-timezone=America/New_York",
			},
		},
	}

	manager := tools.NewMcpManager(mcpServerConfigs)

	// Use with OpenAI client
	client, err := openai.NewClient(&openai.Configuration{
		API:    os.Getenv("OPENAI_API"),
		APIKey: os.Getenv("OPENAI_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}

	// List available models
	models, err := client.Models()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Available models: %d\n", len(models))
	for _, model := range models {
		log.Printf("  - Model: %s\n", model.ID)
	}

	request := &openai.ChatCompletionRequest{
		Model: openai.DeepSeekChat,
		Messages: []openai.ChatCompletionMessage{
			openai.UserMessage("What time is it? Also list files in my projects directory."),
		},
		Tools: manager.ToolDefs,
		// Stream: true,
	}
	resp, err := client.CreateChatCompletion(request)
	if err != nil {
		log.Fatal(err)
	}
	message := resp.GetMessage()
	log.Println("response:", message.Content)

	ctx := context.Background()
	for _, tc := range message.ToolCalls {
		log.Println(tc.Function.Name, tc.Function.Arguments)
		tool := manager.Tools[tc.Function.Name]
		result := tool.Run(ctx, tc.Function.Arguments)
		log.Println(result)
	}
}
