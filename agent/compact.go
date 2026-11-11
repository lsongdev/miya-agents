package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/session"
)

// CompactOptions tunes context compaction. The zero value uses the defaults.
type CompactOptions struct {
	// Threshold is the fraction of the context window at which the loop
	// compacts on its own. Defaults to 0.8.
	Threshold float64
	// KeepTokens is roughly how much recent conversation is kept verbatim.
	// Defaults to a quarter of the context window, at most 20000.
	KeepTokens int
	// Prompt replaces the default instructions for writing the summary.
	Prompt string
}

const (
	defaultCompactThreshold = 0.8
	defaultKeepTokens       = 20000
	// summaryToolResultLimit caps each tool result quoted to the summarizer.
	// Results are the bulk of a long session and rarely matter verbatim.
	summaryToolResultLimit = 2000
)

const defaultCompactPrompt = `You are summarizing a conversation between a user and an AI agent so that the agent can continue the work without the original messages.

Write a summary that preserves everything needed to carry on:
- The user's goals and any instructions or preferences they gave.
- What has been done so far, and what was learned or decided along the way.
- Files, commands, names, numbers and other specifics that later steps depend on.
- What remains to be done, and the immediate next step if one was in progress.

Be concrete and complete rather than brief. Do not address the user, do not continue the task, and do not call tools. Output only the summary.`

// ErrNothingToCompact is returned by Compact when the conversation is too
// short for a summary to replace any of it.
var ErrNothingToCompact = errors.New("nothing to compact")

// Compact replaces the older part of the model's context with a summary,
// keeping the most recent messages verbatim. The log is only appended to.
// instructions, if any, are added to the summarizer's prompt.
func (a *Agent) Compact(ctx context.Context, sess *session.Session, instructions string, sink Sink) error {
	if a.Model == nil {
		return errors.New("agent has no model")
	}
	previous, entries := sess.Active()
	cut := a.cutPoint(entries)
	if cut <= 0 {
		return ErrNothingToCompact
	}
	before := a.contextTokens(sess)

	prompt := cmp.Or(a.Compaction.Prompt, defaultCompactPrompt)
	if instructions = strings.TrimSpace(instructions); instructions != "" {
		prompt += "\n\nAdditional instructions for this summary:\n" + instructions
	}
	request := &llm.Request{
		Model:     a.ModelName,
		System:    prompt,
		Messages:  []llm.Message{llm.UserText(transcript(previous, entries[:cut]))},
		MaxTokens: a.MaxTokens,
	}
	var builder llm.Builder
	for delta, err := range a.Model.Stream(ctx, request) {
		if err != nil {
			return fmt.Errorf("summarize: %w", err)
		}
		builder.Add(delta)
	}
	summary := strings.TrimSpace(builder.Message().Text())
	if summary == "" {
		return errors.New("summarize: model returned no summary")
	}

	err := sess.Add(session.Entry{Compaction: &session.Compaction{
		Summary:      summary,
		FirstKept:    entries[cut].ID,
		TokensBefore: before,
	}})
	if err != nil {
		return err
	}
	return sink(Event{Type: EventCompaction, Compaction: &CompactionEvent{
		TokensBefore: before,
		TokensAfter:  a.contextTokens(sess),
	}})
}

// cutPoint returns the index in entries of the first one to keep, or 0 when
// there is nothing worth summarizing before it.
//
// Walking back from the end, it is the first entry that both leaves at least
// KeepTokens after it and is a safe place to start a context: a user or
// assistant message. A tool message is never first, since its call would be
// summarized away.
func (a *Agent) cutPoint(entries []session.Entry) int {
	keep := a.Compaction.KeepTokens
	if keep <= 0 {
		keep = defaultKeepTokens
		if a.ContextWindow > 0 {
			keep = min(keep, a.ContextWindow/4)
		}
	}
	kept := 0
	for i := len(entries) - 1; i > 0; i-- {
		kept += estimateTokens(*entries[i].Message)
		if kept >= keep && entries[i].Message.Role != llm.RoleTool {
			return i
		}
	}
	return 0
}

// needsCompaction reports whether the context has grown past the threshold.
// Without a known context window the loop never compacts on its own.
func (a *Agent) needsCompaction(sess *session.Session) bool {
	if a.ContextWindow <= 0 {
		return false
	}
	threshold := a.Compaction.Threshold
	if threshold <= 0 || threshold >= 1 {
		threshold = defaultCompactThreshold
	}
	return float64(a.contextTokens(sess)) >= threshold*float64(a.ContextWindow)
}

