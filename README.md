# Mnemo

**Use one task across multiple coding agents.**

Mnemo runs next to your repo. You keep using Claude, Codex, Aider, Continue,
Copilot, Cursor, Windsurf/Devin, or a custom agent as usual; Mnemo reads their
local session logs, groups the work into tasks, and prints a short handoff for
the next agent.

## Quick Start

```bash
go install github.com/sirrobot01/mnemo/cmd/mnemo@latest

cd path/to/your/repo
mnemo init

# optional: see which supported agents have local session logs
mnemo agents detect

# after you have used an agent in this repo
mnemo ingest

# make the current work explicit
mnemo task start "fix auth flow" --goal "Preserve the failing case and next fix"

# print the handoff
mnemo resume

# or open the next agent with the handoff as its first prompt
mnemo resume claude
mnemo resume codex
mnemo resume aider
mnemo resume continue
mnemo resume copilot
mnemo resume cursor
mnemo resume windsurf
```

## Daily Use

1. Start or continue work in any supported coding agent.
2. Run `mnemo ingest` to import new local transcripts.
3. Run `mnemo status` or `mnemo task list` to see the active task.
4. Run `mnemo resume` before switching tools.
5. Paste the handoff, or let `mnemo resume <agent>` launch the next agent for
   you.

For a browser view:

```bash
mnemo serve
```

Open the printed local URL. The UI lets you sign in, ingest sessions, create or
select tasks, inspect the timeline, render a resume, and copy it.

For continuous updates while you work:

```bash
mnemo watch
```

## Configuration

`mnemo init` writes:

- `~/.mnemo/config.yaml` for the local SQLite database.
- `.mnemo/config.yaml` inside the repo for agents, contexts, privacy, and task
  settings.

Known agents are registered by default. Limit them with:

```bash
mnemo init --agents codex,claude,cursor
mnemo agents list
mnemo agents add aider
```

Add read-only repo context when the next agent should always see it:

```bash
mnemo context add rules --type file --path AGENTS.md
mnemo context show
```

Mnemo is local-first. Fresh installs use SQLite, do not call external model
providers, and secret-scan transcript and context content before storing or
rendering it. Optional LLM enrichment is off until configured.

## Documentation

Full documentation lives at:

**https://sirrobot01.github.io/mnemo**

## Status

Mnemo is under active development. The current focus is making the first-run
workflow and cross-agent handoff reliable.
