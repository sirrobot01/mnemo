---
title: Status
description: Current implementation state and remaining hardening.
---

Mnemo is a working cross-agent task-continuity tool.

## Implemented

- **Local CLI workflow.** `init`, `agents`, `context`, `ingest`, `watch`, `task`, `resume`, `status`, `forget`, `serve`, `db`, and `version`.
- **One machine-level database.** The default store is one SQLite database under the Mnemo home, partitioned by stable repository identity.
- **Layered config.** Machine config holds database settings. Project config holds agents, contexts, privacy, task decay, and enrichment.
- **Agent registry.** Known agents include Claude, Codex, Aider, Continue, Copilot, Cursor, and Windsurf. Custom agents can declare parser, source globs, and capabilities.
- **Task threading.** Sessions attach to active or pinned tasks, with branch and idle-window heuristics for unpinned work.
- **State of play.** Mnemo compiles goal, done work, rejected approaches, decisions, next steps, files, open questions, and hypotheses.
- **Resume handoff.** `mnemo resume` prints the handoff; supported built-ins can be launched with `mnemo resume <agent>`.
- **Contexts.** File, directory, URL, and context-reference entries can be appended to each handoff after secret scanning.
- **Web UI and API.** `mnemo serve` exposes the task timeline and `/v1` API with browser/API auth enabled by default.
- **Privacy controls.** Secret scanning runs before persistence and before render. URL context fetches, cross-vendor API rendering, and LLM enrichment are explicit opt-ins.

## Current Limits

- The local CLI workflow is SQLite-only.
- PostgreSQL is metadata-only and validated by an env-gated integration test, not part of default CI.
- Web/API auth is intentionally small: users, password hashes, and bearer tokens. There is no org/RBAC model.
- Cursor and Windsurf/Devin ingestion use bounded structured extraction because their public docs do not expose a stable transcript schema.
- Windows is not yet validated.
