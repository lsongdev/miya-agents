package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/session"
)

const (
	defaultSummarizerAgent = "summarizer"
	compactionSummaryMark  = "Miya compacted conversation summary:"
)

type SessionToolset struct {
	Session *session.Session
	Runner  AgentRunner
}

func NewSessionTools(sess *session.Session, runner AgentRunner) []openai.Tool {
	toolset := &SessionToolset{Session: sess, Runner: runner}
	return []openai.Tool{
		&RenameSessionTool{toolset: toolset},
		&SummarizeSessionTool{toolset: toolset},
		&CompactContextTool{toolset: toolset},
	}
}

type RenameSessionTool struct {
	toolset *SessionToolset
}

func (t *RenameSessionTool) Def() openai.ToolDef {
	return openai.ToolDef{
		Type: "function",
		Function: openai.FunctionDef{
			Name:        "rename_session",
			Description: "Internal session metadata tool. Rename the current session with a concise title after the user goal is clear.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{
						"type":        "string",
						"description": "Short title, no markdown, no quotes, no newline.",
					},
				},
				"required": []string{"title"},
			},
		},
	}
}

func (t *RenameSessionTool) Run(ctx context.Context, args string) string {
	var input struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return fmt.Sprintf("Error: failed to parse arguments: %v", err)
	}
	title := sanitizeTitle(input.Title)
	if title == "" {
		return "Error: title cannot be empty"
	}
	t.toolset.Session.Title = title
	if err := t.toolset.Session.Save(); err != nil {
		return fmt.Sprintf("Error: failed to save session title: %v", err)
	}
	return "Session title updated."
}

type SummarizeSessionTool struct {
	toolset *SessionToolset
}

func (t *SummarizeSessionTool) Def() openai.ToolDef {
	return openai.ToolDef{
		Type: "function",
		Function: openai.FunctionDef{
			Name:        "summarize_session",
			Description: "Internal session metadata tool. Update the durable session summary using a summarizer agent.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Optional summarizer profile name. Defaults to 'summarizer'.",
					},
					"instructions": map[string]any{
						"type":        "string",
						"description": "Optional extra focus for the summary.",
					},
				},
			},
		},
	}
}

func (t *SummarizeSessionTool) Run(ctx context.Context, args string) string {
	var input struct {
		AgentName    string `json:"agent_name"`
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return fmt.Sprintf("Error: failed to parse arguments: %v", err)
	}
	summary, err := t.toolset.runSummarizer(ctx, input.AgentName, buildSummaryPrompt(t.toolset.Session, t.toolset.Session.Messages, input.Instructions))
	if err != nil {
		return fmt.Sprintf("Error: failed to summarize session: %v", err)
	}
	t.toolset.Session.Summary = strings.TrimSpace(summary)
	if err := t.toolset.Session.Save(); err != nil {
		return fmt.Sprintf("Error: failed to save session summary: %v", err)
	}
	return "Session summary updated."
}

type CompactContextTool struct {
	toolset *SessionToolset
}

func (t *CompactContextTool) Def() openai.ToolDef {
	return openai.ToolDef{
		Type: "function",
		Function: openai.FunctionDef{
			Name:        "compact_context",
			Description: "Internal session context tool. Summarize older model messages and replace them with a compact system summary. Does not modify replay events.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Optional summarizer profile name. Defaults to 'summarizer'.",
					},
					"keep_recent": map[string]any{
						"type":        "number",
						"description": "Recent message count to preserve exactly. Runtime clamps this between 8 and 40.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Why compaction is needed and what continuity must be preserved.",
					},
				},
			},
		},
	}
}

