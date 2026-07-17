# ACP Package

This package implements Miya's Go bindings for the Agent Client Protocol (ACP).
It provides both sides of the protocol:

- a client SDK for applications that want to talk to an ACP agent;
- a server SDK for agent runtimes that want to expose ACP over stdio or in-process pipes.

Protocol reference: <https://agentclientprotocol.com/llms-full.txt>

## Transport Model

ACP messages are JSON-RPC 2.0 frames separated by newlines.

The package supports two connection modes:

```go
client, err := acp.DialStdio("miya", "acp")
```

Use `DialStdio` when the agent runs as a subprocess.

```go
client := acp.DialInProcess(handler)
```

Use `DialInProcess` when embedding an agent runtime in the same process, such
as a desktop app. This avoids process management but still preserves the ACP
JSON-RPC boundary.

For custom transports, use:

```go
client := acp.NewClient(stdinWriter, stdoutReader)
```

## Client Usage

A typical client flow is:

1. connect to an ACP agent;
2. register a notification handler for streaming updates;
3. initialize the protocol;
4. create or load a session;
5. send prompts;
6. close or delete sessions when needed.

```go
client, err := acp.DialStdio("miya", "acp")
if err != nil {
    return err
}
defer client.Close()

client.OnNotification(acp.NewNotificationHandler(&MyNotificationReceiver{}))

client.OnRequest(&MyClientHandler{})

init, err := client.Initialize(&acp.InitializeRequest{
    ProtocolVersion:    1,
    ClientCapabilities: acp.DefaultClientCapabilities(),
    ClientInfo:         &acp.Implementation{Name: "my-client", Version: "1.0.0"},
})
if err != nil {
    return err
}
_ = init

sess, err := client.NewSession(&acp.NewSessionRequest{
    Cwd: "/Users/me/project",
})
if err != nil {
    return err
}

resp, err := client.Prompt(&acp.PromptRequest{
    SessionID: sess.SessionID,
    Prompt: []acp.ContentBlock{
        {Type: "text", Text: "Summarize this repository."},
    },
})
if err != nil {
    return err
}
fmt.Println(resp.StopReason)
```

## Client SDK Calls

These methods live on `acp.Client`. They are the calls an ACP client sends to an
agent server. The agent server handles them by implementing `acp.ServerHandler`.

High-level request helpers:

```go
Initialize(*InitializeRequest) (*InitializeResponse, error)
Authenticate(*AuthenticateRequest) (*AuthenticateResponse, error)
NewSession(*NewSessionRequest) (*NewSessionResponse, error)
Prompt(*PromptRequest) (*PromptResponse, error)
LoadSession(*LoadSessionRequest) (*LoadSessionResponse, error)
ResumeSession(*ResumeSessionRequest) (*ResumeSessionResponse, error)
CloseSession(*CloseSessionRequest) (*CloseSessionResponse, error)
DeleteSession(*DeleteSessionRequest) (*DeleteSessionResponse, error)
ListSessions(*ListSessionsRequest) (*ListSessionsResponse, error)
SetSessionMode(*SetSessionModeRequest) (*SetSessionModeResponse, error)
SetSessionConfigOption(*SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
Logout(*LogoutRequest) (*LogoutResponse, error)
CancelSession(SessionID) error
```

Low-level helpers:

```go
Call(method string, params, result any) error
SendNotification(method string, params any) error
OnNotification(func(method string, params json.RawMessage))
OnRequest(ClientHandler)
ReceiveSessionUpdates(ctx any) <-chan SessionUpdate
Close() error
```

Use `Call` and `SendNotification` for protocol extensions or methods that do
not yet have typed helpers.

`OnNotification` intentionally keeps the raw JSON-RPC callback shape. Use
`NewNotificationHandler` or `DispatchNotification` when you want ACP
notifications parsed and routed for you.

## Notification Helpers

Implement `NotificationReceiver` to handle parsed notifications without
manually checking method names and unmarshalling JSON in every client:

