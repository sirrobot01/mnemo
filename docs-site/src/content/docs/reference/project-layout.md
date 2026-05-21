---
title: Project layout
description: Where the code lives.
---

```text
cmd/mnemo/                 binary entrypoint
internal/
  cli/                     cobra command tree (init, ingest, watch, task,
                           resume, status, forget, serve, db)
  api/                     HTTP /v1 surface
  ui/                      embeds the built web/ bundle
  app/
    ingestsvc/             session ingestion (secret-scanned, idempotent)
    tasksvc/               threading heuristic + lifecycle + decay
    statesvc/              deterministic state-of-play compiler
    resumesvc/             render + cross-vendor gate + secret redaction
  sessions/                Adapter contract (+ optional DirWatcher)
    claude/ codex/ aider/ continueide/ copilot/ cursor/ windsurf/
  domain/                  entities, validation, ID helpers
  storage/                 interfaces
    sqlite/ postgres/      adapters
  migrations/              embedded SQL (sqlite/ + postgres/)
  safety/                  local secret detection
  config/                  .mnemo/config.yaml + .mnemo/ignore
  logging/                 slog setup + HTTP middleware
web/                       Vite + React task-timeline SPA (embedded)
docs/                      PRD, BUILD_PLAN, IMPLEMENTATION_STATUS, …
docs-site/                 this Astro/Starlight site
```

## Layering invariant

`internal/app/*svc` packages must not import concrete adapters
(`storage/sqlite`, `storage/postgres`) or surfaces (`api`, `cli`, `ui`).
Enforced by `internal/app/layering_test.go`, which parses every service
file's imports.

## Extension seams

- **New agent** → implement `sessions.Adapter` (+ optional `DirWatcher`), add a built-in provider, and register a resume launcher when the agent has a CLI prompt entrypoint.
- **New resume target** → extend `resumesvc` rendering.
- **Optional enrichment** → a `statesvc.Enricher` (disabled by default; errors fall back to the deterministic state).
