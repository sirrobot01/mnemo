---
title: Install
description: Build the mnemo binary.
---

Mnemo is a single Go binary. It needs Go (CGO enabled — the SQLite driver requires it) and runs on macOS and Linux. Windows is not yet validated.

## From source

```bash
git clone <your-mnemo-remote> mnemo
cd mnemo
go build -o mnemo ./cmd/mnemo
```

Put the resulting `mnemo` binary on your `PATH`.

## Verify

```bash
mnemo version
mnemo --help
```

You should see `init`, `agents`, `context`, `ingest`, `watch`, `task`, `resume`, `status`, `forget`, `serve`, `db`, and `version`.

## The Mnemo home

Mnemo keeps one global database and machine config under a single home:

- `$MNEMO_HOME` if set, else
- `$XDG_DATA_HOME/mnemo` if set, else
- `~/.mnemo`

That directory holds `mnemo.db` (the single database shared across all your projects, partitioned by repository) and `config.yaml` (machine-level database settings). Each repository additionally has its own `.mnemo/config.yaml` for agents, contexts, privacy, and task settings.

## Requirements at a glance

- No external services. A fresh install uses one local SQLite database and makes no network calls.
- No CLI account setup. Browser/API authentication belongs to `mnemo serve` and happens in the web UI.

Next: [Quick start](../quick-start/).
