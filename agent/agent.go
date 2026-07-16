package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/session"
	"github.com/lsongdev/miya-agents/tools"
)

type Agent struct {
	Name   string
	Config *config.ProfileConfig
	LLM    *openai.Client
	// tools
	toolsMap  map[string]openai.Tool
	toolsDefs []openai.ToolDef
}

func (a *Agent) RunAgentLoop(ctx context.Context, sess *session.Session, sink EventSink) error {
	for {
		req := openai.ChatCompletionRequest{
			Model:    a.Config.ModelName,
			Messages: sess.Messages,
			Tools:    a.toolsDefs,
			Stream:   true,
		}
		var respMessage openai.ChatCompletionMessage
		if req.Stream {
			resp, err := a.LLM.CreateChatCompletionStream(ctx, &req)
			if err != nil {
				err = fmt.Errorf("Error: failed to create chat completion stream: %v", err)
				return err
			}
			builder := openai.NewMessageBuilder()
			for chunk := range resp {
				if chunk.Error != nil {
					return fmt.Errorf("API error: %s", chunk.Error.Message)
				}
				m := chunk.GetMessage()
				if m == nil {
					continue
				}
				builder.Update(*m)
				if m.ReasoningContent != "" {
					if err := sink.ThoughtDelta(m.ReasoningContent); err != nil {
						return err
					}
				}
				if m.Content != "" {
					if err := sink.AssistantDelta(m.Content); err != nil {
						return err
					}
				}
			}
			respMessage = builder.Build()
		} else {
			resp, err := a.LLM.CreateChatCompletion(ctx, &req)
			if err != nil {
				err = fmt.Errorf("Error failed to create chat completion: %v", err)
				return err
			}
			m := resp.GetMessage()
			if m == nil {
				return fmt.Errorf("no message in response")
			}
			respMessage = *m
			if respMessage.ReasoningContent != "" {
				if err := sink.ThoughtDelta(respMessage.ReasoningContent); err != nil {
					return err
				}
			}
			if respMessage.Content != "" {
				if err := sink.AssistantDelta(respMessage.Content); err != nil {
					return err
				}
			}
		}
		sess.AppendResponse(respMessage)
		// finish
		if !respMessage.HasToolCall() {
			if err := sink.Usage(UsageEvent{}); err != nil {
				return err
			}
			sess.SaveMessages()
			if err := sink.Done(); err != nil {
				return err
			}
			return nil
		}
		// Execute tool calls
		for _, tc := range respMessage.ToolCalls {
			if tc.ID == "" {
				continue
			}
			tool, ok := a.toolsMap[tc.Function.Name]
			var result string
			if err := sink.ToolCallStart(ToolCallEvent{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
				Status:    "in_progress",
				Call:      tc,
			}); err != nil {
				return err
			}
			titleBefore := sess.Title
			if ok {
				result = tool.Run(ctx, tc.Function.Arguments)
			} else {
				result = fmt.Sprintf("Error: unknown tool '%s'", tc.Function.Name)
			}
			if sess.Title != "" && sess.Title != titleBefore {
				if err := sink.SessionInfo(SessionInfoEvent{Title: sess.Title}); err != nil {
					return err
				}
			}
			status := "completed"
			if !ok {
				status = "failed"
			}
			if err := sink.ToolCallDone(ToolCallEvent{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
				Result:    result,
				Status:    status,
				Call:      tc,
			}); err != nil {
				return err
			}
			sess.Messages = append(sess.Messages, openai.ToolResultMessage(tc.ID, tc.Function.Name, result))
		}
		sess.SaveMessages()
	}
}

func (a *Agent) AddTool(tool openai.Tool) {
	d := tool.Def()
	a.toolsMap[d.Function.Name] = tool
	a.toolsDefs = append(a.toolsDefs, d)
}

func (a *Agent) AddSessionTools(sess *session.Session, runner tools.AgentRunner) {
	for _, tool := range tools.NewSessionTools(sess, runner) {
		a.AddTool(tool)
	}
	a.ensureInternalToolInstructions(sess)
}

func (a *Agent) NewSession() *session.Session {
	s := session.New(a.Name)
	prompt := a.readSystemPrompt()
	if prompt != "" {
		s.Messages = append(s.Messages, openai.SystemMessage(prompt))
	}
	return s
}

func (a *Agent) NewSessionWithPrompt(prompt string) *session.Session {
	s := session.New(a.Name)
	if prompt != "" {
		s.Messages = append(s.Messages, openai.SystemMessage(prompt))
	}
	return s
}

func (a *Agent) readSystemPrompt() string {
	workspace := a.Config.GetWorkspace()
	if workspace == "" {
		return "You are a helpful assistant."
	}
	data, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		return "You are a helpful assistant."
	}
	return string(data)
}

func (a *Agent) ensureInternalToolInstructions(sess *session.Session) {
	if len(sess.Messages) == 0 || sess.Messages[0].Role != openai.RoleSystem {
		sess.Messages = append([]openai.ChatCompletionMessage{openai.SystemMessage(a.readSystemPrompt())}, sess.Messages...)
	}
	if strings.Contains(sess.Messages[0].Content, "Internal session tools:") {
		return
	}
	sess.Messages[0].Content = strings.TrimSpace(sess.Messages[0].Content) + `

Internal session tools:
- Use rename_session once when the user's goal is clear and the session title is empty or generic. Titles must be short, plain text, and user-facing.
- Use summarize_session when durable decisions, current task state, important files, TODOs, or user preferences should be preserved for later turns.
- Use compact_context when the conversation is getting long. Preserve user goals, decisions, constraints, changed files, important command results, unresolved TODOs, and user preferences.
- These tools update session metadata/model context only. Do not mention them unless the user asks about session management.`
}

func (a *Agent) BuildTools() {
	workspace := a.Config.GetWorkspace()
	var tools = []openai.Tool{
		&tools.WebFetchTool{},
		&tools.WebSearchTool{},
		&tools.ReadFileTool{Workspace: workspace},
		&tools.WriteFileTool{Workspace: workspace},
		&tools.AppendFileTool{Workspace: workspace},
		&tools.EditFileTool{Workspace: workspace},
		&tools.ExecTool{
			Workspace:           workspace,
			DefaultTimeout:      tools.ExecDefaultTimeoutSeconds,
			RestrictToWorkspace: true,
		},
		&tools.SkillsTool{
			Workspace: filepath.Join(config.ConfigPath, "skills"),
		},
	}
	for _, t := range tools {
		a.AddTool(t)
	}
}
