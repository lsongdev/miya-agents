---
name: miya-config
description: Help configure and safely edit the Miya ~/.miya/config.json file.
---

# Miya Config

Use this skill when the user wants to create, inspect, explain, or modify the Miya configuration file.

The main configuration file is `~/.miya/config.json`. Treat it as user-owned runtime configuration, not as repository source. Preserve unrelated fields and existing user choices whenever editing it.

## Configuration Shape

The root object may contain these fields:

```json
{
  "agents": [],
  "profiles": {},
  "providers": {},
  "mcpServers": {},
  "channels": [],
  "channelsEnabled": true,
  "tools": {},
  "logging": {}
}
```

`profiles` are the built-in agents used for session creation and grouping.
`agents` contains only external ACP endpoints such as OpenCode.

```json
{
  "id": "opencode",
  "name": "OpenCode",
  "enabled": true,
  "type": "stdio",
  "command": "opencode",
  "args": ["acp"],
  "url": "",
  "headers": {}
}
```

Valid agent `type` values are:

- `stdio`: start an ACP process with `command` and optional `args`.
- `http`: connect to an HTTP ACP endpoint with `url`.
- `sse`: connect to an SSE ACP endpoint with `url`.

`profiles` defines built-in agents and their model/runtime settings. The profile ID is also its Agent ID.

```json
{
  "provider": "openai",
  "model": "gpt-5",
  "workspace": "~/.miya/workspace",
  "maxTokens": 8192,
  "temperature": 0.95,
  "contextWindowTokens": 128000,
  "contextWarnRatio": 0.9
}
```

`providers` stores model provider connection settings.

```json
{
  "apiKey": "sk-...",
  "apiBase": "https://api.openai.com/v1",
  "type": "openai"
}
```

Provider `type` defaults to `openai` when omitted. Use `anthropic` for Anthropic-compatible provider entries.

`mcpServers` maps server names to MCP server definitions. Stdio servers use `command`, `args`, and optional `env`; remote servers use `url`, optional `headers`, and `type` set to `sse` or `streamable_http` when needed.

`channels` is an array of channel instances. Each item should include a stable instance id, a channel type, an enabled flag, and channel-specific options.

```json
{
  "id": "telegram-main",
  "type": "telegram",
  "enabled": true,
  "agent": "default",
  "responseLevel": "messages",
  "options": {}
}
```

`channelsEnabled` controls whether Miya Desktop starts the embedded channel service. Prefer treating it as a Desktop-local preference even though it currently lives in the shared config file.

`logging` controls runtime logs.

```json
{
  "enabled": true,
  "level": "info",
  "stdout": false,
  "file": "~/.miya/logs/miya.log"
}
```

## Editing Rules

1. Read the existing file before proposing or applying changes.
2. Keep valid JSON with two-space indentation.
3. Do not remove unknown fields unless the user explicitly asks.
4. Do not invent API keys, tokens, chat IDs, channel credentials, or local executable paths.
5. Never print full secrets back to the user. Redact values such as `apiKey`, tokens, passwords, and authorization headers.
6. Prefer stable IDs such as `default`, `telegram-main`, or `wechat-work` over display names that may change.
7. Use `~` for user-facing examples, but preserve the user's existing absolute paths when editing.
8. If a requested change affects running agents or channels, mention that the relevant service may need restart or reconnect.

## Minimal Examples

Basic built-in profile:

```json
{
  "agents": [],
  "profiles": {
    "default": {
      "provider": "openai",
      "model": "gpt-5",
      "workspace": "~/.miya/workspace",
      "maxTokens": 8192,
      "temperature": 0.95,
      "contextWindowTokens": 128000,
      "contextWarnRatio": 0.9
    }
  },
  "providers": {
    "openai": {
      "apiKey": "YOUR_API_KEY",
      "type": "openai"
    }
  },
  "mcpServers": {},
  "channels": []
}
```

External ACP agents:

```json
{
  "agents": [
    {
      "id": "remote-work",
      "name": "Remote Work Agent",
      "enabled": true,
      "type": "http",
      "url": "http://127.0.0.1:8765/acp"
    }
  ]
}
```

Channel instance bound to an agent:

```json
{
  "channelsEnabled": true,
  "channels": [
    {
      "id": "telegram-main",
      "type": "telegram",
      "enabled": true,
	  "agent": "default",
      "responseLevel": "messages",
      "options": {
        "botToken": "YOUR_TELEGRAM_BOT_TOKEN"
      }
    }
  ]
}
```

When uncertain about the current schema, inspect the Miya source types before editing. The source of truth is the `config.Config` structure in `miya-agents`, with channel instance details owned by `miya-channels`.
