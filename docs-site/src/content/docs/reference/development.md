---
title: Development
description: Build, test, and run locally.
---

## Build and test

```bash
go build ./...
go vet ./...
go test ./...
```

`go test ./...` covers ingestion idempotency + redaction, the threading
heuristic (same-branch merge, cross-branch split, idle split, explicit
override, decay), the state-of-play compiler, the HTTP API end to end, the
PostgreSQL sanitizers, and every session adapter parser.

A live PostgreSQL server is not required — the Postgres adapter and its
migrations are build-validated; storage tests run against SQLite.

## The web bundle

```bash
cd web
npm install      # first time
npm run build    # tsc + vite → web/dist (embedded by internal/ui)
```

A placeholder `web/dist/index.html` is committed so `go build` works before the bundle is built.

## The docs site

```bash
cd docs-site
npm install
npm run dev      # local preview
npm run build    # static build
```

## Conventions

- Application logic lives in `internal/app/*svc`; CLI/API/UI are thin surfaces over the same services.
- New code goes through services, not storage adapters directly (enforced by the layering test).
- Migrations are append-only embedded SQL; both backends stay in lockstep.
- Secret scanning runs on ingest and on resume render — never weaken either path.
