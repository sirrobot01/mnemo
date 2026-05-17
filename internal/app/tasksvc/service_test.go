package tasksvc_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirrobot01/mnemo/internal/app/tasksvc"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/storage/sqlite"
)

func openStore(t *testing.T) (*sqlite.Adapter, domain.Repository) {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_t", Name: "t", RootPath: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	if err := a.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("repo: %v", err)
	}
	return a, repo
}

func saveSession(t *testing.T, a *sqlite.Adapter, repo domain.Repository, id, branch string, start time.Time) {
	t.Helper()
	ctx := context.Background()
	s := domain.Session{
		ID: domain.ID(id), RepoID: repo.ID, Tool: domain.SessionToolClaude,
		SourcePath: "/x/" + id + ".jsonl", Branch: branch, Status: domain.SessionStatusIngested,
		StartedAt: start, IngestedAt: start, CreatedAt: start, UpdatedAt: start, MessageCount: 1,
	}
	if err := a.SaveSession(ctx, s); err != nil {
		t.Fatalf("save session: %v", err)
	}
}

func TestThreadingHeuristic(t *testing.T) {
	ctx := context.Background()
	a, repo := openStore(t)
	base := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)

	saveSession(t, a, repo, "sess_a", "main", base)
	saveSession(t, a, repo, "sess_b", "main", base.Add(10*time.Minute)) // within idle window
	saveSession(t, a, repo, "sess_c", "feature", base.Add(20*time.Minute))
	saveSession(t, a, repo, "sess_d", "main", base.Add(10*time.Hour)) // beyond idle window

	svc := tasksvc.New(repo, a, a, a, tasksvc.DefaultIdleWindow)
	n, err := svc.Thread(ctx)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4 sessions threaded, got %d", n)
	}
	tasks, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// main(close pair) -> 1 task, feature -> 1 task, main(after idle gap) -> 1 task = 3.
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	// The first main task must hold exactly sess_a and sess_b.
	for _, tk := range tasks {
		ids, _ := svc.Sessions(ctx, tk.ID)
		if tk.Branch == "feature" && len(ids) != 1 {
			t.Fatalf("feature task should have 1 session, got %d", len(ids))
		}
	}

	// Re-thread is idempotent: no new attachments.
	n2, err := svc.Thread(ctx)
	if err != nil {
		t.Fatalf("re-thread: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("re-thread attached %d, want 0", n2)
	}
}

func TestExplicitOverrideWins(t *testing.T) {
	ctx := context.Background()
	a, repo := openStore(t)
	svc := tasksvc.New(repo, a, a, a, tasksvc.DefaultIdleWindow)

	// Explicitly start a branchless task, then ingest sessions on branch
	// "main". They must attach to the pinned task even though the branch
	// heuristic would never match a branchless task.
	started, err := svc.Start(ctx, "the thing I am doing", "", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	base := time.Date(2026, 5, 17, 9, 0, 0, 0, time.UTC)
	saveSession(t, a, repo, "sess_1", "main", base)
	saveSession(t, a, repo, "sess_2", "feature-x", base.Add(2*time.Hour))

	n, err := svc.Thread(ctx)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 sessions attached to pinned task, got %d", n)
	}
	tasks, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != started.ID {
		t.Fatalf("expected only the started task, got %+v", tasks)
	}
	ids, _ := svc.Sessions(ctx, started.ID)
	if len(ids) != 2 {
		t.Fatalf("expected both sessions on the pinned task across branches, got %d", len(ids))
	}

	// Pausing the pinned task relinquishes the override: a new session now
	// threads by the branch heuristic into its own task.
	if _, err := svc.Pause(ctx, started.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	saveSession(t, a, repo, "sess_3", "feature-y", base.Add(4*time.Hour))
	if _, err := svc.Thread(ctx); err != nil {
		t.Fatalf("thread after pause: %v", err)
	}
	tasks, _ = svc.List(ctx)
	if len(tasks) != 2 {
		t.Fatalf("after unpin, sess_3 should form its own task (2 total), got %d", len(tasks))
	}
}

func TestDecayColdTasks(t *testing.T) {
	ctx := context.Background()
	a, repo := openStore(t)
	svc := tasksvc.New(repo, a, a, a, tasksvc.DefaultIdleWindow)

	old := time.Now().UTC().Add(-30 * 24 * time.Hour) // older than 14d default
	fresh := time.Now().UTC()
	save := func(id string, pinned bool, last time.Time) {
		tk := domain.Task{
			ID: domain.ID(id), RepoID: repo.ID, Title: id,
			Status: domain.TaskStatusActive, Pinned: pinned,
			CreatedAt: last, UpdatedAt: last, LastActiveAt: last,
		}
		if err := a.SaveTask(ctx, tk); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	save("task_cold", false, old)
	save("task_warm", false, fresh)
	save("task_pinned_old", true, old)

	// A cold, non-pinned task must not be offered as active.
	active, ok, err := svc.Active(ctx)
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if !ok || active.ID != "task_warm" {
		t.Fatalf("expected task_warm active, got ok=%v id=%s", ok, active.ID)
	}

	// Decay auto-pauses the cold non-pinned task only.
	n, err := svc.Decay(ctx)
	if err != nil {
		t.Fatalf("decay: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 task decayed, got %d", n)
	}
	cold, _ := svc.Get(ctx, "task_cold")
	if cold.Status != domain.TaskStatusPaused {
		t.Fatalf("cold task should be paused, got %s", cold.Status)
	}
	pinned, _ := svc.Get(ctx, "task_pinned_old")
	if pinned.Status != domain.TaskStatusActive {
		t.Fatalf("pinned task must never decay, got %s", pinned.Status)
	}
	warm, _ := svc.Get(ctx, "task_warm")
	if warm.Status != domain.TaskStatusActive {
		t.Fatalf("warm task must stay active, got %s", warm.Status)
	}
}

func TestLifecycleTransitions(t *testing.T) {
	ctx := context.Background()
	a, repo := openStore(t)
	svc := tasksvc.New(repo, a, a, a, tasksvc.DefaultIdleWindow)

	tk, err := svc.Start(ctx, "fix race", "no data races on refresh", "main")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if tk.Status != domain.TaskStatusActive {
		t.Fatalf("new task should be active, got %s", tk.Status)
	}
	if _, err := svc.Pause(ctx, tk.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err := svc.Switch(ctx, tk.ID); err != nil {
		t.Fatalf("switch (paused->active): %v", err)
	}
	if _, err := svc.Done(ctx, tk.ID); err != nil {
		t.Fatalf("done: %v", err)
	}
	if _, err := svc.Switch(ctx, tk.ID); err == nil {
		t.Fatal("done is terminal: switch should fail")
	}
}
