# Anthropic Package

This package provides a Go client for the Anthropic Messages API. It supports
non-streaming and streaming requests, tool use, and model listing. It depends
on nothing else in this module; the agent loop reaches it through the `llm`
package's adapter.

## Client Setup

```go
client := anthropic.NewClient(&anthropic.Configuration{
    API:    "https://api.anthropic.com",
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
})
```

API endpoints can be relative to the configured base URL (e.g. `/v1/messages`)
or absolute. The `v1/` prefix is collapsed when the base already ends in
`/v1`.

To override the default `x-api-key` authentication (e.g. OAuth bearer tokens),
set a per-request header provider with `SetHeaders`:

```go
client.SetHeaders(func() (map[string]string, error) {
    token, err := refreshToken()
    if err != nil {
        return nil, err
    }
    return map[string]string{"Authorization": "Bearer " + token}, nil
})
```

Customize the transport with `SetHTTPClient`:

```go
client.SetHTTPClient(&http.Client{Timeout: 30 * time.Second})
```

`NewClient` returns a plain `*Client` (no error). Use `NewRequest`/`Do` for
raw, uninterpreted requests when you need to preserve upstream status codes,
headers, and bodies.

## Non-Streaming Messages

```go
resp, err := client.CreateMessage(ctx, &anthropic.Request{
    Model:     "claude-3-5-sonnet-latest",
    MaxTokens: 1024,
    Messages: []anthropic.Message{
        anthropic.TextMessage("user", "Explain the memex."),
    },
})
if err != nil {
    log.Fatal(err)
}

for _, block := range resp.Content {
    if block.Type == "text" {
        fmt.Println(block.Text)
    }
}
fmt.Println("stop reason:", resp.StopReason)
```

A `Request` may include a `System` prompt, `Tools` (name, description, JSON
schema), sampling parameters (`Temperature`, `TopP`), and `StopSequences`.

Messages carry either plain text (`Content`) or structured `Blocks`. Use
`TextMessage(role, text)` for the common case, or set `Blocks` for `thinking`,
`tool_use`, and `tool_result` content blocks.

## Streaming Messages

`CreateMessageStream` sets `stream: true` internally (the input request is not
mutated) and returns a `MessageStream` whose `Events` channel yields Anthropic
SSE events:

```go
stream, err := client.CreateMessageStream(ctx, req)
if err != nil {
    log.Fatal(err)
}

for event := range stream.Events {
    switch event.Type {
    case "content_block_delta":
        var delta anthropic.Delta
        _ = json.Unmarshal(event.Delta, &delta)
        if delta.Type == "text_delta" {
            fmt.Print(delta.Text)
        }
    case "message_stop":
        return
    case "error":
        log.Fatal(event.Error)
    }
}
```

The stream ends when `Events` closes; the context can be canceled to stop early.

## Models

```go
models, err := client.Models(ctx)
```

returns the list of available model IDs.

## Server-Side SSE

`anthropic.ResponseWriter` writes Anthropic-format SSE events to an
`http.ResponseWriter`:

```go
rw := anthropic.NewResponseWriter(w)
rw.SendMessageStart(id, "message", "assistant", model)
rw.SendContentBlockStart(0, "text")
rw.SendContentBlockDelta(0, anthropic.Delta{Type: "text_delta", Text: "Hello"})
rw.SendContentBlockStop(0)
rw.SendMessageDelta("end_turn", nil, usage)
rw.SendMessageStop()
```

Events are emitted as `event: <type>\ndata: <json>\n\n` frames, matching the
`MessageStream` reader above.

## Types

- `Request` / `Response` — Messages API request and response.
- `Message` / `ContentBlock` — conversational messages and content blocks
  (`text`, `thinking`, `redacted_thinking`, `tool_use`, `tool_result`).
- `Tool` — client tool available to the model.
- `Event` / `Delta` / `MessageStart` — streaming event types.
- `Usage` — input/output token counts.
- `APIError` — API error payload.