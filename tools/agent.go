package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/lsongdev/miya-agents/openai"
)

type DelegateFunc func(context.Context, string, string) (string, error)

type DelegateTool struct {
	Delegate DelegateFunc
	Agents   map[string]string
}

func NewDelegateTool(delegate DelegateFunc, agents map[string]string) *DelegateTool {
	return &DelegateTool{Delegate: delegate, Agents: agents}
}

func (t *DelegateTool) Def() openai.ToolDef {
	names := make([]string, 0, len(t.Agents))
	for name := range t.Agents {
		names = append(names, name)
	}
	sort.Strings(names)

	description := "Delegate a self-contained task to another agent. The delegated agent has an independent context and returns only its result."
	if len(names) > 0 {
		var agents strings.Builder
		agents.WriteString(description + " Available agents:")
		for _, name := range names {
			agents.WriteString("\n- " + name)
			if detail := strings.TrimSpace(t.Agents[name]); detail != "" {
				agents.WriteString(": " + detail)
			}
		}
		description = agents.String()
	}

	return openai.ToolDef{
		Type: "function",
		Function: openai.FunctionDef{
			Name:        "delegate",
			Description: description,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent": map[string]any{
						"type": "string",
						"enum": names,
						"description": "Agent profile to run the task.",
					},
					"task": map[string]any{
						"type":        "string",
						"description": "Complete, self-contained task for the delegated agent.",
					},
				},
				"required": []string{"agent", "task"},
			},
		},
	}
}

func (t *DelegateTool) Run(ctx context.Context, args string) string {
	var input struct {
		Agent string `json:"agent"`
		Task  string `json:"task"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return fmt.Sprintf("Error: failed to parse arguments: %v", err)
	}
	if strings.TrimSpace(input.Agent) == "" || strings.TrimSpace(input.Task) == "" {
		return "Error: agent and task are required"
	}
	if t.Delegate == nil {
		return "Error: delegation is not configured"
	}

	result, err := t.Delegate(ctx, input.Agent, input.Task)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return result
}
