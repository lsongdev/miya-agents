package agent

import (
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/tools"
)

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
	// EventDone marks the end of the agent's turn.
	EventDone EventType = "done"
)

// Event is something the agent loop reports while it runs. Type selects which
// fields are set.
type Event struct {
	Type  EventType
	Text  string
	Tool  *ToolEvent
	File  *File
	Usage *llm.Usage
	// ContextWindow accompanies Usage: the model's context size in tokens,
	// or zero if unknown.
	ContextWindow int
	Title         string
}

// ToolEvent describes a tool call. Result and Failed are set once it is done.
type ToolEvent struct {
	Call   llm.ToolCall
	Result string
	Failed bool
}

// File is a file handed to the user.
type File = tools.File

// Sink receives the loop's events in order. Returning an error stops the
// loop. A sink handles the event types it knows and ignores the rest, so new
// types can be added without changing existing sinks.
type Sink func(Event) error

// Discard is a Sink that ignores every event.
func Discard(Event) error { return nil }
