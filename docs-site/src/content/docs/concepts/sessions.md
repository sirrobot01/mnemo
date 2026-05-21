---
title: Agents and ingestion
description: How Mnemo discovers agent transcripts via the configured agent registry, idempotently and with secret scanning.
---

A **session** is one ingested agent transcript. Mnemo never asks you to save or export anything; it reads the files agents already write to disk. The **agent registry** in your project config controls which agents Mnemo reads.

## The agent registry

`mnemo init --agents codex claude` writes an `agents:` list into `.mnemo/config.yaml`. Each agent has a `name` (project-unique), a `kind` (the transcript parser), optional `sources` (globs that override discovery), and `capabilities`.

| Kind (built-in) | Default discovery | `mnemo watch`? | `mnemo resume <agent>` |
|------|--------|----------------|------------------------|
| `claude` | `~/.claude/projects/<encoded-repo>/<uuid>.jsonl` | yes | `claude <handoff>` |
| `codex` | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` | yes | `codex <handoff>` |
| `aider` | `<repo>/.aider.chat.history.md` | yes | `aider --message <handoff>` |
| `continue` | `~/.continue/sessions/*.json` | yes | `cn -p <handoff>` |
| `copilot` | `~/.copilot/session-state` | yes | `copilot -i <handoff>` |
| `cursor` | `~/.cursor/agent` | yes | `cursor-agent <handoff>` |
| `windsurf` | `~/.devin` and `~/.config/devin` | yes | `devin -- <handoff>` |

Known agents carry **no explicit `sources`** by default: discovery delegates to the built-in provider, which knows the tool's layout and scopes results to *this* repository. (A naive `~/.codex/**` glob would ingest every repo's sessions — the built-in provider matches by working directory instead.) Cursor and Windsurf/Devin do not publish a stable transcript schema, so Mnemo uses bounded structured extraction and only imports candidates that mention the current repository path.

Manage the registry without hand-editing YAML:

```bash
mnemo agents list           # configured agents + kinds + capabilities
mnemo agents detect         # which known agents are present on this machine
mnemo agents add my-bot --kind custom --parser jsonl-openai \
  --source '~/work/bot/logs/*.jsonl' --cap resume.file
mnemo agents remove my-bot
```

## Custom agents

Anything not in the built-in table is a **custom agent**. It must declare a `parser` (`jsonl`, `jsonl-openai`, or `jsonl-anthropic` — newline-delimited `{role, content}` records) and explicit `sources`. Source globs support `~` and the `{repo}` token (which expands to the encoded projects dir for Claude, the repo root otherwise) and a single recursive `**` segment.

## Capabilities

Each agent declares capability tags (`resume.cli`, `resume.stdin`, `resume.file`, …). Claude, Codex, Cursor, Copilot, and Windsurf/Devin receive the handoff through their CLI prompt entrypoints. Aider and Continue receive it through documented one-shot prompt flags.

## Ingest vs. watch

- `mnemo ingest` — one-shot sweep of every configured agent. Use `mnemo ingest && mnemo resume` as a synchronous hook right before launching the next agent.
- `mnemo watch` — long-running; tails the agents' directories (built-in watch dirs or the base dirs of explicit `sources`) and re-ingests on change. One failing agent never aborts the others.

## Idempotency

Session and event IDs are deterministic (derived from **agent name** + source path + sequence). Re-ingesting the same transcript upserts the session and ignores events that already exist, so a growing transcript appends without duplicating. A session also records its `kind` — the vendor used for cross-vendor egress checks.

## Secret scanning

Every event runs through the secret scanner **before** it is persisted. A matching event is dropped, not stored. If every event in a session is scrubbed, the session is recorded with `status = redacted`. See [Privacy and safety](../privacy/).

## Opting out: `.mnemo/ignore`

A `.mnemo/ignore` file in the repo excludes sessions from ingestion. A bare name matches an agent **by name or by kind**:

```text
# skip the agent named (or any agent of kind) "codex"
codex

# a glob matches the session file name
*-experiment.jsonl

# a glob with "/" matches the full path
sessions/scratch/*
```

Blank lines and `#` comments are ignored. Skipped sessions are counted and reported by `mnemo ingest`.
