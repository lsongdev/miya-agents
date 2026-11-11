# Agent Loop Roadmap

Status: target design; steps 1 to 3 of [Order of Work](#order-of-work) are implemented

Updated: 2026-11-11

This note is the target architecture for the miya-agents runtime: package
layering, the model-facing message types, the session format, the event
stream, and context compaction. It replaces the earlier roadmap, whose
skill-guided compaction and dual `messages`/`events` session file are
superseded here (see [Superseded Decisions](#superseded-decisions)).

## Current Shape

- `llm` holds the neutral message, request and stream types, with one adapter
  per SDK. `agent.Agent` is built from plain fields and calls an `llm.Model`.
- `agent.Agent.Run` drives the loop and reports `Event` values to a `Sink`
  function, including real token usage after each model call.
- A session is an append-only JSONL log (see [Session Log](#session-log)).
  The model context and ACP replay are both derived from it. `session` no
  longer imports `acp`.
- `agent.Manager` both assembles agents from config and implements
  `acp.ServerHandler`.
- There is no context compaction. The earlier context maintenance notice has
  been removed; nothing replaces it until step 4.

## Problems

1. **The core depends on its transport.** `agent` implements the ACP server,
   so the loop cannot be used without the protocol package. (`session` no
   longer imports `acp`, since step 3.)
2. **OpenAI Chat was the implicit canonical model** and the persisted format.
   It cannot carry content blocks, images, thinking signatures, or cache
   hints. *Fixed by step 1*, except that `Manager.Prompt` still forwards only
   the text blocks of a prompt.
3. **A session had two sources of truth.** `messages` and `events` described
   the same conversation and had to be kept in step by hand. Compaction had
   to rewrite `messages`, which is why "who edits the file" had no good
   answer. *Fixed by step 3.*
4. **Compaction is delegated to the model.** A model editing the file that
   backs its own running session races with the next save, can split a tool
   call from its result, and depends on the model noticing a notice. The
   notice is a mid-conversation system message, which Anthropic Messages does
   not accept and which invalidates prompt caching.
5. **`EventSink` grew by breaking implementers.** Each new event type changed
   four implementations, `attach_file` was special-cased by tool name inside
   the loop, and `UsageEvent` carried nothing. *Fixed by step 2.*

## Target Layering

```text
openai/  anthropic/  sse/    wire SDKs; import nothing else from this module
        ^
llm/                         neutral Message / Request / stream types,
                             plus one adapter per SDK
        ^
session/                     append-only log and its projections
agent/                       loop, Tool, Event, compaction
tools/  skills/  mcp/        capabilities
        ^
runtime/                     config -> Agent assembly, session store, delegation
        ^
acp/                         protocol library (types, client, server transport)
acpserver/                   maps runtime calls and events onto ACP
        ^
cmd/miya/                    CLI (today: root main.go, readline.go, markdown.go)

proxy/                       separate product; depends on the wire SDKs only
```

Rules:

- Imports point downward only.
- `agent` and `session` import neither `acp` nor `config`. An `Agent` is built
  from plain options; `runtime` translates `config.ProfileConfig` into them.
- `openai/` and `anthropic/` stay faithful to their wire protocols and know
  nothing about agents. `anthropic/openai.go` still imports `openai` for the
  proxy's conversions; it moves under `proxy/` in step 5.
- `Tool` belongs to `agent`. Until `agent.Manager` moves to `runtime/`
  (step 5), `agent` imports `tools`, so the interface is declared in `tools`
  and aliased as `agent.Tool`.
- `miya-channels` and `miya-desktop` reach the runtime only through ACP, over
  stdio or `acp.DialInProcess`.

## Model Types (`llm`)

A small neutral model, shaped as content blocks so both protocols map onto it
without loss:

```go
type Message struct {
    Role    Role      // user, assistant, tool
    Content []Block
}

type Block struct {
    Type       BlockType // text, thinking, image, tool_call, tool_result
    Text       string
    Signature  string          // thinking signature, passed back verbatim
    ToolCall   *ToolCall
    ToolResult *ToolResult
    Media      *Media
}

type Request struct {
    Model     string
    System    string
    Messages  []Message
    Tools     []ToolDef
    MaxTokens int
}

// Model is the only capability the loop needs.
type Model interface {
    Stream(ctx context.Context, req *Request) (iter.Seq2[Delta, error])
}
```

The system prompt is a request field, not a message. Each stream ends with a
`Delta` carrying the stop reason and real token usage.

This is not the proxy's canonical codec. The proxy translates arbitrary client
requests between wire protocols; `llm` only has to express what this agent
sends and receives.

## Session Log

A session is an append-only JSONL file, `~/.miya/sessions/<id>.jsonl`. Each
line is one typed entry:

| Entry | Content |
| --- | --- |
| `meta` | agent name, title; later `meta` entries override the fields they set |
| `message` | one complete user, assistant or tool message. An assistant entry also carries the token usage of its model call; a tool entry also carries the files the tool handed to the user |
| `compaction` | summary, first kept entry id, token count before (step 4) |

Each entry has an `id` (its position in the log) and a `time`.

Entries are never rewritten or deleted. Two projections are derived from the
log:

- **Model context**: the system prompt, then the latest `compaction` summary,
  then every message entry from its first kept entry onward. A tool call
  left without a result by an interrupted turn is answered with a placeholder
  here, because providers reject unanswered calls.
- **Replay**: the full log, mapped to client updates by the transport
  (`acpserver` for ACP). Compactions do not shorten what the user sees.

Streaming deltas are not persisted; an entry is written to the file the
moment its message is complete, so there is no separate save step. A session
made with `session.New` has no file and lives only in memory, which is how
delegated tasks stay ephemeral. A partial last line left by a crash is
dropped on load.

Listing sessions scans each log for metadata without decoding message
bodies.

An existing `<id>.json` session is converted the first time it is listed or
loaded, and the original is kept as `<id>.json.bak`. Its recorded ACP event
list is not carried over, so files attached in old sessions no longer appear
in replay.

## Events

One event type and one function replace the interface:

```go
type Event struct {
    Type  EventType // text, thought, tool_start, tool_done, file,
                    // usage, session_info, compaction, done
    Text  string
    Tool  *ToolCall
    File  *File
    Usage *Usage
    // ...
}

type Sink func(Event) error
```

A sink handles the types it cares about and ignores the rest, so adding a
type (plan, permission request) breaks nobody. A tool that produces files
implements `FileTool` (`RunFiles` returns text for the model plus files for
the user), and the loop turns those into `file` events without knowing any
tool by name. Plain tools keep returning a string.

## Context Compaction

Compaction is a deterministic runtime step. The model is not asked to do it.

Before each model call the loop:

1. **Checks pressure.** Uses the real usage from the last response plus an
   estimate for entries added since. Compacts when the total crosses a
   threshold (default 80% of `contextWindowTokens`).
2. **Picks a cut point.** Keeps roughly the most recent N tokens and cuts only
   at a turn boundary, never between a tool call and its result.
3. **Summarizes.** Sends the entries before the cut, including the previous
   summary, in one plain model call with no tools. The summary model may be
   configured separately from the main model.
4. **Records it.** Appends a `compaction` entry. Nothing else in the log
   changes; only the model-context projection does.
5. **Tells the client.** Emits a `compaction` event.

If a provider still rejects a request as too long, the loop compacts and
retries once.

Manual compaction calls the same function,
`Compact(ctx, sess, instructions)`. The entry points are thin:

- CLI: `/compact [instructions]` in the REPL, `miya sessions compact <id>`.
- ACP: a slash command advertised through available commands.
- Channels: `/compact`, routed by the gateway like its other commands.

User guidance is kept, but as input rather than as the mechanism: the summary
prompt has a default and can be overridden by a file in the agent workspace.

Later, if needed: prune large old tool outputs in the projection before
resorting to a summary.

## Titles

The first user message remains the fallback title. A generated title is one
more plain model call after the first turn; it appends a `meta` entry and
emits `session_info`.

## Superseded Decisions

- *"Replay must not reconstruct UI history from model messages."* This held
  because OpenAI messages were too lossy to replay. Log entries are richer
  than those messages, so one log now serves both projections.
- *Skill-guided compaction through a context maintenance notice.* Replaced by
  runtime compaction. `agent/context_notice.go` is removed.
- *"The session file should remain editable JSON."* The log is still
  line-oriented JSON a person can read, but the runtime owns writes to it.

## Order of Work

1. Add `llm` types and the two SDK adapters; move `Tool` out of `openai/`;
   build `Agent` from plain options. **Done.**
2. Replace `EventSink` with `Event` and `Sink`; report real usage. **Done.**
3. Move sessions to the append-only log with projections; convert old files.
   **Done.**
4. Implement compaction and `/compact` on the new session model.
5. Split the ACP server out of `agent/manager.go` into `runtime/` and
   `acpserver/`; move the CLI to `cmd/miya/`.

Step 3 is the one that unlocks compaction.

## Notes

- Thought output stays opt-in per provider and client.
- Tool events carry raw input and output; clients show concise summaries by
  default.
- Tool calls within one assistant message may later run concurrently; the log
  format does not assume an order between their results.