// contextTokens estimates the size of the context the next model call would
// send. It starts from the provider's own count for the latest call, when
// one was made since the last compaction, and estimates only what was added
// after it.
func (a *Agent) contextTokens(sess *session.Session) int {
	added := 0
	for i := len(sess.Entries) - 1; i >= 0; i-- {
		entry := sess.Entries[i]
		if entry.Compaction != nil {
			break
		}
		if entry.Message == nil {
			continue
		}
		if entry.Usage != nil {
			return entry.Usage.InputTokens + entry.Usage.OutputTokens + added
		}
		added += estimateTokens(*entry.Message)
	}
	total := len(a.System) / charsPerToken
	for _, message := range sess.Context() {
		total += estimateTokens(message)
	}
	return total
}

// charsPerToken is a deliberately rough ratio. It only has to be good enough
// to decide when to compact; real counts come from the provider.
const charsPerToken = 4

func estimateTokens(message llm.Message) int {
	chars := 0
	for _, block := range message.Content {
		chars += len(block.Text) + len(block.Signature)
		if call := block.ToolCall; call != nil {
			chars += len(call.Name) + len(call.Arguments)
		}
		if result := block.ToolResult; result != nil {
			chars += len(result.Content)
		}
		if block.Media != nil {
			// Providers bill images by size, not bytes; assume a typical one.
			chars += 1000 * charsPerToken
		}
	}
	return chars/charsPerToken + 1
}

// transcript renders the part of a conversation being summarized as plain
// text. Sending it as one document, not as chat messages, keeps the model
// from continuing the conversation and works without declaring the tools the
// messages refer to.
func transcript(previous string, entries []session.Entry) string {
	var sb strings.Builder
	if previous != "" {
		sb.WriteString("[Summary of the conversation before this point]\n" + previous + "\n\n")
	}
	for _, entry := range entries {
		for _, block := range entry.Message.Content {
			switch block.Type {
			case llm.BlockText:
				label := "Assistant"
				if entry.Message.Role == llm.RoleUser {
					label = "User"
				}
				sb.WriteString("[" + label + "]\n" + block.Text + "\n\n")
			case llm.BlockImage:
				sb.WriteString("[User attached an image]\n\n")
			case llm.BlockToolCall:
				if call := block.ToolCall; call != nil {
					sb.WriteString("[Assistant called tool " + call.Name + "]\n" + call.Arguments + "\n\n")
				}
			case llm.BlockToolResult:
				if result := block.ToolResult; result != nil {
					sb.WriteString("[Tool result]\n" + truncate(result.Content, summaryToolResultLimit) + "\n\n")
				}
			}
		}
	}
	sb.WriteString("Summarize the conversation above.")
	return sb.String()
}

// truncate shortens text to about limit bytes, keeping its start and end and
// never splitting a UTF-8 sequence.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	half := limit / 2
	head, tail := text[:half], text[len(text)-half:]
	head = strings.ToValidUTF8(head, "")
	tail = strings.ToValidUTF8(tail, "")
	return fmt.Sprintf("%s\n[... %d bytes omitted ...]\n%s", head, len(text)-len(head)-len(tail), tail)
}

// isContextOverflow reports whether a model error says the request was too
// long. Providers have no common error code for this, so it goes by the
// wording they use.
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, phrase := range []string{
		"context_length_exceeded",
		"maximum context length",
		"context window",
		"prompt is too long",
		"input is too long",
		"too many tokens",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

// CompactCommand reports whether prompt is the /compact command, and returns
// the instructions that follow it.
func CompactCommand(prompt string) (instructions string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(prompt), "/compact")
	if !found || (rest != "" && !strings.ContainsAny(rest[:1], " \t\n")) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// RunCompactCommand compacts sess on request and returns the reply to show.
// The command is not added to the log; only the compaction it causes is.
func RunCompactCommand(ctx context.Context, ag *Agent, sess *session.Session, instructions string) (string, error) {
	var note string
	err := ag.Compact(ctx, sess, instructions, func(event Event) error {
		note = CompactionNote(event) + "."
		return nil
	})
	if errors.Is(err, ErrNothingToCompact) {
		return "Nothing to compact yet: the conversation is still short.", nil
	}
	if err != nil {
		return "", fmt.Errorf("compact: %w", err)
	}
	return note, nil
}

// CompactionNote describes a compaction event in one line.
func CompactionNote(event Event) string {
	c := event.Compaction
	switch {
	case c == nil:
		return "Context compaction failed: " + event.Text
	case c.TokensAfter > 0:
		return fmt.Sprintf("Context compacted: about %d → %d tokens", c.TokensBefore, c.TokensAfter)
	default:
		return "Context compacted"
	}
}
