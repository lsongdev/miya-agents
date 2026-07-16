# Agent Loop Roadmap

This note tracks the next miya-agents architecture changes needed for a richer ACP client experience.

## Current Shape

The current loop is intentionally simple:

- `agent.Agent.RunAgentLoop` emits structured events through `EventSink`.
- Tool calls are executed after the assistant message is fully assembled.
- Tool execution is emitted as structured events for transports such as ACP and CLI.
- Session files persist OpenAI chat messages for model context and ACP session events for UI replay.
- ACP replay sends the persisted event log in order.

This works for basic structured replay. The remaining gaps are richer event types, user-provided maintenance guidance, and better generated metadata quality.

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

Long-running sessions need a way to reduce old context while keeping task continuity. miya-agents keeps runtime behavior minimal: when the estimated context window is nearly full, it appends a short system notice with the session id/path so the next model turn can compact the session JSON using user-provided guidance.

### Session Titles

Using the session ID as the visible title is poor UX. The current fallback is the first user message; richer title and summary updates can be handled by user-provided skills or prompts that edit session metadata.

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

Add compaction as a skill-guided session file operation.

Initial implementation:

1. Runtime estimates current message size against the configured context window.
2. If the remaining window is below the warning threshold, append one short maintenance notice to `messages`.
3. The notice includes the session id and `~/.miya/sessions/<id>.json`.
4. User-provided maintenance guidance describes how to compact `messages`, update `summary/title/compactions`, and leave `events` unchanged.

Current and potential entry points:

- Context pressure notice appended by runtime.
- User-provided skill or prompt invoked by the model.
- Future CLI command: `miya sessions compact <id>`
- ACP method or command if the protocol surface supports it later

Avoid silently rewriting active sessions without recording the compaction.

## Title And Summary

Short term:

- Return first user message as `SessionInfo.title` when no explicit title exists.
- Store `title` and `updated_at` in session metadata.

Medium term:

- Generate a concise title after the first assistant turn.
- Emit `session_info_update` so clients update the row immediately.
- Save the generated title to the session file.

Current approach:

- Runtime does not provide dedicated metadata tools.
- Skills document the exact JSON edits for `title`, `summary`, `messages`, and `compactions`.
- Skills must preserve recent active context and never mutate `events`.

## Suggested Order

1. Add session metadata fields: `title`, `updated_at`, `summary`. Done.
2. Return title/updatedAt from `session/list`. Done.
3. Persist replayable session events. Done.
4. Add context pressure notice. Done.
5. Add optional user-facing maintenance guidance.
6. Add manual compaction command if the skill-based flow is not enough.

## Notes

- Thought/reasoning should be opt-in per provider and UI, because not every model exposes safe or useful reasoning text.
- Tool call events should carry raw input/output for debugging, but clients should display concise summaries by default.
- The session file should remain editable JSON, but code should provide safe helpers for metadata and compaction updates.
