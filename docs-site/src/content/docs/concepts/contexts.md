---
title: Contexts
description: Composable read-only knowledge inputs woven into the handoff.
---

Sessions are *what happened*. **Contexts** are the standing knowledge that should travel with every handoff but does not live in any transcript: house rules, an `AGENTS.md`, an API spec directory, or a project convention.

Contexts are **inputs only**. Mnemo reads them and appends them to the resume output so the next agent sees them — it never writes back into the source.

## Types

A context entry in `.mnemo/config.yaml` has a `name` and a `type`:

```yaml
contexts:
  - { name: house-rules, type: file, path: ./AGENTS.md }
  - { name: api-spec,    type: dir,  path: ./docs/api }
  - { name: upstream,    type: url,  url: https://example.com/conventions.md }
  - { name: shared,      type: context, ref: house-rules }
```

- **file** — a single file's contents.
- **dir** — every file in a directory (bounded; first N files, capped size).
- **url** — fetched over the network. **Off by default** — see Privacy below.
- **context** — a reference to another context by name. References compose into a DAG, resolved transitively with **cycle detection** and a depth cap.

Manage them with the CLI:

```bash
mnemo context add house-rules --type file --path ./AGENTS.md
mnemo context list
mnemo context show          # the resolved, scrubbed block, exactly as the next agent will see it
mnemo context remove house-rules
```

## Resolution and safety

At resume time every context is resolved, the DAG is flattened, content is truncated to bounded caps, and **every line is run through the same secret scanner** ingestion uses — a line that trips it becomes `[REDACTED]`. The result is appended to the handoff under a clearly marked *read-only* section.

`url` contexts are an egress action: they are **withheld** (resolved to a placeholder) unless the repo sets `privacy.allow_context_url_egress: true`. See [Privacy and safety](../privacy/).

## Where it lands

The resolved block is appended to `mnemo resume` output between the resume markers, after the state of play, headed *"Context — read-only house rules, do not modify these sources."* It is part of the [resume injection](../resume/), governed by the same cross-vendor egress rules.
