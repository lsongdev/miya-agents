package acp

import "encoding/json"

// Protocol
type ProtocolVersion uint16

// Identifiers
type SessionID string
type MessageID string
type SessionModeID string
type SessionConfigID string
type SessionConfigValueID string
type SessionConfigGroupID string
type ToolCallID string
type PermissionOptionID string
type RequestID any // string | number | null

// Meta is a generic metadata container.
type Meta = map[string]any

// === Capabilities ===

type ClientCapabilities struct {
	Fs       *FileSystemCapabilities `json:"fs,omitempty"`
	Terminal bool                    `json:"terminal"`
}

type FileSystemCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type AgentCapabilities struct {
	LoadSession         bool                  `json:"loadSession"`
	PromptCapabilities  PromptCapabilities    `json:"promptCapabilities"`
	McpCapabilities     McpCapabilities       `json:"mcpCapabilities"`
	SessionCapabilities SessionCapabilities   `json:"sessionCapabilities"`
	Auth                AgentAuthCapabilities `json:"auth"`
}

type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type McpCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

type SessionCapabilities struct {
	AdditionalDirectories *SessionAdditionalDirectoriesCapabilities `json:"additionalDirectories,omitempty"`
	Close                 *SessionCloseCapabilities                 `json:"close,omitempty"`
	Delete                *SessionDeleteCapabilities                `json:"delete,omitempty"`
	List                  *SessionListCapabilities                  `json:"list,omitempty"`
	Resume                *SessionResumeCapabilities                `json:"resume,omitempty"`
}

type SessionAdditionalDirectoriesCapabilities struct{}
type SessionCloseCapabilities struct{}
type SessionDeleteCapabilities struct{}
type SessionListCapabilities struct{}
type SessionResumeCapabilities struct{}

type AgentAuthCapabilities struct {
	Logout *LogoutCapabilities `json:"logout,omitempty"`
}

type LogoutCapabilities struct{}

// === Auth ===

type AuthMethod struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// === Implementation Info ===

type Implementation struct {
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Title   *string `json:"title,omitempty"`
}

// === MCP Server Config ===

type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// McpServer represents an MCP server configuration.
// For stdio: set Command, Args, Env, Name.
// For HTTP:  set Name, URL, Headers.
// For SSE:   set Name, URL, Headers.
type McpServer struct {
	Command string        `json:"command,omitempty"`
	Args    []string      `json:"args,omitempty"`
	Env     []EnvVariable `json:"env,omitempty"`
	Name    string        `json:"name"`
	URL     string        `json:"url,omitempty"`
	Headers []HTTPHeader  `json:"headers,omitempty"`
}

// === Content Blocks ===

type Role string

const (
	RoleAssistant Role = "assistant"
	RoleUser      Role = "user"
)

type Annotations struct {
	Audience     []Role   `json:"audience,omitempty"`
	Priority     *float64 `json:"priority,omitempty"`
	LastModified *string  `json:"lastModified,omitempty"`
}

