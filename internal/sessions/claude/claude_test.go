package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirrobot01/mnemo/internal/domain"
)

func TestEncodeRepoRoot(t *testing.T) {
	got := EncodeRepoRoot("/Users/alice/cools/mnemo")
	want := "-Users-alice-cools-mnemo"
	if got != want {
		t.Fatalf("EncodeRepoRoot = %q, want %q", got, want)
	}
}

func TestDiscoverAndIngest(t *testing.T) {
	home := t.TempDir()
	repoRoot := "/private/var/tmp/example-project"
	projectsDir := filepath.Join(home, "projects", EncodeRepoRoot(repoRoot))
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	jsonl := `{"type":"permission-mode","permissionMode":"default","sessionId":"sess-1"}
{"type":"user","message":{"role":"user","content":"don't use GORM"},"timestamp":"2026-05-14T10:00:00.000Z","sessionId":"sess-1","cwd":"/private/var/tmp/example-project","gitBranch":"main","uuid":"u-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"understood"}]},"timestamp":"2026-05-14T10:00:05.000Z","sessionId":"sess-1","cwd":"/private/var/tmp/example-project","gitBranch":"main","uuid":"u-2"}
`
	sessionFile := filepath.Join(projectsDir, "sess-1.jsonl")
	if err := os.WriteFile(sessionFile, []byte(jsonl), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	adapter := New(home)
	if adapter.Tool() != domain.SessionToolClaude {
		t.Fatalf("Tool = %s, want claude", adapter.Tool())
	}

	discoveries, err := adapter.Discover(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discoveries) != 1 {
		t.Fatalf("Discover returned %d entries, want 1", len(discoveries))
	}
	if discoveries[0].ExternalID != "sess-1" {
		t.Fatalf("ExternalID = %q, want sess-1", discoveries[0].ExternalID)
	}
	if !strings.HasSuffix(discoveries[0].SourcePath, "sess-1.jsonl") {
		t.Fatalf("SourcePath = %q, want suffix sess-1.jsonl", discoveries[0].SourcePath)
	}

	ingestion, err := adapter.Ingest(context.Background(), discoveries[0].SourcePath)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if ingestion.Session.Branch != "main" {
		t.Fatalf("Branch = %q, want main", ingestion.Session.Branch)
	}
	if ingestion.Session.MessageCount != 2 {
		t.Fatalf("MessageCount = %d, want 2", ingestion.Session.MessageCount)
	}
	if got := len(ingestion.Events); got != 2 {
		t.Fatalf("len(events) = %d, want 2 (permission-mode is skipped)", got)
	}
	if ingestion.Events[0].Type != domain.SessionEventTypeUserMessage {
		t.Fatalf("event 0 type = %s, want user_message", ingestion.Events[0].Type)
	}
	if ingestion.Events[0].Content != "don't use GORM" {
		t.Fatalf("event 0 content = %q", ingestion.Events[0].Content)
	}
	if ingestion.Events[1].Type != domain.SessionEventTypeAssistantMessage {
		t.Fatalf("event 1 type = %s, want assistant_message", ingestion.Events[1].Type)
	}
	if ingestion.Events[1].Content != "understood" {
		t.Fatalf("event 1 content = %q", ingestion.Events[1].Content)
	}
}

func TestDiscoverMissingDir(t *testing.T) {
	home := t.TempDir()
	discoveries, err := New(home).Discover(context.Background(), "/no/such/repo")
	if err != nil {
		t.Fatalf("Discover should not fail on missing dir: %v", err)
	}
	if len(discoveries) != 0 {
		t.Fatalf("expected no discoveries, got %d", len(discoveries))
	}
}

func TestClaudeRealWorldVariance(t *testing.T) {
	home := t.TempDir()
	repoRoot := "/private/var/tmp/variance-proj"
	dir := filepath.Join(home, "projects", EncodeRepoRoot(repoRoot))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// summary line (skipped), permission-mode (skipped), a user line whose
	// content is an array, an assistant with mixed text/thinking/tool_use
	// blocks, and a system line with no timestamp.
	jsonl := `{"type":"summary","summary":"prior session"}
{"type":"permission-mode","permissionMode":"default","sessionId":"v1"}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"refactor please"}]},"timestamp":"2026-05-18T10:00:00.000Z","sessionId":"v1","gitBranch":"dev"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","text":"hmm"},{"type":"text","text":"on it"},{"type":"tool_use","name":"Edit","id":"t1"}]},"timestamp":"2026-05-18T10:00:05.000Z","sessionId":"v1","gitBranch":"dev"}
{"type":"system","content":"context compacted","sessionId":"v1"}
`
	if err := os.WriteFile(filepath.Join(dir, "v1.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ds, err := New(home).Discover(context.Background(), repoRoot)
	if err != nil || len(ds) != 1 {
		t.Fatalf("discover: %v %+v", err, ds)
	}
	ing, err := New(home).Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest must not fail on real-world variance: %v", err)
	}
	if ing.Session.Branch != "dev" {
		t.Fatalf("branch = %q, want dev", ing.Session.Branch)
	}
	if ing.Session.MessageCount != 2 {
		t.Fatalf("message count = %d, want 2 (user + assistant)", ing.Session.MessageCount)
	}
	var sawUser, sawAssistant, sawSystem bool
	for _, e := range ing.Events {
		switch e.Type {
		case domain.SessionEventTypeUserMessage:
			sawUser = true
			if e.Content != "refactor please" {
				t.Fatalf("array user content not flattened: %q", e.Content)
			}
		case domain.SessionEventTypeAssistantMessage:
			sawAssistant = true
			if e.Content != "on it" {
				t.Fatalf("assistant text block not extracted (got %q)", e.Content)
			}
		case domain.SessionEventTypeSystem:
			sawSystem = true
		}
	}
	if !sawUser || !sawAssistant || !sawSystem {
		t.Fatalf("missing event types: user=%v assistant=%v system=%v", sawUser, sawAssistant, sawSystem)
	}
}
