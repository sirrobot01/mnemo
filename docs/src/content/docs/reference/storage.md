---
title: Storage
description: Entities, tables, and the two backends.
---

One storage abstraction, two adapters. SQLite is the local default; PostgreSQL is metadata-only by construction.

## Entities

```text
Repository      a registered local repo
Session         one ingested agent transcript
SessionEvent    one normalized event in a session
Task            an in-progress unit of work (the continuity unit)
WorkingState    a versioned, compiled state of play for a task
Setting         local key/value (reserved)
```

`Session ──* SessionEvent`. `Task ──* Session` (via `task_sessions`). `Task ──* WorkingState` versions (latest wins).

## Tables

```text
repositories
sessions
session_events
tasks            (incl. pinned)
task_sessions
working_states
settings
```

Migrations are embedded SQL applied with go-migrate. SQLite is used by the local CLI workflow; PostgreSQL is available as a metadata-only backend.

## Key types

```text
SessionKind      claude | codex | aider | continue | copilot | cursor | windsurf | custom parser kind
SessionStatus    ingested | ignored | redacted
TaskStatus       active | paused | done        (done is terminal)
```

`WorkingState` carries `goal, done[], in_progress, next_steps[],
rejected[{approach,reason}], decisions[{decision,rationale}],
open_questions[], files_touched[{path,summary}],
hypotheses[{claim,confidence,confirmed}]`, plus `version` and a
`source_watermark` (last event consumed).

## Identity and idempotency

- Sessions: deterministic ID from tool + source path → re-ingest upserts.
- Events: deterministic ID from session + sequence → re-ingest is insert-or-ignore (growing transcripts append without duplicating).
- Tasks: random ID (user/threading created).

## Backends

- **SQLite** — local-first, holds full transcripts. Timestamps as RFC3339 text.
- **PostgreSQL** — metadata-only. The adapter strips absolute source paths and drops raw event content + structured payloads at the persistence boundary. Only metadata and the compiled, already-scrubbed `working_states` are stored. Build-validated; see [Privacy and safety](../../concepts/privacy/).
