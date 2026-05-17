package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirrobot01/mnemo/internal/app/ingestsvc"
	"github.com/sirrobot01/mnemo/internal/app/statesvc"
	"github.com/sirrobot01/mnemo/internal/app/tasksvc"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/sessions/claude"
	"github.com/sirrobot01/mnemo/internal/sessions/codex"
	"github.com/sirrobot01/mnemo/internal/storage/sqlite"
)

// TestClaudeThenCodexSingleTask proves the headline promise: a Claude
// session followed by a Codex session on the same repo+branch (within the
// idle window) threads into ONE task with ONE continuous state of play.
func TestClaudeThenCodexSingleTask(t *testing.T) {
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

	repoRoot := t.TempDir()
	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_c", Name: "c", RootPath: repoRoot, CreatedAt: now, UpdatedAt: now}
	if err := a.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("repo: %v", err)
	}

	// Claude fixture.
	cHome := t.TempDir()
	cDir := filepath.Join(cHome, ".claude", "projects", claude.EncodeRepoRoot(repoRoot))
	if err := os.MkdirAll(cDir, 0o755); err != nil {
		t.Fatalf("mkdir claude: %v", err)
	}
	claudeJSONL := `{"type":"user","message":{"role":"user","content":"add retry to internal/billing/paystack.go"},"timestamp":"2026-05-17T12:00:00.000Z","sessionId":"cs","gitBranch":"main"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I think backoff goes in the provider."}]},"timestamp":"2026-05-17T12:01:00.000Z","sessionId":"cs","gitBranch":"main"}
`
	if err := os.WriteFile(filepath.Join(cDir, "cs.jsonl"), []byte(claudeJSONL), 0o644); err != nil {
		t.Fatalf("write claude: %v", err)
	}

	// Codex fixture (10 minutes later, same branch — within idle window).
	xHome := t.TempDir()
	xDir := filepath.Join(xHome, "sessions", "2026", "05", "17")
	if err := os.MkdirAll(xDir, 0o755); err != nil {
		t.Fatalf("mkdir codex: %v", err)
	}
	codexJSONL := `{"type":"session_meta","timestamp":"2026-05-17T12:10:00Z","payload":{"id":"xs","timestamp":"2026-05-17T12:10:00Z","cwd":"` + repoRoot + `","git":{"branch":"main"}}}
{"type":"response_item","timestamp":"2026-05-17T12:10:30Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done. Implemented backoff in paystack.go."}]}}
`
	if err := os.WriteFile(filepath.Join(xDir, "rollout-2026-05-17-xs.jsonl"), []byte(codexJSONL), 0o644); err != nil {
		t.Fatalf("write codex: %v", err)
	}

	isvc := ingestsvc.New(repo, a, claude.New(cHome+"/.claude"), codex.New(xHome))
	if _, err := isvc.Import(ctx); err != nil {
		t.Fatalf("import: %v", err)
	}

	tsvc := tasksvc.New(repo, a, a, a, tasksvc.DefaultIdleWindow)
	if _, err := tsvc.Thread(ctx); err != nil {
		t.Fatalf("thread: %v", err)
	}
	tasks, err := tsvc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected ONE task spanning both tools, got %d", len(tasks))
	}
	ids, _ := tsvc.Sessions(ctx, tasks[0].ID)
	if len(ids) != 2 {
		t.Fatalf("expected 2 sessions on the task, got %d", len(ids))
	}

	ssvc := statesvc.New(a, a, a)
	ws, err := ssvc.Compile(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	foundFile := false
	for _, f := range ws.FilesTouched {
		if f.Path == "internal/billing/paystack.go" {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatalf("state of play should span both sessions; files=%+v", ws.FilesTouched)
	}
	if len(ws.Done) == 0 {
		t.Fatalf("expected a done item from the Codex turn")
	}
}
