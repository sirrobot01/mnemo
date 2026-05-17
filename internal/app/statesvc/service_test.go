package statesvc_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sirrobot01/mnemo/internal/app/statesvc"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/storage/sqlite"
)

func TestCompileExtractsStateOfPlay(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer a.Close()

	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_t", Name: "t", RootPath: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	if err := a.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("repo: %v", err)
	}
	task := domain.Task{ID: "task_1", RepoID: repo.ID, Title: "auth race", Status: domain.TaskStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now}
	if err := a.SaveTask(ctx, task); err != nil {
		t.Fatalf("task: %v", err)
	}
	sess := domain.Session{ID: "sess_1", RepoID: repo.ID, Tool: domain.SessionToolClaude, SourcePath: "/x.jsonl", Status: domain.SessionStatusIngested, StartedAt: now, IngestedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := a.SaveSession(ctx, sess); err != nil {
		t.Fatalf("sess: %v", err)
	}
	if err := a.AttachSession(ctx, task.ID, sess.ID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	events := []domain.SessionEvent{
		{ID: "e1", SessionID: sess.ID, Sequence: 1, Type: domain.SessionEventTypeUserMessage, Content: "refactor the auth cache in internal/auth/cache.go", Timestamp: now, CreatedAt: now},
		{ID: "e2", SessionID: sess.ID, Sequence: 2, Type: domain.SessionEventTypeAssistantMessage, Content: "I think the bug is in the cache layer.", Timestamp: now, CreatedAt: now},
		{ID: "e3", SessionID: sess.ID, Sequence: 3, Type: domain.SessionEventTypeUserMessage, Content: "no, don't use a global mutex", Timestamp: now, CreatedAt: now},
		{ID: "e4", SessionID: sess.ID, Sequence: 4, Type: domain.SessionEventTypeAssistantMessage, Content: "Done. Fixed the race with singleflight.", Timestamp: now, CreatedAt: now},
	}
	if err := a.AppendSessionEvents(ctx, sess.ID, events); err != nil {
		t.Fatalf("events: %v", err)
	}

	svc := statesvc.New(a, a, a)
	ws, err := svc.Compile(ctx, task.ID)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if ws.Version != 1 {
		t.Fatalf("version = %d, want 1", ws.Version)
	}
	if len(ws.Hypotheses) != 1 || ws.Hypotheses[0].Confirmed {
		t.Fatalf("expected 1 unconfirmed hypothesis, got %+v", ws.Hypotheses)
	}
	if len(ws.Rejected) != 1 {
		t.Fatalf("expected 1 rejected approach, got %+v", ws.Rejected)
	}
	if len(ws.Done) == 0 {
		t.Fatalf("expected at least one done item")
	}
	foundFile := false
	for _, f := range ws.FilesTouched {
		if f.Path == "internal/auth/cache.go" {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatalf("expected internal/auth/cache.go in files touched, got %+v", ws.FilesTouched)
	}

	ws2, err := svc.Compile(ctx, task.ID)
	if err != nil {
		t.Fatalf("recompile: %v", err)
	}
	if ws2.Version != 2 {
		t.Fatalf("recompile version = %d, want 2", ws2.Version)
	}
}

func TestExtractionQualityHardening(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer a.Close()
	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_q", Name: "q", RootPath: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	if err := a.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("repo: %v", err)
	}
	task := domain.Task{ID: "task_q", RepoID: repo.ID, Title: "q", Status: domain.TaskStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now}
	if err := a.SaveTask(ctx, task); err != nil {
		t.Fatalf("task: %v", err)
	}
	sess := domain.Session{ID: "sess_q", RepoID: repo.ID, Tool: domain.SessionToolClaude, SourcePath: "/q.jsonl", Status: domain.SessionStatusIngested, StartedAt: now, IngestedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := a.SaveSession(ctx, sess); err != nil {
		t.Fatalf("sess: %v", err)
	}
	if err := a.AttachSession(ctx, task.ID, sess.ID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	ev := func(seq int, typ domain.SessionEventType, content string) domain.SessionEvent {
		return domain.SessionEvent{ID: domain.ID("e" + strconv.Itoa(seq)), SessionID: sess.ID, Sequence: seq, Type: typ, Content: content, Timestamp: now, CreatedAt: now}
	}
	if err := a.AppendSessionEvents(ctx, sess.ID, []domain.SessionEvent{
		ev(1, domain.SessionEventTypeUserMessage, "bumped deps to v2.0 and python 3.11; touch internal/api/server.go please. e.g. the handler."),
		ev(2, domain.SessionEventTypeAssistantMessage, "I cannot do that yet. Another option exists."),
		ev(3, domain.SessionEventTypeAssistantMessage, "Done. Fixed the retry in internal/billing/paystack.go."),
		ev(4, domain.SessionEventTypeUserMessage, "no, do not add a new dependency"),
	}); err != nil {
		t.Fatalf("events: %v", err)
	}

	ws, err := statesvc.New(a, a, a).Compile(ctx, task.ID)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Files: only real paths; v2.0 / 3.11 / e.g. must NOT appear.
	paths := map[string]bool{}
	for _, f := range ws.FilesTouched {
		paths[f.Path] = true
	}
	if !paths["internal/api/server.go"] || !paths["internal/billing/paystack.go"] {
		t.Fatalf("expected real files, got %+v", ws.FilesTouched)
	}
	for bad := range paths {
		if bad == "v2.0" || bad == "3.11" || bad == "e.g." {
			t.Fatalf("version/prose token treated as file: %q", bad)
		}
	}

	// Done must be the informative clause, never a bare "Done".
	for _, d := range ws.Done {
		if d == "Done" || len(d) < 8 {
			t.Fatalf("low-quality done item: %q (full: %+v)", d, ws.Done)
		}
	}
	if len(ws.Done) == 0 {
		t.Fatalf("expected a done item from the 'Done. Fixed...' message")
	}

	// "I cannot do that" / "Another option" must NOT be a correction; the
	// only correction is the e4 message starting with "no,".
	if len(ws.Rejected) != 1 {
		t.Fatalf("expected exactly 1 rejected (only the 'no,' message), got %+v", ws.Rejected)
	}
}
