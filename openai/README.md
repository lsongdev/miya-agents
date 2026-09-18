# OpenAI Package

This package provides a Go client for OpenAI-compatible APIs. It covers the
Chat Completions API (non-streaming and streaming), the Embeddings API, and the
Responses API request/response shapes, including bidirectional conversions
between the two formats. It is the format consumed by the agent loop.

## Client Setup

```go
client, err := openai.NewClient(&openai.Configuration{
    API:    "https://api.openai.com/v1",
    APIKey: os.Getenv("OPENAI_API_KEY"),
})
if err != nil {
    log.Fatal(err)
}
```

API endpoints can be absolute or relative to the configured base URL (e.g.
`/chat/completions`). Any OpenAI-compatible base URL works, so the same client
can target providers such as DeepSeek.

To override the default `Authorization: Bearer <key>` header (e.g. custom
authentication), use `SetHeaders`:

```go
client.SetHeaders(func() (map[string]string, error) {
    token, err := refreshToken()
    if err != nil {
        return nil, err
    }
    return map[string]string{"Authorization": "Bearer " + token}, nil
})
```

Customize the transport with `SetHTTPClient`. Use `NewRequest`/`Do`/
`MakeRequest` for raw requests when you need to preserve upstream status codes,
headers, and bodies.

## Chat Completions

Message constructors:

```go
messages := []openai.ChatCompletionMessage{
    openai.SystemMessage("You are a terse assistant."),
    openai.UserMessage("What is the capital of France?"),
}
```

Non-streaming:

```go
resp, err := client.CreateChatCompletion(ctx, &openai.ChatCompletionRequest{
    Model:    openai.GPT4o,
    Messages: messages,
})
if err != nil {
    log.Fatal(err)
}

message := resp.GetFirstChoice().Message
fmt.Println(message.Content)

// reasoning models surface their reasoning via ReasoningContent
fmt.Println(message.ReasoningContent)
```

Streaming — `CreateChatCompletionStream` sets `stream: true` internally (the
input request is not mutated) and returns a `<-chan openai.ChatCompletionResponse`:

```go
ch, err := client.CreateChatCompletionStream(ctx, &openai.ChatCompletionRequest{
    Model:    "deepseek-chat",
    Messages: messages,
})
if err != nil {
    log.Fatal(err)
}

for chunk := range ch {
    if chunk.Error != nil {
        log.Fatal(chunk.Error.Message)
    }
    if msg := chunk.GetMessage(); msg != nil {
        fmt.Print(msg.Content)
    }
}
```

The channel closes when the stream finishes or the context is canceled.

## Embeddings

```go
resp, err := client.CreateEmbeddings(ctx, &openai.EmbeddingRequest{
    Input: "hello world",
    Model: "text-embedding-ada-002",
})
if err != nil {
    log.Fatal(err)
}
for _, data := range resp.Data {
    fmt.Println(data.Embedding)
}
```

## Tools

Implements OpenAI function calling. A `Tool` provides a `Def()` describing the
function (`ToolDef`/`FunctionDef`, a JSON Schema in `Parameters`) and a `Run`
method executed by the agent loop when the model requests it.

Model tool calls arrive as `ChatCompletionMessage.ToolCalls`. Feed results back
with:

- `AssistantMessageWithTools(content, toolCalls)` — assistant message carrying
  the tool requests;
- `ToolResultMessage(toolCallID, name, content)` — the result for one call.

## Streaming Helpers

- `MessageBuilder` accumulates delta chunks into a complete
  `ChatCompletionMessage` (including reasoning content and tool calls keyed by
  stream index or call id). Use `Update` per delta and `Build` for the result.
- `ResponseAssembler` accumulates `ChatCompletionResponse` chunks into a single
  final response. `AssembleFromChunks(ch)` consumes a channel and returns the
  assembled response.

## Responses API

The package models the `/v1/responses` API (`ResponseRequest`,
`ResponseObject`, `ResponseOutputItem`, etc.) and converts it to and from the
Chat Completions shape:

- `NewChatCompletionRequestFromResponseRequest` — a Responses request becomes an
  equivalent `ChatCompletionRequest`, translating message items, function calls,
  and function call outputs into roles/tool calls.
- `NewResponseObjectFromChatCompletionResponse` — a chat completion response
  becomes a `ResponseObject` with message and `function_call` output items.

For streaming to clients expecting the Responses API:

- `ConvertChatCompletionStreamToResponsesStream` consumes an OpenAI
  `ChatCompletionResponse` stream and writes `response.created`,
  `output_item.added`, `output_text.delta`, `function_call_arguments.delta`,
  and `response.completed` SSE events to an `http.ResponseWriter`. It returns
  the assembled final response.

A simple streaming SSE example:

```go
ch, _ := client.CreateChatCompletionStream(ctx, req)
final := openai.ConvertChatCompletionStreamToResponsesStream(nil, ch, w)
```

## Response Helpers

- `ChatCompletionResponse.GetFirstChoice()` — first choice (or `nil`).
- `ChatCompletionResponse.GetMessage()` — the message from the first choice,
  preferring `Delta` over `Message` for streaming chunks.
- `ChatCompletionMessage.IsEmpty()` / `.HasToolCall()` — useful when filtering
  no-op stream chunks.
- `openai.NewChatCompletionResponse(id, model, content, reasoning)` — builds a
  complete response, convenient when wrapping a non-OpenAI backend.

## Models

```go
models, err := client.Models()
```

returns the list of available models.

## Types

- `Configuration` — API base URL and key.
- `ChatCompletionRequest` / `ChatCompletionResponse` / `ChatCompletionMessage` /
  `ChatCompletionChoice` — chat.completions payloads.
- `ToolDef` / `FunctionDef` / `ToolCall` / `FunctionCall` — function calling.
- `EmbeddingRequest` / `EmbeddingResponse` / `Embedding` — embeddings payloads.
- `CompletionUsage` / `Usage` — token usage, including deepseek-reasoner
  cache/cached and reasoning token fields.
- `Error` — API error payload, also surfaced in stream chunks.
- Responses API types — `ResponseRequest`, `ResponseObject`,
  `ResponseInput`/`ResponseInputItem`, `ResponseOutputItem`,
  `ResponseContentPart`, `ResponsesEvent`, and more.