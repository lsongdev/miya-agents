package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
			sess.SaveMessages()
			if err := sink.Usage(UsageEvent{}); err != nil {
				return err
			}
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
			if ok {
				result = tool.Run(ctx, tc.Function.Arguments)
			} else {
				result = fmt.Sprintf("Error: unknown tool '%s'", tc.Function.Name)
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
