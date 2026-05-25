---
title: State of play
description: The compiled, versioned summary a task carries between agents.
---

The next agent does not need a full 200-message transcript. It needs a compact picture of where things stand. That is the **state of play** — a versioned record compiled from a task's ordered session events.

## What it captures

```text
goal             what the task is trying to achieve
done             completed steps
in_progress      the current focus
next_steps       planned but not yet done
rejected         {approach, reason} — tried and abandoned
decisions        {decision, rationale}
open_questions   unresolved questions
files_touched    {path, summary}
hypotheses       {claim, confidence, confirmed}  — confirmed defaults to false
```

`rejected` is the high-value field: it stops the next agent re-proposing an approach you already ruled out, *with the reason why*.

## Deterministic and heuristic-first

Compilation reads signals already present in transcripts — correction language ("no", "actually", "don't") becomes decisions/rejected, plan/done language becomes next/done, uncertainty language becomes unconfirmed hypotheses, path-like tokens become files. **No LLM is required**; compilation is deterministic and offline.

Because it is heuristic, the output is intentionally rough rather than perfect. An optional [enrichment](../../guides/configuration/#enrichment) pass can refine the compiled state with a configured model (OpenAI, Anthropic, Ollama, or any OpenAI-compatible endpoint). It is disabled by default, and if it errors or times out, compilation falls back to the deterministic result — so the offline path is always the floor, never the ceiling.

## Versioning

Each compile produces a new version; the latest wins. The record stores a *source watermark* (the last event consumed) so versions are auditable. Recompiling is cheap and happens automatically under `mnemo watch`.

## Hypothesis tiering

A mid-task belief ("I think the bug is in the cache layer") is recorded with `confirmed: false` and is always rendered as **UNCONFIRMED**. The handoff must never promote a guess to established fact.