```go
type MyNotificationReceiver struct {
    acp.DefaultNotificationReceiver
}

func (r *MyNotificationReceiver) AgentMessageChunk(sessionID acp.SessionID, content acp.ContentBlock, messageID *acp.MessageID) {
    if content.Type == "text" {
        fmt.Print(content.Text)
    }
}

func (r *MyNotificationReceiver) AgentThoughtChunk(sessionID acp.SessionID, thought string, content acp.ContentBlock) {
    fmt.Printf("[thought] %s\n", thought)
}

func (r *MyNotificationReceiver) ToolCall(sessionID acp.SessionID, toolCall *acp.ToolCall) {
    fmt.Printf("[%s] %s (%s)\n", toolCall.Kind, toolCall.Title, toolCall.Status)
}
```

`DefaultNotificationReceiver` provides no-op methods, so your receiver only
needs to override the events it cares about.

Variant callbacks:

```go
UserMessageChunk(sessionID, content, messageID)
AgentMessageChunk(sessionID, content, messageID)
AgentThoughtChunk(sessionID, thought, content)
ToolCall(sessionID, toolCall)
ToolCallUpdate(sessionID, update)
Plan(sessionID, plan)
AvailableCommandsUpdate(sessionID, update)
CurrentModeUpdate(sessionID, update)
ConfigOptionUpdate(sessionID, update)
SessionInfoUpdate(sessionID, update)
UsageUpdate(sessionID, update)
UnknownSessionUpdate(notification)
```

For custom wiring, call the dispatcher directly:

```go
client.OnNotification(func(method string, params json.RawMessage) {
    acp.DispatchNotification(receiver, method, params)
})
```

For the common path:

```go
client.OnNotification(acp.NewNotificationHandler(&MyNotificationReceiver{}))
```

## Sessions

Sessions are the unit of conversation and workspace state.

Use `NewSession` to create a fresh session:

```go
sess, err := client.NewSession(&acp.NewSessionRequest{
    Cwd: "/Users/me/project",
    McpServers: []acp.McpServer{},
    AdditionalDirectories: []string{
        "/Users/me/shared",
    },
})
```

Use `LoadSession` to attach to an existing persisted session:

```go
_, err := client.LoadSession(&acp.LoadSessionRequest{
    SessionID: "abc123",
    Cwd:       "/Users/me/project",
})
```

Use `ListSessions` to discover sessions. Responses include session metadata
such as `sessionId`, `cwd`, `title`, `updatedAt`, and additional directories.

## Prompting

Prompt input is a list of content blocks:

```go
_, err := client.Prompt(&acp.PromptRequest{
    SessionID: sess.SessionID,
    Prompt: []acp.ContentBlock{
        {Type: "text", Text: "What changed in this repo?"},
    },
})
```

`Prompt` waits for the final `PromptResponse`. User-visible output is streamed
separately through `session/update` notifications while the prompt is running.

To cancel an active prompt:

```go
err := client.CancelSession(sess.SessionID)
```

The agent should observe the prompt context cancellation and return a response
with `StopReason: acp.StopCancelled` when possible.

## Streaming Updates

Agents stream session activity as `session/update` notifications.

Main update types:

- `user_message_chunk`: user message content.
- `agent_message_chunk`: assistant message content.
- `agent_thought_chunk`: thought or reasoning content.
- `tool_call`: a new tool call snapshot.
- `tool_call_update`: incremental tool call changes.
- `plan`: current plan entries.
- `available_commands_update`: command menu updates.
- `current_mode_update`: active mode changed.
- `config_option_update`: session config options changed.
- `session_info_update`: title or metadata changed.
- `usage_update`: context or cost usage changed.

Example text handler:

```go
func handleUpdate(sessionID acp.SessionID, update acp.SessionUpdate) {
    switch update.SessionUpdate {
    case "agent_message_chunk":
        if update.Content.Type == "text" {
            fmt.Print(update.Content.Text)
        }
    case "tool_call":
        fmt.Printf("tool: %s\n", update.ToolCall.Title)
    case "usage_update":
        fmt.Printf("usage: %d/%d\n", update.Usage.Used, update.Usage.Size)
    }
}
```

## Content Blocks and Files

`ContentBlock` is used for prompt input and streamed output.

Common fields:

```go
type ContentBlock struct {
    Type        string
    Text        string
    Data        string
    MimeType    string
    URI         *string
    Name        string
    Description *string
    Size        *int
    Title       *string
}
```

Recommended usage:

