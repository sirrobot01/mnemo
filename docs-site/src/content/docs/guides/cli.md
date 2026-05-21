---
title: CLI workflow
description: The day-to-day commands for cross-agent continuity.
---

## The core loop

```bash
mnemo init                       # once per repo
mnemo ingest                     # read new agent sessions, thread them
mnemo resume                     # print the state of play
mnemo resume codex               # launch Codex with the state of play
mnemo resume claude              # launch Claude with the state of play
mnemo resume aider               # run Aider with the state of play
mnemo resume continue            # run Continue CLI with the state of play
mnemo resume copilot             # run GitHub Copilot CLI with the state of play
mnemo resume cursor              # launch Cursor agent with the state of play
mnemo resume windsurf            # run Devin/Windsurf with the state of play
```

`mnemo ingest && mnemo resume` is the synchronous hook when you only want the rendered handoff text. To switch agents directly, pass the agent name:

```bash
mnemo resume codex
mnemo resume aider
mnemo resume cursor
```

## Continuous mode

```bash
mnemo watch
```

Tails enabled adapter directories; on change it re-ingests, re-threads, recompiles non-done tasks, and decays cold ones. `--debounce-ms` controls the quiet period (default 750). Ctrl-C stops cleanly. One failing adapter does not stop the loop.

A sweep that imports or redacts sessions logs one line per affected agent (with `agent`, `kind`, and counts) at info level; no-op sweeps stay silent. Raise `--log-level info` to see them.

## Tasks

```bash
mnemo task list
mnemo task show <id>
mnemo task start "<title>" [--goal <g>] [--branch <b>]   # pins the task
mnemo task switch <id>                                    # pins the task
mnemo task pause <id>
mnemo task done <id>
mnemo status
```

`list`, `status`, and `resume` thread freshly-ingested sessions first, so a task appears as soon as its sessions are ingested. A pinned task captures all new sessions regardless of branch until you pause/finish it.

## Resume options

```bash
mnemo resume [agent] --task <id> --print --write
```

- positional agent — launch that CLI with the rendered handoff. Supported built-ins are `claude`, `codex`, `aider`, `continue`, `copilot`, `cursor`, and `windsurf`.
- `--task` — a specific task instead of the most-recently-active.
- `--print` — render the handoff instead of launching the agent.
- `--write` — managed block under `.mnemo/` instead of stdout.

## Forget

```bash
mnemo forget <session-id>      # delete a session + its events
mnemo forget --task <task-id>  # delete a task, its working states + links
```

Source transcripts on disk are never touched.

## Output format

Every command supports `--format json` for scripting, and `--root <path>` to operate on a repo other than the working directory.
