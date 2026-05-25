---
title: Mnemo
description: Cross-agent memory for AI coding. Switch tools without losing your place.
template: splash
hero:
  tagline: Use one task across multiple coding agents.
  actions:
    - text: Understand Mnemo
      link: /start/understanding-mnemo/
      icon: open-book
      variant: primary
    - text: Quick start
      link: /start/quick-start/
      icon: right-arrow
    - text: Install
      link: /start/install/
      icon: setting
---

Mnemo is a local tool you run from a repository when you want Claude, Codex, Aider, Continue, Copilot, Cursor, Windsurf/Devin, or another configured agent to pick up the same piece of work without a long re-explanation.

## How You Use It

```bash
go install github.com/sirrobot01/mnemo/cmd/mnemo@latest
cd path/to/repo
mnemo init
mnemo agents detect
mnemo ingest
mnemo task start "fix auth flow" --goal "Preserve the failing case and next fix"
mnemo resume
mnemo resume claude
mnemo resume aider
mnemo resume cursor
```

That is the normal loop:

1. Work in an agent.
2. Run `mnemo ingest`.
3. Keep the active task explicit with `mnemo task start`, `mnemo task switch`, `mnemo task pause`, or `mnemo task done`.
4. Run `mnemo resume` before switching tools.
5. Paste the handoff, or launch a supported CLI directly with it.

## Browser Workflow

```bash
mnemo serve
```

Open the local URL printed by the command. The UI asks you to sign in, then lets you ingest, create or select tasks, review the session timeline, render a handoff, and copy it.

## Useful Setup

```bash
mnemo context add rules --type file --path AGENTS.md
mnemo watch
```

Contexts add read-only repo knowledge to each handoff. `mnemo watch` keeps ingestion and compiled state fresh while you work.

Mnemo is local-first by default: SQLite storage, no model calls unless enrichment is configured, and secret scanning before content is stored or rendered.