func (t *CompactContextTool) Run(ctx context.Context, args string) string {
	var input struct {
		AgentName  string `json:"agent_name"`
		KeepRecent int    `json:"keep_recent"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return fmt.Sprintf("Error: failed to parse arguments: %v", err)
	}
	keepRecent := clamp(input.KeepRecent, 8, 40)
	if keepRecent == 0 {
		keepRecent = 16
	}

	clean := removeCompactionMessages(t.toolset.Session.Messages)
	systemEnd := leadingSystemMessages(clean)
	compactEnd := len(clean) - keepRecent
	if compactEnd <= systemEnd {
		return "Context is already short; no compaction needed."
	}

	selected := clean[systemEnd:compactEnd]
	prompt := buildCompactionPrompt(t.toolset.Session, selected, input.Reason)
	summary, err := t.toolset.runSummarizer(ctx, input.AgentName, prompt)
	if err != nil {
		return fmt.Sprintf("Error: failed to compact context: %v", err)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "Error: summarizer returned an empty summary"
	}

	next := make([]openai.ChatCompletionMessage, 0, systemEnd+1+len(clean[compactEnd:]))
	next = append(next, clean[:systemEnd]...)
	next = append(next, openai.SystemMessage(compactionSummaryMark+"\n"+summary))
	next = append(next, clean[compactEnd:]...)

	t.toolset.Session.Messages = next
	t.toolset.Session.Summary = summary
	t.toolset.Session.AppendCompaction(systemEnd, compactEnd, keepRecent, summary)
	if err := t.toolset.Session.Save(); err != nil {
		return fmt.Sprintf("Error: failed to save compacted session: %v", err)
	}
	return fmt.Sprintf("Context compacted. Replaced %d older messages and kept %d recent messages.", len(selected), len(clean)-compactEnd)
}

func (t *SessionToolset) runSummarizer(ctx context.Context, agentName, prompt string) (string, error) {
	if t.Runner == nil {
		return "", fmt.Errorf("session summarizer runner is not configured")
	}
	explicitAgent := strings.TrimSpace(agentName) != ""
	if strings.TrimSpace(agentName) == "" {
		agentName = defaultSummarizerAgent
	}
	result, err := t.Runner.RunAgent(ctx, agentName, prompt)
	if err == nil || explicitAgent || t.Session.AgentName == "" || t.Session.AgentName == agentName {
		return result, err
	}
	return t.Runner.RunAgent(ctx, t.Session.AgentName, prompt)
}

func sanitizeTitle(title string) string {
	title = strings.Join(strings.Fields(strings.Trim(title, "\"'` \t\r\n")), " ")
	title = strings.Trim(title, "#*_`[]()")
	if len(title) > 80 {
		title = title[:80]
	}
	return strings.TrimSpace(title)
}

func clamp(value, min, max int) int {
	if value == 0 {
		return 0
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func removeCompactionMessages(messages []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == openai.RoleSystem && strings.HasPrefix(strings.TrimSpace(msg.Content), compactionSummaryMark) {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func leadingSystemMessages(messages []openai.ChatCompletionMessage) int {
	i := 0
	for i < len(messages) && messages[i].Role == openai.RoleSystem {
		i++
	}
	return i
}

func buildSummaryPrompt(sess *session.Session, messages []openai.ChatCompletionMessage, instructions string) string {
	var sb strings.Builder
	sb.WriteString("Update the durable summary for this AI agent session.\n")
	sb.WriteString("Write concise plain text with these sections: Goal, Decisions, Current State, Important Files, Open Tasks, User Preferences.\n")
	sb.WriteString("Preserve durable facts and omit throwaway small talk.\n")
	if sess.Summary != "" {
		sb.WriteString("\nExisting summary:\n")
		sb.WriteString(sess.Summary)
		sb.WriteString("\n")
	}
	if strings.TrimSpace(instructions) != "" {
		sb.WriteString("\nExtra instructions:\n")
		sb.WriteString(instructions)
		sb.WriteString("\n")
	}
	sb.WriteString("\nConversation messages:\n")
	sb.WriteString(messagesToText(messages))
	return sb.String()
}

func buildCompactionPrompt(sess *session.Session, messages []openai.ChatCompletionMessage, reason string) string {
	var sb strings.Builder
	sb.WriteString("Summarize older conversation messages for context compaction.\n")
	sb.WriteString("The summary will replace those messages in the model context, so preserve continuity precisely.\n")
	sb.WriteString("Include user goals, decisions, constraints, files changed, commands/results that matter, unresolved TODOs, and user preferences.\n")
	sb.WriteString("Do not include markdown tables. Keep it compact but complete.\n")
	if sess.Summary != "" {
		sb.WriteString("\nExisting durable summary:\n")
		sb.WriteString(sess.Summary)
		sb.WriteString("\n")
	}
	if strings.TrimSpace(reason) != "" {
		sb.WriteString("\nCompaction reason:\n")
		sb.WriteString(reason)
		sb.WriteString("\n")
	}
	sb.WriteString("\nMessages to compact:\n")
	sb.WriteString(messagesToText(messages))
	return sb.String()
}

func messagesToText(messages []openai.ChatCompletionMessage) string {
	var sb strings.Builder
	for i, msg := range messages {
		sb.WriteString(fmt.Sprintf("\n[%d] role=%s", i, msg.Role))
		if msg.Name != "" {
			sb.WriteString(" name=")
			sb.WriteString(msg.Name)
		}
		if msg.ToolCallID != "" {
			sb.WriteString(" tool_call_id=")
			sb.WriteString(msg.ToolCallID)
		}
		sb.WriteString("\n")
		if msg.Content != "" {
			sb.WriteString(msg.Content)
			sb.WriteString("\n")
		}
		if msg.ReasoningContent != "" {
			sb.WriteString("Reasoning:\n")
			sb.WriteString(msg.ReasoningContent)
			sb.WriteString("\n")
		}
		for _, call := range msg.ToolCalls {
			sb.WriteString(fmt.Sprintf("Tool call: %s args=%s\n", call.Function.Name, call.Function.Arguments))
		}
	}
	return sb.String()
}
