---
title: Configuration
description: .mnemo/config.yaml and .mnemo/ignore.
---

Configuration is **two layers**, merged at load time (project overrides global), with strict parsing — unknown keys are rejected — on every layer.

## Machine-level: `~/.mnemo/config.yaml`

Written once by the first `mnemo init` (and never clobbered by later ones). Holds what is per-machine, not per-repo: the single shared database.

```yaml
database:
  type: sqlite                 # sqlite | postgres
  dsn: ~/.mnemo/mnemo.db       # one DB for all repos, partitioned by repository
```

The CLI has no login/signup flow and no client-side auth config. Browser/API auth is controlled by `mnemo serve` flags; see [Web UI and API](../web-ui-api/).

## Project-level: `<repo>/.mnemo/config.yaml`

Written by `mnemo init`. Holds the agent registry, contexts, privacy, and task decay. It carries **no `database` section** — the database is machine-level.

```yaml
agents:
  - name: claude
    kind: claude               # known kind → built-in repo-scoped discovery
    capabilities: [resume.cli, resume.stdin]
  - name: my-bot
    kind: custom
    parser: jsonl-openai       # required for custom agents
    sources: ["~/work/bot/logs/*.jsonl"]
    capabilities: [resume.file]

contexts:
  - { name: house-rules, type: file, path: ./AGENTS.md }

privacy:
  allow_cross_vendor_egress: false   # inject across vendors only if true
  share_metadata_to_team: false      # opt a repo into the PostgreSQL metadata backend
  allow_context_url_egress: false    # fetch url-type contexts only if true

tasks:
  cold_after: 336h             # decay window for non-pinned tasks (default 14d)

enrichment:
  enabled: false               # off by default — may send content to a model
```

Every section is optional; omitted values use safe defaults. Prefer `mnemo agents …` and `mnemo context …` over hand-editing the lists. See [Agents](../../concepts/sessions/) and [Contexts](../../concepts/contexts/).

- **`privacy.allow_cross_vendor_egress`** — permits configured render paths to target a different vendor. The simple CLI launcher form, such as `mnemo resume codex`, is treated as the explicit opt-in for that run.
- **`privacy.share_metadata_to_team`** — the per-repo participation flag for the PostgreSQL metadata backend. Even when true, the Postgres adapter stores metadata + compiled state only; raw transcript content and absolute paths never leave the local store.
- **`privacy.allow_context_url_egress`** — permits `url`-type contexts to be fetched over the network. Off by default; a `url` context resolves to a placeholder until this is true.
- **`tasks.cold_after`** — a Go duration string (e.g. `336h`). A non-pinned task idle longer than this is cold: it stops surfacing and is auto-paused by `mnemo watch`. Invalid/empty falls back to the 14-day default — decay can't be accidentally disabled.
- **`enrichment.*`** — optional LLM refinement of the compiled state of play. Disabled by default; see [Enrichment](#enrichment) below.

## .mnemo/ignore

Opt sessions out of ingestion entirely:

```text
# bare known tool name → skip that whole tool
codex

# glob → matched against the session file name
*-scratch.jsonl

# glob with "/" → matched against the full path
sessions/throwaway/*
```

Blank lines and `#` comments are ignored. A missing file means "ingest everything". See [Session ingestion](../../concepts/sessions/).

## Enrichment

State-of-play compilation is deterministic and offline by default (see [State of play](../../concepts/state-of-play/)). Enrichment is an optional pass that asks a configured model to refine that compiled state before it is saved. It is **disabled by default** because enabling it sends task/session content to the configured endpoint — and is therefore a privacy boundary, covered in [Privacy and safety](../../concepts/privacy/).

```yaml
enrichment:
  enabled: true
  provider: openai_compatible   # see provider table below
  base_url: http://localhost:1234/v1
  model: qwen2.5-coder
  api_key_env: MNEMO_LLM_API_KEY # env var to read the key from; optional for local
  timeout: 20s
  max_events: 80                 # newest N events sent
  max_event_chars: 2400          # per-event truncation
  max_input_chars: 60000         # total prompt cap
  max_output_tokens: 1600
  temperature: 0
```

`provider`, `model`, and a reachable `base_url` are the only required keys when `enabled: true`; everything else falls back to the defaults shown above. Cloud providers also require their API key env var to be set, or `mnemo` errors before any compile calls out.

| `provider` | Default `base_url` | API key env |
| --- | --- | --- |
| `openai` | `https://api.openai.com/v1` | `OPENAI_API_KEY` (required) |
| `anthropic` | `https://api.anthropic.com/v1` | `ANTHROPIC_API_KEY` (required) |
| `openrouter` | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` (required) |
| `ollama` | `http://localhost:11434` | none |
| `lmstudio` | `http://localhost:1234/v1` | none |
| `localai` | `http://localhost:8080/v1` | none |
| `openai_compatible` | *(none — `base_url` required)* | optional |

`openai_compatible` speaks the standard chat-completions API and works with llama.cpp servers and similar local endpoints. Secrets are scrubbed from both the prompt and the model's output, and if the provider errors or times out, compilation silently keeps the deterministic result — enrichment can never block or corrupt a handoff.

## Backends

SQLite is local-first and holds full transcripts. PostgreSQL is metadata-only by construction — see [Privacy and safety](../../concepts/privacy/) and [Storage](../../reference/storage/).
