package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirrobot01/mnemo/internal/domain"
)

func writeRollout(t *testing.T, home, repoRoot string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "05", "17")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jsonl := `{"type":"session_meta","timestamp":"2026-05-17T12:00:00Z","payload":{"id":"cx-1","timestamp":"2026-05-17T12:00:00Z","cwd":"` + repoRoot + `","git":{"branch":"main","commit_hash":"abc123","repository_url":"git@github.com:acme/widget.git"}}}
{"type":"turn_context","payload":{}}
{"type":"response_item","timestamp":"2026-05-17T12:00:05Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"add retry to internal/billing/paystack.go"}]}}
{"type":"response_item","timestamp":"2026-05-17T12:00:09Z","payload":{"type":"reasoning","content":[{"type":"text","text":"thinking about backoff"}]}}
{"type":"response_item","timestamp":"2026-05-17T12:00:12Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done. Added exponential backoff."}]}}
`
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-05-17-cx-1.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCodexDiscoverAndIngest(t *testing.T) {
	home := t.TempDir()
	repoRoot := t.TempDir()
	writeRollout(t, home, repoRoot)

	ad := New(home)
	if ad.Kind() != domain.SessionKindCodex {
		t.Fatalf("tool = %s", ad.Kind())
	}
	ds, err := ad.Discover(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(ds) != 1 || ds[0].ExternalID != "cx-1" {
		t.Fatalf("discover = %+v", ds)
	}
	ing, err := ad.Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if ing.Session.Branch != "main" || ing.Session.CommitHash != "abc123" {
		t.Fatalf("meta not parsed: %+v", ing.Session)
	}
	if ing.Session.MessageCount != 2 {
		t.Fatalf("message count = %d, want 2", ing.Session.MessageCount)
	}
	// user, reasoning(thinking), assistant = 3 events
	if len(ing.Events) != 3 {
		t.Fatalf("events = %d, want 3", len(ing.Events))
	}
	if ing.Events[0].Type != domain.SessionEventTypeUserMessage || ing.Events[0].Content != "add retry to internal/billing/paystack.go" {
		t.Fatalf("event0 = %+v", ing.Events[0])
	}
	if ing.Events[1].Type != domain.SessionEventTypeThinking {
		t.Fatalf("event1 type = %s, want thinking", ing.Events[1].Type)
	}
	if ing.Events[2].Type != domain.SessionEventTypeAssistantMessage {
		t.Fatalf("event2 type = %s", ing.Events[2].Type)
	}
}

func TestCodexDiscoverIgnoresOtherRepos(t *testing.T) {
	home := t.TempDir()
	writeRollout(t, home, "/some/other/repo")
	ds, err := New(home).Discover(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(ds) != 0 {
		t.Fatalf("expected no discoveries for unrelated repo, got %d", len(ds))
	}
}

func TestCodexReasoningSummaryFallback(t *testing.T) {
	home := t.TempDir()
	repoRoot := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "05", "18")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jsonl := `{"type":"session_meta","timestamp":"2026-05-18T12:00:00Z","payload":{"id":"rx","cwd":"` + repoRoot + `","git":{"branch":"main"}}}
{"type":"response_item","timestamp":"2026-05-18T12:00:01Z","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"weighing two designs"}]}}
{"type":"response_item","timestamp":"2026-05-18T12:00:02Z","payload":{"type":"message","role":"assistant","content":"picked design B"}}
`
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-05-18-rx.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ds, err := New(home).Discover(context.Background(), repoRoot)
	if err != nil || len(ds) != 1 {
		t.Fatalf("discover: %v %+v", err, ds)
	}
	ing, err := New(home).Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ing.Events) != 2 {
		t.Fatalf("want 2 events, got %d", len(ing.Events))
	}
	if ing.Events[0].Type != domain.SessionEventTypeThinking || ing.Events[0].Content != "weighing two designs" {
		t.Fatalf("reasoning summary not extracted: %+v", ing.Events[0])
	}
	if ing.Events[1].Content != "picked design B" {
		t.Fatalf("assistant string content not extracted: %+v", ing.Events[1])
	}
}
