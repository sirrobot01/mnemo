---
title: HTTP API
description: The local /v1 surface served by mnemo serve.
---

`mnemo serve` binds the API to localhost (default `127.0.0.1:47321`). All routes are under `/v1`. JSON in, JSON out.

Auth is enabled by default for the browser UI and API. `GET /v1/health`, `POST /v1/auth/signup`, and `POST /v1/auth/login` are public. `POST /v1/auth/logout` and every other `/v1` route require `Authorization: Bearer <token>` unless the server was started with `--auth=false`.

## Routes

```text
GET    /v1/health                  → {ok, repository, auth_required, allow_signup}
POST   /v1/auth/signup             → {id, email}
POST   /v1/auth/login              → {token, expires_at}
POST   /v1/auth/logout             → {status}
GET    /v1/db/status               → {backend, applied, pending}
GET    /v1/tasks                   → [Task]            (threads first)
POST   /v1/tasks                   → Task
GET    /v1/tasks/{id}              → Task
DELETE /v1/tasks/{id}              → {forgot}
GET    /v1/tasks/{id}/state        → WorkingState      (latest)
GET    /v1/tasks/{id}/sessions     → [session id]
POST   /v1/tasks/{id}/switch       → Task
POST   /v1/tasks/{id}/pause        → Task
POST   /v1/tasks/{id}/done         → Task
GET    /v1/sessions/{id}/events    → [SessionEvent]
POST   /v1/ingest                  → [ImportResult]    (ingest + thread)
POST   /v1/resume                  → {tool, content}
DELETE /v1/sessions/{id}           → {forgot}
```

### POST /v1/resume body

```json
{ "task_id": "", "tool": "", "allow_cross_vendor": false }
```

`task_id` empty → the most-recently-active non-cold task. `tool` empty → local render (never gated). A different-vendor `tool` without `allow_cross_vendor` returns 403.

## Status codes

| Code | When |
|------|------|
| 200 | success |
| 400 | malformed JSON body |
| 401 | missing, invalid, or expired bearer token |
| 403 | cross-vendor egress refused |
| 404 | task/session not found, or no active task |
| 500 | unexpected error |

## Notes

The API uses the same services as the CLI. `GET /v1/tasks` and the resume/ingest routes thread freshly-ingested sessions first, so results reflect the latest on-disk state. The shape of `Task`, `WorkingState`, and `SessionEvent` is in [Storage](../storage/).
