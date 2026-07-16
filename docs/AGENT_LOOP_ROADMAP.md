# Agent Loop Roadmap

This note tracks the next miya-agents architecture changes needed for a richer ACP client experience.

## Current Shape

The current loop is intentionally simple:

- `agent.Agent.RunAgentLoop` emits structured events through `EventSink`.
- Tool calls are executed after the assistant message is fully assembled.
- Tool execution is emitted as structured events for transports such as ACP and CLI.
- Session files persist OpenAI chat messages for model context and ACP session events for UI replay.
- ACP replay sends the persisted event log in order.

This works for basic structured replay. The remaining gaps are richer event types, automatic compaction thresholds, and better generated metadata quality.

## Problem Areas

### Structured Output

Clients such as opencode ACP distinguish:

- assistant text
- thought/reasoning chunks
- tool call start/update/result
- plan updates
- usage/context updates
- session info updates

miya-agents now emits structured tool activity at runtime and records ACP-shaped session updates for exact replay. The next gap is broadening the event vocabulary to plan updates, richer usage information, permissions, and future channel-specific metadata.

### Session History

Session files now keep `[]openai.ChatCompletionMessage` and `[]session.Event` separately. `messages` remains the mutable model context. `events` is the append-only display protocol history used by ACP replay.

### Context Compaction

Long-running sessions need a way to reduce old context while keeping task continuity. miya-agents exposes this as controlled internal tools, so the model can decide when session maintenance is useful while the runtime still owns the actual mutation boundaries.

### Session Titles

Using the session ID as the visible title is poor UX. The current fallback is the first user message, and `rename_session` lets the model update the title once the goal is clear.

## Target Architecture

Introduce a structured event sink between the agent loop and transports.

```go
type EventSink interface {
    UserMessage(text string) error
    AssistantDelta(text string) error
    ThoughtDelta(text string) error
    ToolCallStart(call ToolCallEvent) error
    ToolCallUpdate(update ToolCallUpdateEvent) error
    ToolCallDone(update ToolCallUpdateEvent) error
    PlanUpdate(plan PlanEvent) error
    Usage(update UsageEvent) error
    SessionInfo(update SessionInfoEvent) error
    Done(stopReason string) error
}
```

ACP, CLI stdout, and future channels should each adapt this event stream to their output format:

- ACP sink maps events to `session/update`.
- CLI sink renders concise terminal text.
- Channel sinks can chunk/format content safely.
- A recording sink persists display events in the session file.

The old string-only `Writer` path has been removed. New transports should implement `EventSink` directly.

## Session File Evolution

Extend session files from model-only history to model history plus UI/event metadata.

```json
{
  "id": "...",
  "agent_name": "default",
  "title": "Implement ACP session replay",
  "summary": "...",
  "created_at": "...",
  "updated_at": "...",
  "messages": [],
  "events": [],
  "compactions": []
}
```

Guidelines:

- `messages` remains the source for model context.
- `events` is the source for ACP/UI replay.
- Replay must not reconstruct UI history from model messages.
- `title` is short and user-facing.
- `summary` is the latest durable conversation summary.
- `compactions` records what was replaced and when.

## Context Compaction

Add compaction as an explicit internal session operation.

Initial implementation:

1. Select older messages beyond a configurable recent window.
2. Ask the configured model to summarize durable facts, decisions, files touched, open tasks, and user preferences.
3. Replace the selected message range with one synthetic system/developer message containing the summary.
4. Store the summary in `session.summary`.
5. Append a compaction record for auditability.

Current and potential entry points:

- Internal tool: `compact_context`
- Internal tool: `summarize_session`
- Internal tool: `rename_session`
- Future CLI command: `miya sessions compact <id>`
- ACP method or command if the protocol surface supports it later
- Automatic threshold: compact when estimated token usage exceeds a configured limit

Avoid silently rewriting active sessions without recording the compaction.

## Title And Summary

Short term:

- Return first user message as `SessionInfo.title` when no explicit title exists.
- Store `title` and `updated_at` in session metadata.

Medium term:

- Generate a concise title after the first assistant turn.
- Emit `session_info_update` so clients update the row immediately.
- Save the generated title to the session file.

Current internal tools:

- `rename_session` updates `session.title`; the agent loop emits and records a replayable `session_info_update` when it observes the title change.
- `summarize_session` calls a summarizer profile when configured, falling back to the current profile, then updates `session.summary`.
- `compact_context` summarizes older messages, replaces them with a synthetic system summary, updates `session.summary`, and appends a compaction audit record.

These are model-callable tools, but they are intentionally narrow. The model provides intent and summary content through a summarizer agent; the runtime decides the exact message range, preserves recent active tool calls, and never mutates `events`.

## Suggested Order

1. Add session metadata fields: `title`, `updated_at`, `summary`. Done.
2. Return title/updatedAt from `session/list`. Done.
3. Persist replayable session events. Done.
4. Add controlled internal tools for title, summary, and compaction. Done.
5. Add manual compaction command.
6. Add automatic compaction threshold.

## Notes

- Thought/reasoning should be opt-in per provider and UI, because not every model exposes safe or useful reasoning text.
- Tool call events should carry raw input/output for debugging, but clients should display concise summaries by default.
- The session file should remain editable JSON, but code should provide safe helpers for metadata and compaction updates.
