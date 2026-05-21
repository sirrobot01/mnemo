package postgres_test

// Live PostgreSQL integration. Skipped unless MNEMO_POSTGRES_DSN points at a
// reachable database; the CI/dev runner sets it (see docs/DEVELOPMENT.md).
// This is the durable counterpart to the build-validated adapter — it
// exercises migrations + every store + the §12 metadata-only invariant
// against a real server.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/storage"
	"github.com/sirrobot01/mnemo/internal/storage/postgres"
)

func TestPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("MNEMO_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set MNEMO_POSTGRES_DSN to run the live PostgreSQL integration test")
	}
	ctx := context.Background()

	if _, err := migrations.ApplyPostgres(ctx, dsn); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	a, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer a.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	repo := domain.Repository{ID: "repo_pg", Name: "pg", RootPath: "/tmp/pg-int", CreatedAt: now, UpdatedAt: now}
	if err := a.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if got, err := a.GetRepository(ctx, repo.ID); err != nil || got.Name != "pg" {
		t.Fatalf("get repo: %v / %+v", err, got)
	}

	// Session with an absolute path and a secret-bearing event. The §12
	// invariant: the shared DB must hold neither.
	sess := domain.Session{
		ID: "sess_pg", RepoID: repo.ID, Agent: "claude", Kind: domain.SessionKindClaude,
		SourcePath:        "/Users/secret/proj/.claude/projects/x/s.jsonl",
		SourceFingerprint: "123:456", Branch: "main",
		Status: domain.SessionStatusIngested, StartedAt: now,
		IngestedAt: now, CreatedAt: now, UpdatedAt: now, MessageCount: 1,
	}
	if err := a.SaveSession(ctx, sess); err != nil {
		t.Fatalf("save session: %v", err)
	}
	got, err := a.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.SourcePath != "s.jsonl" {
		t.Fatalf("absolute path leaked to shared DB: %q", got.SourcePath)
	}
	if got.SourceFingerprint != "123:456" || got.Branch != "main" {
		t.Fatalf("metadata not preserved: %+v", got)
	}

	if err := a.AppendSessionEvents(ctx, sess.ID, []domain.SessionEvent{{
		ID: "ev_pg", SessionID: sess.ID, Sequence: 1,
		Type: domain.SessionEventTypeUserMessage,
		Content: "do not leak: token=ghp_abcdefghijklmnopqrstuvwxyz0123",
		StructuredValue: map[string]any{"x": "secret"}, Timestamp: now, CreatedAt: now,
	}}); err != nil {
		t.Fatalf("append events: %v", err)
	}
	evs, err := a.ListSessionEvents(ctx, storage.SessionEventFilter{SessionID: sess.ID})
	if err != nil || len(evs) != 1 {
		t.Fatalf("list events: %v / %d", err, len(evs))
	}
	if evs[0].Content != "" || evs[0].StructuredValue != nil {
		t.Fatalf("raw content/structured leaked to shared DB: %+v", evs[0])
	}
	if evs[0].Type != domain.SessionEventTypeUserMessage || evs[0].Sequence != 1 {
		t.Fatalf("event shape metadata lost: %+v", evs[0])
	}

	// Task incl. Pinned, working state, and the cascade deletes.
	task := domain.Task{
		ID: "task_pg", RepoID: repo.ID, Title: "pg task", Goal: "g",
		Status: domain.TaskStatusActive, Pinned: true, Branch: "main",
		CreatedAt: now, UpdatedAt: now, LastActiveAt: now,
	}
	if err := a.SaveTask(ctx, task); err != nil {
		t.Fatalf("save task: %v", err)
	}
	gt, err := a.GetTask(ctx, task.ID)
	if err != nil || !gt.Pinned || gt.Status != domain.TaskStatusActive {
		t.Fatalf("task roundtrip: %v / %+v", err, gt)
	}
	if err := a.AttachSession(ctx, task.ID, sess.ID); err != nil {
		t.Fatalf("attach: %v", err)
	}

	ws := domain.WorkingState{
		ID: "ws_pg", TaskID: task.ID, Version: 1, CompiledAt: now, CreatedAt: now,
		Goal: "g", Done: []string{"a"},
		Rejected: []domain.RejectedApproach{{Approach: "x", Reason: "y"}},
	}
	if err := a.SaveWorkingState(ctx, ws); err != nil {
		t.Fatalf("save ws: %v", err)
	}
	lw, err := a.GetLatestWorkingState(ctx, task.ID)
	if err != nil || lw.Version != 1 || len(lw.Rejected) != 1 || lw.Rejected[0].Reason != "y" {
		t.Fatalf("ws roundtrip: %v / %+v", err, lw)
	}

	if err := a.DeleteWorkingStates(ctx, task.ID); err != nil {
		t.Fatalf("delete ws: %v", err)
	}
	if err := a.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	if err := a.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
}
