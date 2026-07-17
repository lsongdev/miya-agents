package agent

import (
	"fmt"
	"strings"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/openai"
	"github.com/lsongdev/miya-agents/session"
)

const maintenanceNoticePrefix = "[context maintenance notice]"

func (a *Agent) AppendContextMaintenanceNotice(sess *session.Session) {
	sess.Messages = removeContextMaintenanceNotices(sess.Messages)

	window := contextWindowTokens(a.Config)
	used := estimateMessagesTokens(sess.Messages)
	if window <= 0 || used <= 0 {
		return
	}

	remainingRatio := 1 - float64(used)/float64(window)
	warnRemaining := 1 - contextWarnRatio(a.Config)
	if warnRemaining <= 0 || warnRemaining >= 1 {
		warnRemaining = 0.10
	}
	if remainingRatio > warnRemaining {
		return
	}

	remainingPercent := int(remainingRatio*100 + 0.5)
	if remainingPercent < 0 {
		remainingPercent = 0
	}
	notice := fmt.Sprintf("%s ~%d%% context remaining for session %s", maintenanceNoticePrefix, remainingPercent, sess.ID)
	sess.Messages = append(sess.Messages, openai.SystemMessage(notice))
}

func contextWindowTokens(cfg *config.ProfileConfig) int {
	if cfg != nil && cfg.ContextWindowTokens > 0 {
		return cfg.ContextWindowTokens
	}
	return 128000
}

func contextWarnRatio(cfg *config.ProfileConfig) float64 {
	if cfg != nil && cfg.ContextWarnRatio > 0 {
		return cfg.ContextWarnRatio
	}
	return 0.90
}

func removeContextMaintenanceNotices(messages []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	out := messages[:0]
	for _, msg := range messages {
		if msg.Role == openai.RoleSystem && strings.HasPrefix(strings.TrimSpace(msg.Content), maintenanceNoticePrefix) {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func estimateMessagesTokens(messages []openai.ChatCompletionMessage) int {
	chars := 0
	for _, msg := range messages {
		chars += len(msg.Role) + len(msg.Name) + len(msg.Content) + len(msg.ReasoningContent) + len(msg.ToolCallID)
		for _, call := range msg.ToolCalls {
			chars += len(call.ID) + len(call.Type) + len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	if chars == 0 {
		return 0
	}
	return chars/4 + 1
}
