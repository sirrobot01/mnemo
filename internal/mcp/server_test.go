package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sirrobot01/mnemo/internal/app/authsvc"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/sessions"
	"github.com/sirrobot01/mnemo/internal/sessions/claude"
	"github.com/sirrobot01/mnemo/internal/storage/sqlite"
)

// newTestServer builds an MCP server backed by a throwaway SQLite store. The
// New call alone is the key assertion: it runs every sdk.AddTool, which
// reflects the domain output types into JSON schema — a panic/error there is
// the main risk of wiring real domain structs as tool outputs. The adapter is
// returned too so HTTP-auth tests can build an authsvc over the same store.
func newTestServer(t *testing.T) (*Server, *sqlite.Adapter) {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	adapter, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })

	repoRoot := t.TempDir()
	now := time.Now().UTC()
	repo := domain.Repository{ID: "repo_t", Name: "t", RootPath: repoRoot, CreatedAt: now, UpdatedAt: now}
	if err := adapter.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	registry := sessions.SingleAgentRegistry("claude", claude.New(t.TempDir()))
	srv, err := New(repo, adapter, registry, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, adapter
}

// connect wires the server to an in-memory client session. Servers must be
// connected before clients (the client initializes the session on connect).
func connect(t *testing.T, srv *Server) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := sdk.NewInMemoryTransports()
	ss, err := srv.mcp.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestListToolsExposesTheReadIngestSurface(t *testing.T) {
	srv, _ := newTestServer(t)
	cs := connect(t, srv)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil input schema", tool.Name)
		}
	}
	for _, want := range []string{"mnemo_resume", "mnemo_list_tasks", "mnemo_task_state", "mnemo_ingest"} {
		if !got[want] {
			t.Errorf("missing tool %q (have %v)", want, got)
		}
	}
	// Read + ingest only — task mutation must not leak in.
	for _, banned := range []string{"mnemo_start_task", "mnemo_switch_task", "mnemo_done_task"} {
		if got[banned] {
			t.Errorf("unexpected mutating tool %q exposed", banned)
		}
	}
}

func TestListTasksToolRoundTrips(t *testing.T) {
	srv, _ := newTestServer(t)
	cs := connect(t, srv)

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "mnemo_list_tasks",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call mnemo_list_tasks: %v", err)
	}
	if res.IsError {
		t.Fatalf("mnemo_list_tasks reported tool error: %+v", res.Content)
	}
	if res.StructuredContent == nil {
		t.Fatal("expected structured content with a tasks field")
	}
}

func TestIngestToolRoundTrips(t *testing.T) {
	srv, _ := newTestServer(t)
	cs := connect(t, srv)

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "mnemo_ingest",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call mnemo_ingest: %v", err)
	}
	if res.IsError {
		t.Fatalf("mnemo_ingest reported tool error: %+v", res.Content)
	}
}

// TestStreamableHTTPRoundTrips drives the real Streamable HTTP transport: a
// client connects over HTTP to the SDK handler and lists tools.
func TestStreamableHTTPRoundTrips(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(srv.HTTPHandler(nil))
	t.Cleanup(ts.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("client connect over http: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools over http: %v", err)
	}
	if len(res.Tools) != 4 {
		t.Fatalf("expected 4 tools over http, got %d", len(res.Tools))
	}
}

// TestStreamableHTTPAuthGate proves the bearer gate wraps the transport while
// leaving the health probe open.
func TestStreamableHTTPAuthGate(t *testing.T) {
	srv, adapter := newTestServer(t)
	auth := authsvc.New(adapter, time.Hour)
	ts := httptest.NewServer(srv.HTTPHandler(auth))
	t.Cleanup(ts.Close)

	// Health probe is exempt from auth.
	hres, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	_ = hres.Body.Close()
	if hres.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", hres.StatusCode)
	}

	// An MCP request with no bearer token is rejected before reaching the
	// transport.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/", nil)
	req.Header.Set("Content-Type", "application/json")
	pres, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post without token: %v", err)
	}
	_ = pres.Body.Close()
	if pres.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated POST status = %d, want 401", pres.StatusCode)
	}
}
