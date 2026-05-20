package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lsongdev/miya-agents/openai"
)

// AgentRunner defines the interface for running an agent.
// This avoids circular dependencies between the agent and tools packages.
type AgentRunner interface {
	RunAgent(ctx context.Context, name, prompt string) (string, error)
}

// SubagentTool is a tool that allows an agent to invoke another agent.
type SubagentTool struct {
	Runner AgentRunner
}

// NewSubagentTool creates a new SubagentTool.
func NewSubagentTool(runner AgentRunner) *SubagentTool {
	return &SubagentTool{
		Runner: runner,
	}
}

// Def implements [openai.Tool].
func (t *SubagentTool) Def() openai.ToolDef {
	return openai.ToolDef{
		Type: "function",
		Function: openai.FunctionDef{
			Name:        "invoke_agent",
			Description: "Invoke a specialized sub-agent to perform a specific task or investigation. Use this to delegate complex or repetitive work.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Name of the sub-agent to invoke.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "The COMPLETE query to send the subagent. MUST be comprehensive and detailed.",
					},
				},
				"required": []string{"agent_name", "prompt"},
			},
		},
	}
}

// Run implements [openai.Tool].
func (t *SubagentTool) Run(ctx context.Context, args string) string {
	var input struct {
		AgentName string `json:"agent_name"`
		Prompt    string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return fmt.Sprintf("Error: failed to parse arguments: %v", err)
	}

	result, err := t.Runner.RunAgent(ctx, input.AgentName, input.Prompt)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	return result
}