- `Type: "text"` with `Text` for plain text or Markdown.
- `Type: "image"` with `Data` and `MimeType` for small inline images.
- `Type: "resource"` or `Type: "resource_link"` with `URI` for larger files.
- Include `Name`, `Title`, `Description`, and `Size` when available.

Example file output:

```go
uri := "file:///Users/me/.miya/workspace/report.png"
sender.Send(acp.SessionUpdate{
    SessionUpdate: "agent_message_chunk",
    Content: acp.ContentBlock{
        Type:     "image",
        URI:      &uri,
        MimeType: "image/png",
        Name:     "report.png",
    },
})
```

## Tool Calls

Use `tool_call` for a full tool call snapshot and `tool_call_update` for
incremental changes.

```go
sender.Send(acp.SessionUpdate{
    SessionUpdate: "tool_call",
    ToolCall: &acp.ToolCall{
        ToolCallID: "tc-1",
        Title:      "Read README",
        Kind:       acp.ToolKindRead,
        Status:     acp.ToolCallCompleted,
        Content: []acp.ToolCallContent{
            {
                Type:    "content",
                Content: &acp.ContentBlock{Type: "text", Text: "README contents..."},
            },
        },
    },
})
```

Known tool kinds:

```text
read, edit, delete, move, search, execute, think, fetch, switch_mode, other
```

Known statuses:

```text
pending, in_progress, completed, failed
```

## Implementing an Agent

Implement `acp.ServerHandler` to expose an agent runtime.

`ServerHandler` is the server-side contract for requests sent by `acp.Client`:

```go
Initialize(ctx context.Context, req *InitializeRequest) (*InitializeResponse, error)
Authenticate(ctx context.Context, req *AuthenticateRequest) (*AuthenticateResponse, error)
NewSession(ctx context.Context, req *NewSessionRequest, sender SessionUpdateSender) (*NewSessionResponse, error)
Prompt(ctx context.Context, req *PromptRequest, sender SessionUpdateSender) (*PromptResponse, error)
LoadSession(ctx context.Context, req *LoadSessionRequest, sender SessionUpdateSender) (*LoadSessionResponse, error)
ResumeSession(ctx context.Context, req *ResumeSessionRequest) (*ResumeSessionResponse, error)
CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error)
DeleteSession(ctx context.Context, req *DeleteSessionRequest) (*DeleteSessionResponse, error)
ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error)
SetSessionMode(ctx context.Context, req *SetSessionModeRequest) (*SetSessionModeResponse, error)
SetSessionConfigOption(ctx context.Context, req *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
Logout(ctx context.Context, req *LogoutRequest) (*LogoutResponse, error)
```

```go
type Agent struct{}

func (a *Agent) Initialize(ctx context.Context, req *acp.InitializeRequest) (*acp.InitializeResponse, error) {
    return &acp.InitializeResponse{
        ProtocolVersion:   1,
        AgentCapabilities: acp.DefaultAgentCapabilities(),
        AuthMethods:       []acp.AuthMethod{},
        AgentInfo:         &acp.Implementation{Name: "my-agent", Version: "0.1.0"},
    }, nil
}

func (a *Agent) NewSession(ctx context.Context, req *acp.NewSessionRequest, sender acp.SessionUpdateSender) (*acp.NewSessionResponse, error) {
    return &acp.NewSessionResponse{SessionID: acp.SessionID("session-1")}, nil
}

func (a *Agent) Prompt(ctx context.Context, req *acp.PromptRequest, sender acp.SessionUpdateSender) (*acp.PromptResponse, error) {
    sender.Send(acp.SessionUpdate{
        SessionUpdate: "agent_message_chunk",
        Content:       acp.ContentBlock{Type: "text", Text: "Hello from agent."},
    })
    return &acp.PromptResponse{StopReason: acp.StopEndTurn}, nil
}
```

Then serve it:

```go
func main() {
    server := acp.NewServer(&Agent{})
    if err := server.Serve(); err != nil {
        log.Fatal(err)
    }
}
```

For tests or embedded applications:

```go
client := acp.DialInProcess(&Agent{})
```

## Server Behavior

`NewServer` reads JSON-RPC requests from stdin and writes responses and
notifications to stdout. Each request is handled in a goroutine. Prompt requests
receive a cancellable context; `session/cancel` cancels the active prompt for
that session.

