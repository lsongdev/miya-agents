# Agent Loop Roadmap

This note tracks the next miya-agents architecture changes needed for a richer ACP client experience.

## Current Shape

The current loop is intentionally simple:

- `agent.Agent.RunAgentLoop` streams assistant text through `Writer.Write(string, done)`.
- Tool calls are executed after the assistant message is fully assembled.
- Tool execution is rendered back as markdown text.
- Session files persist OpenAI chat messages only.
- ACP replay reconstructs only user and assistant text chunks.

This works for basic chat, but it cannot faithfully represent structured agent activity such as thoughts, tool calls, plans, or session metadata updates.

## Problem Areas

### Structured Output

Clients such as opencode ACP distinguish:

- assistant text
- thought/reasoning chunks
- tool call start/update/result
- plan updates
- usage/context updates
- session info updates

miya-agents currently collapses tool activity into markdown text. That loses structure for desktop UI, channel adapters, replay, and future analytics.

### Session History

Session files only contain `[]openai.ChatCompletionMessage`. This is sufficient for model context, but not sufficient for UI replay. If the runtime emits structured events, those events need to be stored separately from model messages.

### Context Compaction

Long-running sessions need a way to reduce old context while keeping task continuity. Editing the session file is a reasonable implementation detail, but it should be exposed through a clear agent operation rather than ad hoc external mutation.

### Session Titles

Using the session ID as the visible title is poor UX. The short-term fallback should be the first user message. The long-term solution should be generated session summaries/title metadata.

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

The old `Writer` can be kept as a compatibility adapter during migration.

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
- `title` is short and user-facing.
- `summary` is the latest durable conversation summary.
- `compactions` records what was replaced and when.

Keep backward compatibility when loading old sessions by treating missing fields as empty.

## Context Compaction

Add compaction as an explicit session operation.

Initial implementation:

1. Select older messages beyond a configurable recent window.
2. Ask the configured model to summarize durable facts, decisions, files touched, open tasks, and user preferences.
3. Replace the selected message range with one synthetic system/developer message containing the summary.
4. Store the summary in `session.summary`.
5. Append a compaction record for auditability.

Potential entry points:

- CLI command: `miya sessions compact <id>`
- ACP method or command if the protocol surface supports it later
- Internal tool: `compact_context`
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

Long term:

- Add a `summarize_session` operation that updates both `title` and `summary`.
- Use this same operation for compaction and session list metadata.

This should be an agent operation, not a normal user-visible tool by default. Exposing it as a tool risks the model changing metadata unpredictably during ordinary tasks. A controlled internal operation can still use the same provider/client infrastructure.

## Suggested Order

1. Add session metadata fields: `title`, `updated_at`, `summary`.
2. Return title/updatedAt from `session/list`.
3. Introduce `EventSink` and adapt existing `Writer`.
4. Emit structured ACP tool call events around local tool execution.
5. Persist replayable session events.
6. Add title generation after first turn.
7. Add manual compaction command.
8. Add automatic compaction threshold.

## Notes

- Thought/reasoning should be opt-in per provider and UI, because not every model exposes safe or useful reasoning text.
- Tool call events should carry raw input/output for debugging, but clients should display concise summaries by default.
- The session file should remain editable JSON, but code should provide safe helpers for metadata and compaction updates.
