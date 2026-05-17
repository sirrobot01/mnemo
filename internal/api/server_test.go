package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirrobot01/mnemo/internal/api"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/sessions"
	"github.com/sirrobot01/mnemo/internal/sessions/claude"
	"github.com/sirrobot01/mnemo/internal/storage/sqlite"
)

func TestAPIEndToEnd(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	repoRoot := t.TempDir()
	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_a", Name: "a", RootPath: repoRoot, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("repo: %v", err)
	}

	cHome := t.TempDir()
	cdir := filepath.Join(cHome, "projects", claude.EncodeRepoRoot(repoRoot))
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jsonl := `{"type":"user","message":{"role":"user","content":"fix the cache in internal/cache.go"},"timestamp":"2026-05-17T12:00:00.000Z","sessionId":"s1","gitBranch":"main"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done. Fixed it."}]},"timestamp":"2026-05-17T12:00:30.000Z","sessionId":"s1","gitBranch":"main"}
`
	if err := os.WriteFile(filepath.Join(cdir, "s1.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	h := api.New(repo, store, []sessions.Adapter{claude.New(cHome)}, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	post := func(path, body string) *http.Response {
		resp, err := http.Post(srv.URL+path, "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		return resp
	}
	get := func(path string) *http.Response {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return resp
	}

	if r := get("/v1/health"); r.StatusCode != 200 {
		t.Fatalf("health = %d", r.StatusCode)
	}
	if r := post("/v1/ingest", ""); r.StatusCode != 200 {
		t.Fatalf("ingest = %d", r.StatusCode)
	}

	var tasks []domain.Task
	r := get("/v1/tasks")
	if r.StatusCode != 200 {
		t.Fatalf("tasks = %d", r.StatusCode)
	}
	if err := json.NewDecoder(r.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// resume for the active task, default (local) tool.
	rr := post("/v1/resume", `{}`)
	if rr.StatusCode != 200 {
		t.Fatalf("resume = %d", rr.StatusCode)
	}
	var rendered struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&rendered); err != nil {
		t.Fatalf("decode resume: %v", err)
	}
	if rendered.Content == "" {
		t.Fatal("resume content empty")
	}

	// cross-vendor refused without opt-in.
	if r := post("/v1/resume", `{"tool":"codex"}`); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-vendor resume = %d, want 403", r.StatusCode)
	}

	if r := get("/v1/tasks/" + string(tasks[0].ID) + "/state"); r.StatusCode != 200 {
		t.Fatalf("task state = %d", r.StatusCode)
	}
	if r := get("/v1/tasks/missing"); r.StatusCode != http.StatusNotFound {
		t.Fatalf("missing task = %d, want 404", r.StatusCode)
	}
}
