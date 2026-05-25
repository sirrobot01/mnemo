---
title: Quick start
description: From zero to a working cross-agent handoff.
---

## 1. Initialize

```bash
cd your-repo
mnemo init --agents claude codex
```

Resolves the **git root** (run it from any subdirectory — same project either way), ensures the global database and machine config exist under the Mnemo home, writes `.mnemo/config.yaml` with your agent registry, and registers the repository by a stable identity (git remote, else git-root path). Omit `--agents` to register every known agent.

## 2. Work in an agent

Use Claude Code (or Codex) normally on a task. Mnemo reads its transcript — nothing to export.

## 3. Capture and resume

**One-shot hook** (recommended before switching agents):

```bash
mnemo ingest && mnemo resume
```

`ingest` sweeps every configured agent and threads new sessions; `resume` prints the state of play.

**Background:**

```bash
mnemo watch
```

Keeps ingesting, threading, recompiling, and decaying as you work.

## 4. Switch agents without losing your place

```bash
mnemo resume codex
```

Codex opens already knowing the goal, what's done, what was rejected (and why), the next steps, and the files in play — plus any [contexts](../../concepts/contexts/) you configured.

```bash
mnemo resume codex --print   # inspect the handoff without launching
```

## 5. Add standing knowledge (optional)

```bash
mnemo context add house-rules --type file --path ./AGENTS.md
mnemo context show
```

Every resume now carries your house rules, scrubbed and read-only.

## 6. Inspect

```bash
mnemo status            # active task + latest state version
mnemo task list         # all tasks
mnemo agents list       # configured agents
mnemo serve             # task-timeline UI at http://127.0.0.1:47321
```

## Optional: pin a task

```bash
mnemo task start "fix the auth race"
```

Every new session attaches to it regardless of branch. Pause or finish it to release the pin.

Next: [CLI workflow](../../guides/cli/) · [Configuration](../../guides/configuration/) · [Web UI and API](../../guides/web-ui-api/).
