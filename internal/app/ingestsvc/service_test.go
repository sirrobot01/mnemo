package ingestsvc_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sirrobot01/mnemo/internal/app/ingestsvc"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/sessions"
	"github.com/sirrobot01/mnemo/internal/sessions/claude"
	"github.com/sirrobot01/mnemo/internal/storage/sqlite"
)

func TestImportIdempotentAndRedacts(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	adapter, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer adapter.Close()

	repoRoot := t.TempDir()
	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_t", Name: "t", RootPath: repoRoot, CreatedAt: now, UpdatedAt: now}
	if err := adapter.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	home := t.TempDir()
	projects := filepath.Join(home, "projects", claude.EncodeRepoRoot(repoRoot))
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jsonl := `{"type":"user","message":{"role":"user","content":"refactor the auth cache"},"timestamp":"2026-05-17T10:00:00.000Z","sessionId":"s1","gitBranch":"main"}
{"type":"user","message":{"role":"user","content":"token=ghp_abcdefghijklmnopqrstuvwxyz0123"},"timestamp":"2026-05-17T10:00:01.000Z","sessionId":"s1","gitBranch":"main"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]},"timestamp":"2026-05-17T10:00:05.000Z","sessionId":"s1","gitBranch":"main"}
`
	if err := os.WriteFile(filepath.Join(projects, "s1.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	svc := ingestsvc.New(repo, adapter, sessions.SingleAgentRegistry("claude", claude.New(home)))

	res, err := svc.Import(ctx)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res) != 1 || res[0].Imported != 1 {
		t.Fatalf("expected 1 imported session, got %+v", res)
	}
	if res[0].RedactedEvents != 1 {
		t.Fatalf("expected 1 redacted event (the ghp_ token), got %d", res[0].RedactedEvents)
	}

	sessionsList, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessionsList) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessionsList))
	}
	events, err := svc.Events(ctx, sessionsList[0].ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 kept events (secret dropped), got %d", len(events))
	}

	// Re-import must be idempotent: deterministic IDs + INSERT OR IGNORE.
	if _, err := svc.Import(ctx); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	events2, err := svc.Events(ctx, sessionsList[0].ID)
	if err != nil {
		t.Fatalf("events2: %v", err)
	}
	if len(events2) != 2 {
		t.Fatalf("re-import duplicated events: got %d, want 2", len(events2))
	}
}

func TestImportHonorsIgnore(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	adapter, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer adapter.Close()

	repoRoot := t.TempDir()
	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_i", Name: "i", RootPath: repoRoot, CreatedAt: now, UpdatedAt: now}
	if err := adapter.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	// .mnemo/ignore: skip the codex tool entirely and any skip-*.jsonl.
	if err := os.MkdirAll(filepath.Join(repoRoot, ".mnemo"), 0o755); err != nil {
		t.Fatalf("mkdir .mnemo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".mnemo", "ignore"), []byte("codex\nskip-*.jsonl\n"), 0o644); err != nil {
		t.Fatalf("write ignore: %v", err)
	}

	home := t.TempDir()
	projects := filepath.Join(home, "projects", claude.EncodeRepoRoot(repoRoot))
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	line := `{"type":"user","message":{"role":"user","content":"hello"},"timestamp":"2026-05-17T10:00:00.000Z","sessionId":"s","gitBranch":"main"}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, "keep.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projects, "skip-me.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatalf("write skip: %v", err)
	}

	svc := ingestsvc.New(repo, adapter, sessions.SingleAgentRegistry("claude", claude.New(home)))
	res, err := svc.Import(ctx)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res) != 1 || res[0].Agent != "claude" {
		t.Fatalf("codex tool should be skipped entirely; got %+v", res)
	}
	if res[0].Discovered != 2 || res[0].Skipped != 1 || res[0].Imported != 1 {
		t.Fatalf("expected discovered=2 skipped=1 imported=1, got %+v", res[0])
	}
}

func TestImportSkipsUnchangedFiles(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	adapter, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer adapter.Close()

	repoRoot := t.TempDir()
	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_w", Name: "w", RootPath: repoRoot, CreatedAt: now, UpdatedAt: now}
	if err := adapter.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("repo: %v", err)
	}
	home := t.TempDir()
	projects := filepath.Join(home, "projects", claude.EncodeRepoRoot(repoRoot))
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(projects, "s1.jsonl")
	l1 := `{"type":"user","message":{"role":"user","content":"first"},"timestamp":"2026-05-17T10:00:00.000Z","sessionId":"s1","gitBranch":"main"}` + "\n"
	if err := os.WriteFile(file, []byte(l1), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	svc := ingestsvc.New(repo, adapter, sessions.SingleAgentRegistry("claude", claude.New(home)))

	r1, err := svc.Import(ctx)
	if err != nil {
		t.Fatalf("import 1: %v", err)
	}
	if r1[0].Imported != 1 || r1[0].Unchanged != 0 {
		t.Fatalf("first import: %+v", r1[0])
	}

	// Re-import with the file untouched → skipped via fingerprint.
	r2, err := svc.Import(ctx)
	if err != nil {
		t.Fatalf("import 2: %v", err)
	}
	if r2[0].Imported != 0 || r2[0].Unchanged != 1 {
		t.Fatalf("unchanged re-import should skip, got %+v", r2[0])
	}

	// Append a line (size changes → fingerprint changes) → re-ingested.
	l2 := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"second"}]},"timestamp":"2026-05-17T10:01:00.000Z","sessionId":"s1","gitBranch":"main"}` + "\n"
	if err := os.WriteFile(file, []byte(l1+l2), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	r3, err := svc.Import(ctx)
	if err != nil {
		t.Fatalf("import 3: %v", err)
	}
	if r3[0].Imported != 1 || r3[0].Unchanged != 0 {
		t.Fatalf("changed file should re-ingest, got %+v", r3[0])
	}
	sl, _ := svc.List(ctx)
	evs, _ := svc.Events(ctx, sl[0].ID)
	if len(evs) != 2 {
		t.Fatalf("expected 2 events after append, got %d", len(evs))
	}
}

func BenchmarkImportUnchanged(b *testing.B) {
	ctx := context.Background()
	dsn := filepath.Join(b.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		b.Fatalf("migrate: %v", err)
	}
	adapter, err := sqlite.Open(ctx, dsn)
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	defer adapter.Close()
	repoRoot := b.TempDir()
	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_b", Name: "b", RootPath: repoRoot, CreatedAt: now, UpdatedAt: now}
	_ = adapter.CreateRepository(ctx, repo)
	home := b.TempDir()
	projects := filepath.Join(home, "projects", claude.EncodeRepoRoot(repoRoot))
	_ = os.MkdirAll(projects, 0o755)
	var sb []byte
	for i := 0; i < 400; i++ {
		sb = append(sb, []byte(`{"type":"user","message":{"role":"user","content":"line `+strconv.Itoa(i)+`"},"timestamp":"2026-05-17T10:00:00.000Z","sessionId":"s","gitBranch":"main"}`+"\n")...)
	}
	_ = os.WriteFile(filepath.Join(projects, "s.jsonl"), sb, 0o644)
	svc := ingestsvc.New(repo, adapter, sessions.SingleAgentRegistry("claude", claude.New(home)))
	if _, err := svc.Import(ctx); err != nil { // prime
		b.Fatalf("prime: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Import(ctx); err != nil {
			b.Fatalf("import: %v", err)
		}
	}
}
