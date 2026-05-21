---
title: Tasks and threading
description: The task is the unit of continuity. How sessions thread into it, and how stale tasks decay.
---

A **task** is one in-progress unit of work. It is the durable object — sessions are disposable and per-tool; the task is what many sessions across many agents attach to.

## The threading heuristic

When sessions are ingested, each not-yet-threaded session is attached to a task:

- It joins the most recently active **non-done task on the same git branch** whose last activity is within the *idle window* (default 45 minutes).
- Otherwise a new task is created.
- Tasks are **never merged across branches**. A wrong association produces a confidently wrong resume, so threading stays conservative.

## Explicit override always wins

If you start or switch to a task explicitly, it becomes **pinned**:

```bash
mnemo task start "fix the auth race"
mnemo task switch <task-id>
```

While a non-done pinned task exists, every newly-ingested session attaches to it **regardless of branch**. Pausing or finishing the task relinquishes the override and threading falls back to the heuristic. At most one task is pinned at a time.

## Lifecycle

```text
active ⇄ paused
active or paused → done   (terminal — start a new task instead of reopening)
```

```bash
mnemo task list
mnemo task show <id>
mnemo task pause <id>
mnemo task done <id>
```

## Decay

Working state is ephemeral. A **non-pinned** task with no activity for longer than `cold_after` (default 14 days, set via `tasks.cold_after`) is *cold*: it stops being offered as the active task, and `mnemo watch` auto-pauses it. Pinned tasks never decay — an explicit choice persists until you change it.

This is why `mnemo resume` with no `--task` always reflects something current, not a forgotten task from last month.
