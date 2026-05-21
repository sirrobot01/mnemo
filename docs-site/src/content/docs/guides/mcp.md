---
title: MCP server
description: Expose Mnemo's continuity surface to MCP-aware agents.
---

```bash
mnemo mcp
```

Runs Mnemo as a [Model Context Protocol](https://modelcontextprotocol.io) server so an agent can pull its own continuity instead of you running `mnemo resume` and pasting. It is a transport over the same services the CLI and HTTP API use — no new behavior, just another caller.

Scope is **read + ingest**. Task mutation (start/switch/pause/done) is intentionally not exposed; drive those from the CLI.

## Tools

| Tool | Input | Backed by |
|------|-------|-----------|
| `mnemo_resume` | `task_id?`, `tool?`, `allow_cross_vendor?` | Compile + render the state of play (defaults to the most-recently-active task) |
| `mnemo_list_tasks` | — | Tasks after threading freshly-ingested sessions |
| `mnemo_task_state` | `task_id` | Latest compiled working state, unrendered |
| `mnemo_ingest` | — | Sweep adapters, ingest new/changed transcripts, re-thread |

`mnemo_resume` honors the same cross-vendor egress gate as `mnemo resume`: rendering for an agent whose vendor is not among the task's session sources requires `allow_cross_vendor: true` or `privacy.allow_cross_vendor_egress` in config. Pass the calling agent as `tool` (e.g. `claude`, `codex`).

## stdio (default)

The default transport. The agent client launches `mnemo mcp` as a subprocess and talks over stdin/stdout — no port, no auth, nothing on the network. Register it from inside the repo (or pass `--root`):

```json
{ "mcpServers": { "mnemo": { "command": "mnemo", "args": ["mcp"] } } }
```

## Streamable HTTP (opt-in)

For a remote or multi-client setup, serve the Streamable HTTP transport instead (the current MCP remote transport; the older HTTP+SSE transport is deprecated):

```bash
mnemo mcp --http 127.0.0.1:47422              # auth on (default)
mnemo mcp --http 127.0.0.1:47422 --auth=false # open; logs a warning
```

- The MCP endpoint is served at `/`; `GET /healthz` is an unauthenticated liveness probe.
- With auth on, every other request needs `Authorization: Bearer <token>`. Tokens are the same ones `mnemo serve` issues — both share the repository's auth store — so mint one via `mnemo serve` (there is no login endpoint on the MCP surface).
- `--http` empty (the default) keeps stdio. There is no default address; you must supply one, so nothing ever listens by accident.
- Ctrl-C shuts down cleanly.

See the [Web UI and API](../web-ui-api/) guide for the HTTP surface that mints those tokens.