The server currently dispatches these agent methods:

```text
initialize
authenticate
session/new
session/prompt
session/load
session/resume
session/close
session/delete
session/list
session/set_mode
session/set_config_option
logout
```

## ClientHandler Methods

`ClientHandler` is the opposite direction: these are methods the agent server
can call on the client over the same ACP connection. Register a `ClientHandler`
on the client:

```go
type MyClientHandler struct {
    acp.DefaultClientHandler
}

func (h *MyClientHandler) ReadTextFile(ctx context.Context, req *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
    data, err := os.ReadFile(req.Path)
    if err != nil {
        return nil, err
    }
    return &acp.ReadTextFileResponse{Content: string(data)}, nil
}

client.OnRequest(&MyClientHandler{})
```

`DefaultClientHandler` returns a protocol error for methods you do not
override, so clients can implement only the capabilities they advertise.

Supported client-side methods:

```text
session/request_permission
fs/read_text_file
fs/write_text_file
terminal/create
terminal/output
terminal/release
terminal/wait_for_exit
terminal/kill
```

Associated request/response types:

- `RequestPermissionRequest`
- `RequestPermissionResponse`
- `ReadTextFileRequest`
- `ReadTextFileResponse`
- `WriteTextFileRequest`
- `WriteTextFileResponse`
- `CreateTerminalRequest`
- `CreateTerminalResponse`
- `TerminalOutputRequest`
- `TerminalOutputResponse`
- `ReleaseTerminalRequest`
- `ReleaseTerminalResponse`
- `WaitForTerminalExitRequest`
- `WaitForTerminalExitResponse`
- `KillTerminalRequest`
- `KillTerminalResponse`

Agent handlers access these methods through the server-provided sender:

```go
func (a *Agent) Prompt(ctx context.Context, req *acp.PromptRequest, sender acp.SessionUpdateSender) (*acp.PromptResponse, error) {
    client, ok := acp.ClientFromSender(sender)
    if !ok {
        return nil, fmt.Errorf("client calls are unavailable")
    }

    file, err := client.ReadTextFile(ctx, &acp.ReadTextFileRequest{
        SessionID: req.SessionID,
        Path:      "/Users/me/project/README.md",
    })
    if err != nil {
        return nil, err
    }

    sender.Send(acp.SessionUpdate{
        SessionUpdate: "agent_message_chunk",
        Content:       acp.ContentBlock{Type: "text", Text: file.Content},
    })

    return &acp.PromptResponse{StopReason: acp.StopEndTurn}, nil
}
```

## Implementing a Client

A client usually combines two pieces:

- a `NotificationReceiver` for events the agent streams to the client;
- a `ClientHandler` for capabilities the agent may request from the client.

```go
type MyClient struct {
    acp.DefaultNotificationReceiver
    acp.DefaultClientHandler
}

func (c *MyClient) AgentMessageChunk(sessionID acp.SessionID, content acp.ContentBlock, messageID *acp.MessageID) {
    if content.Type == "text" {
        fmt.Print(content.Text)
    }
}

func (c *MyClient) ToolCall(sessionID acp.SessionID, toolCall *acp.ToolCall) {
    fmt.Printf("tool: %s\n", toolCall.Title)
}

func (c *MyClient) UsageUpdate(sessionID acp.SessionID, usage *acp.UsageUpdate) {
    fmt.Printf("usage: %d/%d\n", usage.Used, usage.Size)
}

func (c *MyClient) ReadTextFile(ctx context.Context, req *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
    data, err := os.ReadFile(req.Path)
    if err != nil {
        return nil, err
    }
    return &acp.ReadTextFileResponse{Content: string(data)}, nil
}

clientRuntime := &MyClient{}
client.OnNotification(acp.NewNotificationHandler(clientRuntime))
client.OnRequest(clientRuntime)
```

This lets the application receive agent messages, thoughts, tool calls, plans,
usage updates, and capability requests without manually decoding JSON-RPC
notifications in application code.

## Examples

See:

- `examples/acp/main.go`: in-process echo agent and client.
- `examples/acp/agent/main.go`: stdio ACP agent.
- `examples/acp/repl/main.go`: interactive ACP client.
