---
title: Resume and injection
description: How the state of play reaches the next agent, and the egress gate.
---

`mnemo resume` is the delivery step. It threads any freshly-ingested sessions, picks the task (explicit `--task`, otherwise the most-recently-active non-cold task), compiles the latest state of play, and either prints it or opens an agent with it.

```bash
mnemo resume                       # print to stdout
mnemo resume codex                 # launch Codex with the handoff
mnemo resume claude                # launch Claude with the handoff
mnemo resume aider                 # run Aider with the handoff
mnemo resume continue              # run Continue CLI with the handoff
mnemo resume copilot               # run GitHub Copilot CLI with the handoff
mnemo resume cursor                # launch Cursor agent with the handoff
mnemo resume windsurf              # run Devin/Windsurf with the handoff
mnemo resume codex --print         # render for Codex, do not launch
mnemo resume --task <id>
mnemo resume --write               # write a managed block to .mnemo/
```

## Output

A markdown block bounded by managed markers:

```md
<!-- mnemo:resume:start -->
# Resume — <goal>
## Done
## Next steps
## Rejected — do not retry
## Open questions
## Files touched
## Working hypotheses — UNCONFIRMED, do not treat as fact
<!-- mnemo:resume:end -->
```

Default is stdout, so you can inspect, pipe, or paste the handoff yourself. Passing an agent name launches that CLI with the handoff. `--write` drops the handoff into a managed block under `.mnemo/` instead of stdout.

## Cross-vendor egress gate

Injecting a state of play derived from one vendor's agent into a **different vendor's** agent is a cross-vendor data flow.

```bash
mnemo resume codex
mnemo resume claude
mnemo resume aider
mnemo resume continue
mnemo resume copilot
mnemo resume cursor
mnemo resume windsurf
```

For the CLI, passing a positional agent is the explicit opt-in to launch that agent with the current handoff. API callers use `allow_cross_vendor` for the same decision.

Targeting `stdout`/`generic`, or an agent that is already among the task's source tools, is never gated.

## Second secret scan

The rendered output is scanned again before it leaves Mnemo. Any line that trips the secret detector is replaced with `[REDACTED]` — a partial secret is never emitted, even though events were already scanned at ingest.
