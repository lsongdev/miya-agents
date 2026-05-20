package acp

import (
	"context"
	"time"
)

// Message represents an ACP message.
type Message struct {
	Role  string        `json:"role"` // "user", "agent", or "agent/{name}"
	Parts []MessagePart `json:"parts"`
}

// MessagePart represents a part of an ACP message.
type MessagePart struct {
	ContentType     string         `json:"content_type"` // e.g., "text/plain", "image/png"
	Content         string         `json:"content,omitempty"`
	ContentURL      string         `json:"content_url,omitempty"`
	ContentEncoding string         `json:"content_encoding,omitempty"` // "plain" (default) or "base64"
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// Artifact is a specialized MessagePart with a required name field.
type Artifact struct {
	MessagePart
	Name string `json:"name"`
}

// RunStatus represents the status of an agent run.
type RunStatus string

const (
	StatusCreated    RunStatus = "created"
	StatusInProgress RunStatus = "in-progress"
	StatusAwaiting   RunStatus = "awaiting"
	StatusPaused     RunStatus = "paused"
	StatusCancelling RunStatus = "cancelling"
	StatusCompleted  RunStatus = "completed"
	StatusFailed     RunStatus = "failed"
	StatusCancelled  RunStatus = "cancelled"
)

// RunMode represents the execution mode for a run.
type RunMode string

const (
	ModeSync   RunMode = "sync"
	ModeAsync  RunMode = "async"
	ModeStream RunMode = "stream"
)

// RunError represents an error returned by a run.
type RunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Run represents an ACP run object.
type Run struct {
	RunID        string               `json:"run_id"`
	AgentName    string               `json:"agent_name"`
	SessionID    string               `json:"session_id,omitempty"`
	Status       RunStatus            `json:"status"`
	Output       []Message            `json:"output,omitempty"`
	AwaitRequest *MessageAwaitRequest `json:"await_request,omitempty"`
	Error        *RunError            `json:"error,omitempty"`
	CreatedAt    time.Time            `json:"created_at,omitempty"`
	FinishedAt   time.Time            `json:"finished_at,omitempty"`
}

// MessageAwaitRequest is sent when the agent pauses for external input.
type MessageAwaitRequest struct {
	RunID string `json:"run_id"`
}

// MessageAwaitResume is used to resume a paused run.
type MessageAwaitResume struct {
	Messages []Message `json:"messages"`
}

// AgentManifest represents an agent's capabilities.
type AgentManifest struct {
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	Version      string         `json:"version,omitempty"`
	Authors      []string       `json:"authors,omitempty"`
	Capabilities map[string]any `json:"capabilities,omitempty"`
}

// Session describes a distributed session with URL-referenced history.
type Session struct {
	ID      string   `json:"id"`
	History []string `json:"history"`
	State   string   `json:"state,omitempty"`
}

// RunCreateRequest is the payload to initiate a new run.
type RunCreateRequest struct {
	AgentName string    `json:"agent_name"`
	Input     []Message `json:"input"`
	Mode      RunMode   `json:"mode,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Session   *Session  `json:"session,omitempty"`
}

// RunResumeRequest is the payload to resume a paused run.
type RunResumeRequest struct {
	RunID       string             `json:"run_id"`
	AwaitResume MessageAwaitResume `json:"await_resume"`
	Mode        RunMode            `json:"mode,omitempty"`
}

// Agent is the abstract interface for the ACP business logic.
type Agent interface {
	ListAgents(ctx context.Context) ([]AgentManifest, error)
	GetAgent(ctx context.Context, name string) (*AgentManifest, error)
	CreateRun(ctx context.Context, req RunCreateRequest) (*Run, error)
	GetRun(ctx context.Context, runID string) (*Run, error)
	ResumeRun(ctx context.Context, runID string, resume MessageAwaitResume) (*Run, error)
	CancelRun(ctx context.Context, runID string) (*Run, error)
	GetSession(ctx context.Context, sessionID string) (any, error)
	StreamRun(ctx context.Context, runID string) (<-chan Event, error)
}

// Event represents an SSE event.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}