type TextContent struct {
	Type        string       `json:"type"`
	Text        string       `json:"text"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

type ImageContent struct {
	Type        string       `json:"type"`
	Data        string       `json:"data"`
	MimeType    string       `json:"mimeType"`
	URI         *string      `json:"uri,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

type AudioContent struct {
	Type        string       `json:"type"`
	Data        string       `json:"data"`
	MimeType    string       `json:"mimeType"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

type ResourceLink struct {
	Type        string       `json:"type"`
	URI         string       `json:"uri"`
	Name        string       `json:"name"`
	Description *string      `json:"description,omitempty"`
	MimeType    *string      `json:"mimeType,omitempty"`
	Size        *int         `json:"size,omitempty"`
	Title       *string      `json:"title,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

type TextResourceContents struct {
	URI      string  `json:"uri"`
	Text     string  `json:"text"`
	MimeType *string `json:"mimeType,omitempty"`
}

type BlobResourceContents struct {
	URI      string  `json:"uri"`
	Blob     string  `json:"blob"`
	MimeType *string `json:"mimeType,omitempty"`
}

type EmbeddedResource struct {
	Type        string          `json:"type"`
	Resource    json.RawMessage `json:"resource,omitempty"`
	Annotations *Annotations    `json:"annotations,omitempty"`
}

type ContentBlock struct {
	Type        string          `json:"type"`
	Text        string          `json:"text,omitempty"`
	Data        string          `json:"data,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	URI         *string         `json:"uri,omitempty"`
	Name        string          `json:"name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Size        *int            `json:"size,omitempty"`
	Title       *string         `json:"title,omitempty"`
	Resource    json.RawMessage `json:"resource,omitempty"`
	Annotations *Annotations    `json:"annotations,omitempty"`
}

// === Tool Call Types ===

type ToolKind string

const (
	ToolKindRead       ToolKind = "read"
	ToolKindEdit       ToolKind = "edit"
	ToolKindDelete     ToolKind = "delete"
	ToolKindMove       ToolKind = "move"
	ToolKindSearch     ToolKind = "search"
	ToolKindExecute    ToolKind = "execute"
	ToolKindThink      ToolKind = "think"
	ToolKindFetch      ToolKind = "fetch"
	ToolKindSwitchMode ToolKind = "switch_mode"
	ToolKindOther      ToolKind = "other"
)

type ToolCallStatus string

const (
	ToolCallPending    ToolCallStatus = "pending"
	ToolCallInProgress ToolCallStatus = "in_progress"
	ToolCallCompleted  ToolCallStatus = "completed"
	ToolCallFailed     ToolCallStatus = "failed"
)

type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

type ToolCallContent struct {
	Type       string        `json:"type"`
	Content    *ContentBlock `json:"content,omitempty"`
	Path       string        `json:"path,omitempty"`
	OldText    *string       `json:"oldText,omitempty"`
	NewText    string        `json:"newText,omitempty"`
	TerminalID string        `json:"terminalId,omitempty"`
}

type ToolCall struct {
	ToolCallID ToolCallID         `json:"toolCallId"`
	Title      string             `json:"title"`
	Kind       ToolKind           `json:"kind"`
	Status     ToolCallStatus     `json:"status"`
	Content    []ToolCallContent  `json:"content"`
	Locations  []ToolCallLocation `json:"locations"`
	RawInput   json.RawMessage    `json:"rawInput"`
	RawOutput  json.RawMessage    `json:"rawOutput"`
}

type ToolCallUpdate struct {
	ToolCallID ToolCallID         `json:"toolCallId"`
	Title      *string            `json:"title,omitempty"`
	Kind       *ToolKind          `json:"kind,omitempty"`
	Status     *ToolCallStatus    `json:"status,omitempty"`
	Content    []ToolCallContent  `json:"content,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
	RawInput   json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage    `json:"rawOutput,omitempty"`
}

// === Permission Types ===

type PermissionOptionKind string

const (
	PermissionAllowOnce    PermissionOptionKind = "allow_once"
	PermissionAllowAlways  PermissionOptionKind = "allow_always"
	PermissionRejectOnce   PermissionOptionKind = "reject_once"
	PermissionRejectAlways PermissionOptionKind = "reject_always"
)

type PermissionOption struct {
	OptionID PermissionOptionID   `json:"optionId"`
	Name     string               `json:"name"`
	Kind     PermissionOptionKind `json:"kind"`
}

type RequestPermissionOutcome struct {
	Outcome  string             `json:"outcome"`
	OptionID PermissionOptionID `json:"optionId,omitempty"`
}

// === Stop Reason ===

type StopReason string

const (
	StopEndTurn     StopReason = "end_turn"
	StopMaxTokens   StopReason = "max_tokens"
	StopMaxTurnReqs StopReason = "max_turn_requests"
	StopRefusal     StopReason = "refusal"
	StopCancelled   StopReason = "cancelled"
)

// === Terminal Types ===

type TerminalExitStatus struct {
	ExitCode *int    `json:"exitCode,omitempty"`
	Signal   *string `json:"signal,omitempty"`
}

// === Session Config Types ===

type SessionConfigSelectOption struct {
	Value       SessionConfigValueID `json:"value"`
	Name        string               `json:"name"`
	Description *string              `json:"description,omitempty"`
}

type SessionConfigSelectGroup struct {
	Group   SessionConfigGroupID        `json:"group"`
	Name    string                      `json:"name"`
	Options []SessionConfigSelectOption `json:"options"`
}

type SessionConfigOption struct {
	Type         string                      `json:"type"`
	ID           SessionConfigID             `json:"id,omitempty"`
	CurrentValue SessionConfigValueID        `json:"currentValue"`
	Options      []SessionConfigSelectOption `json:"options,omitempty"`
	Groups       []SessionConfigSelectGroup  `json:"groups,omitempty"`
}

type SessionMode struct {
	ID          SessionModeID `json:"id"`
	Name        string        `json:"name"`
	Description *string       `json:"description,omitempty"`
}

type SessionModeState struct {
	CurrentModeID  SessionModeID `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

// === Session Info ===

type SessionInfo struct {
	SessionID             SessionID `json:"sessionId"`
	Cwd                   string    `json:"cwd"`
	Title                 *string   `json:"title,omitempty"`
	UpdatedAt             *string   `json:"updatedAt,omitempty"`
	AdditionalDirectories []string  `json:"additionalDirectories"`
	Meta                  Meta      `json:"_meta,omitempty"`
}

// === Session Update Variants ===

type PlanEntryStatus string

const (
	PlanPending    PlanEntryStatus = "pending"
	PlanInProgress PlanEntryStatus = "in_progress"
	PlanCompleted  PlanEntryStatus = "completed"
)

type PlanEntryPriority string

const (
	PlanPriorityHigh   PlanEntryPriority = "high"
	PlanPriorityMedium PlanEntryPriority = "medium"
	PlanPriorityLow    PlanEntryPriority = "low"
)

type PlanEntry struct {
	Content  string            `json:"content"`
	Status   PlanEntryStatus   `json:"status"`
	Priority PlanEntryPriority `json:"priority"`
}

type Plan struct {
	Entries []PlanEntry `json:"entries"`
}

type AvailableCommand struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Input       *AvailableCommandInput `json:"input,omitempty"`
}

type AvailableCommandInput struct {
	Hint string `json:"hint"`
}

type AvailableCommandsUpdate struct {
	AvailableCommands []AvailableCommand `json:"availableCommands"`
}

type SessionInfoUpdate struct {
	Title     *string `json:"title,omitempty"`
	UpdatedAt *string `json:"updatedAt,omitempty"`
}

type CurrentModeUpdate struct {
	CurrentModeID SessionModeID `json:"currentModeId"`
}

type ConfigOptionUpdate struct {
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

type Cost struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type UsageUpdate struct {
	Size uint64 `json:"size"`
	Used uint64 `json:"used"`
	Cost *Cost  `json:"cost,omitempty"`
}

type SessionUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`

	// user_message_chunk / agent_message_chunk
	Content   ContentBlock `json:"content,omitempty"`
	MessageID *MessageID   `json:"messageId,omitempty"`

	// agent_thought_chunk
	Thought string `json:"thought,omitempty"`

	// tool_call
	ToolCall *ToolCall `json:"toolCall,omitempty"`

	// tool_call_update
	ToolCallUpdate *ToolCallUpdate `json:"toolCallUpdate,omitempty"`

	// plan
	Plan *Plan `json:"plan,omitempty"`

	// available_commands_update
	AvailableCommands *AvailableCommandsUpdate `json:"availableCommands,omitempty"`

	// current_mode_update
	CurrentMode *CurrentModeUpdate `json:"currentMode,omitempty"`

	// config_option_update
	ConfigOption *ConfigOptionUpdate `json:"configOption,omitempty"`

	// session_info_update
	SessionInfo *SessionInfoUpdate `json:"sessionInfo,omitempty"`

	// usage_update
	Usage *UsageUpdate `json:"usage,omitempty"`

	Meta Meta `json:"_meta,omitempty"`
}

func (u SessionUpdate) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"sessionUpdate": u.SessionUpdate,
	}
	if len(u.Meta) > 0 {
		out["_meta"] = u.Meta
	}

	switch u.SessionUpdate {
	case "user_message_chunk", "agent_message_chunk":
		out["content"] = u.Content
		if u.MessageID != nil {
			out["messageId"] = u.MessageID
		}
	case "agent_thought_chunk":
		if u.Thought != "" {
			out["thought"] = u.Thought
		}
		if u.Content.Type != "" {
			out["content"] = u.Content
		}
	case "tool_call":
		if u.ToolCall != nil {
			out["toolCall"] = u.ToolCall
		}
	case "tool_call_update":
		if u.ToolCallUpdate != nil {
			out["toolCallUpdate"] = u.ToolCallUpdate
		}
	case "plan":
		if u.Plan != nil {
			out["plan"] = u.Plan
		}
	case "available_commands_update":
		if u.AvailableCommands != nil {
			out["availableCommands"] = u.AvailableCommands
		}
	case "current_mode_update":
		if u.CurrentMode != nil {
			out["currentMode"] = u.CurrentMode
		}
	case "config_option_update":
		if u.ConfigOption != nil {
			out["configOption"] = u.ConfigOption
		}
	case "session_info_update":
		if u.SessionInfo != nil {
			out["sessionInfo"] = u.SessionInfo
		}
	case "usage_update":
		if u.Usage != nil {
			out["usage"] = u.Usage
		}
	default:
		type alias SessionUpdate
		return json.Marshal(alias(u))
	}
	return json.Marshal(out)
}

// === JSON-RPC Request/Response Types ===

type InitializeRequest struct {
	ProtocolVersion    ProtocolVersion    `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         *Implementation    `json:"clientInfo,omitempty"`
	Meta               Meta               `json:"_meta,omitempty"`
}

type InitializeResponse struct {
	ProtocolVersion   ProtocolVersion   `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
	AgentInfo         *Implementation   `json:"agentInfo,omitempty"`
	Meta              Meta              `json:"_meta,omitempty"`
}

type AuthenticateRequest struct {
	MethodID string `json:"methodId"`
	Meta     Meta   `json:"_meta,omitempty"`
}

type AuthenticateResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

type NewSessionRequest struct {
	Cwd                   string      `json:"cwd"`
	McpServers            []McpServer `json:"mcpServers"`
	AdditionalDirectories []string    `json:"additionalDirectories,omitempty"`
	Meta                  Meta        `json:"_meta,omitempty"`
}

type NewSessionResponse struct {
	SessionID     SessionID             `json:"sessionId"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	Meta          Meta                  `json:"_meta,omitempty"`
}

type PromptRequest struct {
	SessionID SessionID      `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
	Meta      Meta           `json:"_meta,omitempty"`
}

type PromptResponse struct {
	StopReason StopReason `json:"stopReason"`
	Meta       Meta       `json:"_meta,omitempty"`
}

type LoadSessionRequest struct {
	SessionID             SessionID   `json:"sessionId"`
	Cwd                   string      `json:"cwd"`
	McpServers            []McpServer `json:"mcpServers"`
	AdditionalDirectories []string    `json:"additionalDirectories,omitempty"`
	Meta                  Meta        `json:"_meta,omitempty"`
}

type LoadSessionResponse struct {
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	Meta          Meta                  `json:"_meta,omitempty"`
}

type ResumeSessionRequest struct {
	SessionID             SessionID   `json:"sessionId"`
	Cwd                   string      `json:"cwd"`
	McpServers            []McpServer `json:"mcpServers"`
	AdditionalDirectories []string    `json:"additionalDirectories,omitempty"`
	Meta                  Meta        `json:"_meta,omitempty"`
}

type ResumeSessionResponse struct {
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	Meta          Meta                  `json:"_meta,omitempty"`
}

type CloseSessionRequest struct {
	SessionID SessionID `json:"sessionId"`
	Meta      Meta      `json:"_meta,omitempty"`
}

type CloseSessionResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

type DeleteSessionRequest struct {
	SessionID SessionID `json:"sessionId"`
	Meta      Meta      `json:"_meta,omitempty"`
}

type DeleteSessionResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

type ListSessionsRequest struct {
	Cursor *string `json:"cursor,omitempty"`
	Cwd    *string `json:"cwd,omitempty"`
	Meta   Meta    `json:"_meta,omitempty"`
}

type ListSessionsResponse struct {
	Sessions   []SessionInfo `json:"sessions"`
	NextCursor *string       `json:"nextCursor,omitempty"`
	Meta       Meta          `json:"_meta,omitempty"`
}

type SetSessionModeRequest struct {
	SessionID SessionID     `json:"sessionId"`
	ModeID    SessionModeID `json:"modeId"`
	Meta      Meta          `json:"_meta,omitempty"`
}

type SetSessionModeResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

type SetSessionConfigOptionRequest struct {
	SessionID SessionID            `json:"sessionId"`
	ConfigID  SessionConfigID      `json:"configId"`
	Value     SessionConfigValueID `json:"value"`
	Meta      Meta                 `json:"_meta,omitempty"`
}

type SetSessionConfigOptionResponse struct {
	ConfigOptions []SessionConfigOption `json:"configOptions"`
	Meta          Meta                  `json:"_meta,omitempty"`
}

type LogoutRequest struct {
	Meta Meta `json:"_meta,omitempty"`
}

type LogoutResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

type CancelNotification struct {
	SessionID SessionID `json:"sessionId"`
	Meta      Meta      `json:"_meta,omitempty"`
}

// === Client Methods (called by Agent on Client) ===

type RequestPermissionRequest struct {
	SessionID SessionID          `json:"sessionId"`
	ToolCall  ToolCallUpdate     `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
	Meta      Meta               `json:"_meta,omitempty"`
}

type RequestPermissionResponse struct {
	Outcome RequestPermissionOutcome `json:"outcome"`
	Meta    Meta                     `json:"_meta,omitempty"`
}

type ReadTextFileRequest struct {
	SessionID SessionID `json:"sessionId"`
	Path      string    `json:"path"`
	Line      *int      `json:"line,omitempty"`
	Limit     *int      `json:"limit,omitempty"`
	Meta      Meta      `json:"_meta,omitempty"`
}

type ReadTextFileResponse struct {
	Content string `json:"content"`
	Meta    Meta   `json:"_meta,omitempty"`
}

type WriteTextFileRequest struct {
	SessionID SessionID `json:"sessionId"`
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	Meta      Meta      `json:"_meta,omitempty"`
}

type WriteTextFileResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

type CreateTerminalRequest struct {
	SessionID       SessionID     `json:"sessionId"`
	Command         string        `json:"command"`
	Args            []string      `json:"args"`
	Cwd             *string       `json:"cwd,omitempty"`
	Env             []EnvVariable `json:"env"`
	OutputByteLimit *int          `json:"outputByteLimit,omitempty"`
	Meta            Meta          `json:"_meta,omitempty"`
}

type CreateTerminalResponse struct {
	TerminalID string `json:"terminalId"`
	Meta       Meta   `json:"_meta,omitempty"`
}

type TerminalOutputRequest struct {
	SessionID  SessionID `json:"sessionId"`
	TerminalID string    `json:"terminalId"`
	Meta       Meta      `json:"_meta,omitempty"`
}

type TerminalOutputResponse struct {
	Output     string              `json:"output"`
	ExitStatus *TerminalExitStatus `json:"exitStatus,omitempty"`
	Truncated  bool                `json:"truncated"`
	Meta       Meta                `json:"_meta,omitempty"`
}

type ReleaseTerminalRequest struct {
	SessionID  SessionID `json:"sessionId"`
	TerminalID string    `json:"terminalId"`
	Meta       Meta      `json:"_meta,omitempty"`
}

type ReleaseTerminalResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

type WaitForTerminalExitRequest struct {
	SessionID  SessionID `json:"sessionId"`
	TerminalID string    `json:"terminalId"`
	Meta       Meta      `json:"_meta,omitempty"`
}

type WaitForTerminalExitResponse struct {
	ExitCode *int    `json:"exitCode,omitempty"`
	Signal   *string `json:"signal,omitempty"`
	Meta     Meta    `json:"_meta,omitempty"`
}

type KillTerminalRequest struct {
	SessionID  SessionID `json:"sessionId"`
	TerminalID string    `json:"terminalId"`
	Meta       Meta      `json:"_meta,omitempty"`
}

type KillTerminalResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

// === Notification Params ===

type SessionNotification struct {
	SessionID SessionID     `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
	Meta      Meta          `json:"_meta,omitempty"`
}

// === Error Codes ===

const (
	ErrParse                  = -32700
	ErrInvalidRequest         = -32600
	ErrMethodNotFound         = -32601
	ErrInvalidParams          = -32602
	ErrInternal               = -32603
	ErrAuthenticationRequired = -32000
	ErrResourceNotFound       = -32002
)

// === Default Values ===

func DefaultClientCapabilities() ClientCapabilities {
	return ClientCapabilities{
		Fs: &FileSystemCapabilities{
			ReadTextFile:  false,
			WriteTextFile: false,
		},
		Terminal: false,
	}
}

func DefaultAgentCapabilities() AgentCapabilities {
	return AgentCapabilities{
		LoadSession: false,
		PromptCapabilities: PromptCapabilities{
			Image:           false,
			Audio:           false,
			EmbeddedContext: false,
		},
		McpCapabilities: McpCapabilities{
			HTTP: true,
			SSE:  true,
		},
		SessionCapabilities: SessionCapabilities{},
		Auth:                AgentAuthCapabilities{},
	}
}
