---
title: Commands
description: Full mnemo CLI surface.
---

Global flags: `--root <path>`, `--format human|json`, `--log-level`, `--log-format`.

| Command | Purpose |
|---------|---------|
| `mnemo init` | Create `.mnemo/`, migrate, register the repo |
| `mnemo ingest` | One-shot sweep of enabled adapters (secret-scanned, idempotent) |
| `mnemo watch` | Long-running: ingest + thread + recompile + decay on change |
| `mnemo task list` | List tasks |
| `mnemo task show <id>` | Show one task |
| `mnemo task start "<title>"` | Start + pin a task (`--goal`, `--branch`) |
| `mnemo task switch <id>` | Make a task active + pinned |
| `mnemo task pause <id>` | Pause a task (releases pin) |
| `mnemo task done <id>` | Finish a task (terminal, releases pin) |
| `mnemo resume` | Compile + emit the state of play |
| `mnemo resume claude` | Launch Claude with the state of play |
| `mnemo resume codex` | Launch Codex with the state of play |
| `mnemo resume aider` | Run Aider with the state of play |
| `mnemo resume continue` | Run Continue CLI with the state of play |
| `mnemo resume copilot` | Run GitHub Copilot CLI with the state of play |
| `mnemo resume cursor` | Launch Cursor agent with the state of play |
| `mnemo resume windsurf` | Run Devin/Windsurf with the state of play |
| `mnemo status` | Active task + latest state version |
| `mnemo forget <session-id>` | Delete a session and its events |
| `mnemo forget --task <id>` | Delete a task, its working states + links |
| `mnemo serve` | Local task-timeline UI + `/v1` API |
| `mnemo mcp` | MCP server (stdio, or Streamable HTTP) — read + ingest |
| `mnemo db migrate` | Apply pending migrations |
| `mnemo db status` | Applied / pending migrations |
| `mnemo version` | Version |

## resume flags

```text
--task <id>             a specific task (default: most-recently-active)
--print                 render instead of launching the selected agent
--write                 managed block under .mnemo/ instead of stdout
```

## watch flags

```text
--debounce-ms <n>       quiet period before recompiling (default 750)
```

## serve flags

```text
--addr <host:port>      HTTP listen address (default 127.0.0.1:47321)
--api-only              serve only the /v1 API
--auth                  require browser/API login (default true)
--allow-signup          allow browser signup when auth is enabled (default true)
--token-ttl <duration>  browser/API auth token lifetime (default 720h)
```

## mcp flags

```text
--http <host:port>      serve Streamable HTTP here instead of stdio (empty = stdio)
--auth                  require a bearer token when serving over HTTP (default true)
```

Tools: `mnemo_resume`, `mnemo_list_tasks`, `mnemo_task_state`, `mnemo_ingest`.
See the [MCP server](../../guides/mcp/) guide.
