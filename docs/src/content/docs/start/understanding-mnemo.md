---
title: Understanding Mnemo
description: The problem Mnemo solves and the model it uses.
---

## The problem

You increasingly use more than one AI coding agent — Claude Code, Codex, Cursor, Aider — often within the same task, because each is better at different things.

But agents have **no shared working memory**. Each one only knows its own conversation. When you switch mid-task, the new agent has no idea what the task is, what you already did, what you tried and abandoned, what you decided, or which files are in play. You become a manual clipboard, and the switch that should take seconds takes ten minutes.

This is distinct from the "project rules" problem (a repo uses bun, not GORM). That is largely handled by per-tool context files. **Working-state continuity is not handled by anything** — and it gets worse as agent usage fragments.

## The model

Mnemo has one job and aims to do it completely.

- **Sessions** are ingested agent transcripts (read-only, secret-scanned).
- A **task** is the durable unit of work; sessions thread into it.
- The **state of play** is a compiled, versioned summary of the task.
- **Resume** injects the state of play into the next agent.

```text
Bad (manual clipboard):
Claude → you re-explain → Codex → you re-explain → Cursor

Good (Mnemo):
Claude / Codex / Cursor
          ↕  auto-ingest         ↕  auto-resume
            Mnemo task state of play
```

## What it deliberately is not

A coding agent, an editor, a hosted SaaS, a rules/linter generator, or a shared *chat* between agents. It shares a task's *state of play*, not conversation history.

## Principles

- **Zero-friction capture** — reads what agents already write; no save step.
- **The task is the unit** — sessions are disposable; threading correctness is paramount.
- **Compiled, not concatenated** — a compact picture, not a transcript dump.
- **Ephemeral and tiered** — working state decays; unconfirmed beliefs stay labeled unconfirmed.
- **Local-first, consent-gated egress** — cross-vendor injection is off by default.
