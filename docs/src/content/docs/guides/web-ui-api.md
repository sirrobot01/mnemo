---
title: Web UI and API
description: The local task-timeline UI and HTTP API.
---

```bash
mnemo serve
```

Starts a local server (default `127.0.0.1:47321`). It binds to localhost only.

- `/` — the task-timeline UI
- `/v1` — the JSON API
- `--addr` — change the listen address
- `--api-only` — skip the UI
- `--auth=false` — disable browser/API auth for a private local session
- `--allow-signup=false` — keep auth on but require existing users
- `--token-ttl` — token lifetime, default `720h`
- Ctrl-C — clean shutdown

## The UI

First visit creates or signs into a browser account, then shows the task timeline:

- left: the task list (with an **Ingest** button)
- selecting a task shows its current **state of play** (goal, done, next, rejected, decisions, open questions, files, unconfirmed hypotheses)
- below it, the merged **session event timeline** for that task

It is an inspector, not an editor — the CLI drives changes.

No CLI login is required. The UI stores its bearer token in browser storage and sends it to `/v1`.

## The API

The same services the CLI uses, over HTTP:

```text
GET    /v1/health          → {ok, repository, auth_required, allow_signup}
POST   /v1/auth/signup     {"email", "password"}     (when signup is allowed)
POST   /v1/auth/login      {"email", "password"}     → {token, expires_at}
POST   /v1/auth/logout
GET    /v1/db/status
GET    /v1/tasks
GET    /v1/tasks/{id}
GET    /v1/tasks/{id}/state
GET    /v1/tasks/{id}/sessions
GET    /v1/sessions/{id}/events
POST   /v1/ingest
POST   /v1/resume          {"task_id"?, "tool"?, "allow_cross_vendor"?}
DELETE /v1/sessions/{id}
```

When auth is enabled, `/v1/health`, `/v1/auth/signup`, and `/v1/auth/login` are public. `/v1/auth/logout` and every other `/v1` route require `Authorization: Bearer <token>`.

Errors map to status codes: not-found / no-active-task → 404, cross-vendor egress refused → 403, malformed JSON → 400.

See the [HTTP API reference](../../reference/api/) for details.

## MCP

For agents that speak MCP rather than HTTP, `mnemo mcp` exposes a read + ingest tool surface over the same services. When served over HTTP it reuses the bearer tokens this server issues. See the [MCP server](../mcp/) guide.
