package agent

import "github.com/lsongdev/miya-agents/openai"

type ToolCallEvent struct {
	ID        string
	Name      string
	Arguments string
	Result    string
	Status    string
	Call      openai.ToolCall
}

type SessionInfoEvent struct {
	Title string
}

type FileEvent struct {
	Name     string
	MimeType string
	Size     int
	Data     string
	URI      string
}

type UsageEvent struct{}

type EventSink interface {
	AssistantDelta(text string) error
	AssistantFile(event FileEvent) error
	ThoughtDelta(text string) error
	ToolCallStart(event ToolCallEvent) error
	ToolCallDone(event ToolCallEvent) error
	SessionInfo(event SessionInfoEvent) error
	Usage(event UsageEvent) error
	Done() error
}
