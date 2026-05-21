---
title: Privacy and safety
description: The two secret-scan boundaries, cross-vendor egress, web/API auth, and forget.
---

Mnemo continuously vacuums full transcripts and can re-inject them into a different vendor's agent. That makes privacy a first-class feature, not an afterthought.

## Secret scanning on two boundaries

1. **Ingest** — every event runs through the secret scanner before persistence. A match is dropped; a fully-scrubbed session is marked `redacted`.
2. **Resume render** — the rendered state of play is scanned again; offending lines become `[REDACTED]`.

## Cross-vendor egress is explicit

The simple CLI form is the explicit opt-in:

```bash
mnemo resume codex
mnemo resume claude
```

For API callers, a transcript captured from one vendor's agent is not rendered for another vendor's agent until `allow_cross_vendor` is set on the request. See [Resume](../resume/).

## Local-first, and PostgreSQL is metadata-only

SQLite is the local default and holds everything locally. The PostgreSQL backend is **metadata-only by construction**: the adapter strips absolute source paths and drops raw event content and structured payloads at the persistence boundary. Only session/task metadata and the compiled, already-scrubbed working state are stored there — never raw transcripts. This is enforced in the adapter itself, so a misconfigured surface cannot leak.

`privacy.share_metadata_to_team` is the per-repo participation flag for PostgreSQL metadata storage; even when true, raw content never leaves the local store.

## Web/API auth does not relax any boundary

The CLI has no account setup and no client auth config. `mnemo serve` gates the browser UI and HTTP API behind bearer-token auth by default, with signup/login handled in the web UI or `/v1/auth/*`. Auth changes access to the web surface only; it does not change what Mnemo stores or what data can leave the process. Passwords are stored only as PBKDF2 hashes; tokens expire and are revocable.

## URL contexts are opt-in egress

A `url`-type [context](contexts/) is fetched over the network only when `privacy.allow_context_url_egress` is true; otherwise it resolves to a placeholder. All resolved context content — file, dir, or url — is secret-scanned before it can reach the handoff.

## Enrichment egress is opt-in

Optional [enrichment](../../guides/configuration/#enrichment) sends task/session content to a configured model endpoint to refine the state of play. It is **disabled by default**; nothing is sent until you set `enrichment.enabled: true`. The content is secret-scanned before it goes into the prompt and again on the model's output, and pointing `provider` at a local endpoint (`ollama`, `lmstudio`, `localai`, or any `openai_compatible` server) keeps that data on your machine.

## Source files are authoritative

Mnemo reads a derived view. It never writes to `~/.claude`, `~/.codex`, or any tool home.

## Forget is complete

```bash
mnemo forget <session-id>     # delete the session + its events
mnemo forget --task <task-id> # delete the task, its working states + links
```

Deletion removes Mnemo's derived data. The original transcript on disk — which Mnemo never owned — is untouched.

## Opting out

`.mnemo/ignore` keeps whole tools or specific sessions out of ingestion entirely. See [Session ingestion](../sessions/).
