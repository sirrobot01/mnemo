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
	"github.com/sirrobot01/mnemo/internal/app/authsvc"
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

	h := api.New(repo, store, sessions.SingleAgentRegistry("claude", claude.New(cHome)), nil, nil, false)
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

	newTaskResp := post("/v1/tasks", `{"title":"Manual UI work","goal":"Polish the web surface","branch":"ui"}`)
	if newTaskResp.StatusCode != http.StatusOK {
		t.Fatalf("start task = %d", newTaskResp.StatusCode)
	}
	var manual domain.Task
	if err := json.NewDecoder(newTaskResp.Body).Decode(&manual); err != nil {
		t.Fatalf("decode manual task: %v", err)
	}
	if !manual.Pinned || manual.Status != domain.TaskStatusActive {
		t.Fatalf("new task should be active and pinned, got status=%s pinned=%v", manual.Status, manual.Pinned)
	}

	pauseResp := post("/v1/tasks/"+string(manual.ID)+"/pause", `{}`)
	if pauseResp.StatusCode != http.StatusOK {
		t.Fatalf("pause task = %d", pauseResp.StatusCode)
	}
	var paused domain.Task
	if err := json.NewDecoder(pauseResp.Body).Decode(&paused); err != nil {
		t.Fatalf("decode paused task: %v", err)
	}
	if paused.Status != domain.TaskStatusPaused || paused.Pinned {
		t.Fatalf("paused task should be unpinned, got status=%s pinned=%v", paused.Status, paused.Pinned)
	}

	switchResp := post("/v1/tasks/"+string(manual.ID)+"/switch", `{}`)
	if switchResp.StatusCode != http.StatusOK {
		t.Fatalf("switch task = %d", switchResp.StatusCode)
	}
	var switched domain.Task
	if err := json.NewDecoder(switchResp.Body).Decode(&switched); err != nil {
		t.Fatalf("decode switched task: %v", err)
	}
	if switched.Status != domain.TaskStatusActive || !switched.Pinned {
		t.Fatalf("switched task should be active and pinned, got status=%s pinned=%v", switched.Status, switched.Pinned)
	}

	doneResp := post("/v1/tasks/"+string(manual.ID)+"/done", `{}`)
	if doneResp.StatusCode != http.StatusOK {
		t.Fatalf("done task = %d", doneResp.StatusCode)
	}
	var done domain.Task
	if err := json.NewDecoder(doneResp.Body).Decode(&done); err != nil {
		t.Fatalf("decode done task: %v", err)
	}
	if done.Status != domain.TaskStatusDone || done.Pinned {
		t.Fatalf("done task should be terminal and unpinned, got status=%s pinned=%v", done.Status, done.Pinned)
	}
}

func TestAPIAuthForWebUI(t *testing.T) {
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

	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_auth", Name: "auth", RootPath: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	if err := store.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("repo: %v", err)
	}

	h := api.New(repo, store, sessions.SingleAgentRegistry("claude", claude.New(t.TempDir())), nil, authsvc.New(store, time.Hour), true)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/tasks")
	if err != nil {
		t.Fatalf("GET /tasks: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tasks without token = %d, want 401", resp.StatusCode)
	}

	logoutNoToken, err := http.Post(srv.URL+"/v1/auth/logout", "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("logout without token: %v", err)
	}
	if logoutNoToken.StatusCode != http.StatusUnauthorized {
		t.Fatalf("logout without token = %d, want 401", logoutNoToken.StatusCode)
	}

	creds := `{"email":"dev@example.com","password":"correct horse"}`
	signup, err := http.Post(srv.URL+"/v1/auth/signup", "application/json", bytes.NewBufferString(creds))
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if signup.StatusCode != http.StatusOK {
		t.Fatalf("signup = %d", signup.StatusCode)
	}

	login, err := http.Post(srv.URL+"/v1/auth/login", "application/json", bytes.NewBufferString(creds))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", login.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&body); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if body.Token == "" {
		t.Fatal("login returned empty token")
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+body.Token)
	authed, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authed tasks: %v", err)
	}
	if authed.StatusCode != http.StatusOK {
		t.Fatalf("tasks with token = %d, want 200", authed.StatusCode)
	}

	logoutReq, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/auth/logout", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	logoutReq.Header.Set("Authorization", "Bearer "+body.Token)
	logout, err := http.DefaultClient.Do(logoutReq)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if logout.StatusCode != http.StatusOK {
		t.Fatalf("logout with token = %d, want 200", logout.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, srv.URL+"/v1/tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+body.Token)
	revoked, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("tasks with revoked token: %v", err)
	}
	if revoked.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tasks with revoked token = %d, want 401", revoked.StatusCode)
	}
}
