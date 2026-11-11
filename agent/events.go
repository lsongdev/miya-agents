package agent

import "github.com/lsongdev/miya-agents/llm"

type EventType string

const (
	// EventText and EventThought carry an increment of streamed output in Text.
	EventText    EventType = "text"
	EventThought EventType = "thought"
	// EventToolStart and EventToolDone bracket one tool call, in Tool.
	EventToolStart EventType = "tool_start"
	EventToolDone  EventType = "tool_done"
	// EventFile hands a file to the user, in File.
	EventFile EventType = "file"
	// EventUsage reports the token usage of one model call, in Usage.
	EventUsage EventType = "usage"
	// EventSessionInfo reports changed session metadata, in Title.
	EventSessionInfo EventType = "session_info"
	// EventCompaction reports that older context was replaced by a summary,
	// in Compaction. When compaction was attempted and failed, Compaction is
	// nil and Text holds the reason.
	EventCompaction EventType = "compaction"
	// EventDone marks the end of the agent's turn.
	EventDone EventType = "done"
)

// Event is something the agent loop reports while it runs. Type selects which
// fields are set.
type Event struct {
	Type  EventType
	Text  string
	Tool  *ToolEvent
	File  *llm.Media
	Usage *llm.Usage
	// ContextWindow accompanies Usage: the model's context size in tokens,
	// or zero if unknown.
	ContextWindow int
	Title         string
	Compaction    *CompactionEvent
}

// CompactionEvent gives the estimated context size around a compaction.
type CompactionEvent struct {
	TokensBefore int
	TokensAfter  int
}

// ToolEvent describes a tool call. Result and Failed are set once it is done.
type ToolEvent struct {
	Call   llm.ToolCall
	Result string
	Failed bool
}

// Sink receives the loop's events in order. Returning an error stops the
// loop. A sink handles the event types it knows and ignores the rest, so new
// types can be added without changing existing sinks.
type Sink func(Event) error

// Discard is a Sink that ignores every event.
func Discard(Event) error { return nil }
