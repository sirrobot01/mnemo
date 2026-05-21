package continueide

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirrobot01/mnemo/internal/domain"
)

func TestContinueDiscoverAndIngest(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	sdir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sess := map[string]any{
		"sessionId":          "cont-1",
		"workspaceDirectory": repo,
		"history": []any{
			map[string]any{"message": map[string]any{"role": "user", "content": "add a cache to internal/cache.go"}},
			map[string]any{"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Done."}}}},
		},
	}
	b, _ := json.Marshal(sess)
	if err := os.WriteFile(filepath.Join(sdir, "cont-1.json"), b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A session for another workspace must be ignored.
	other := map[string]any{"sessionId": "x", "workspaceDirectory": "/elsewhere", "history": []any{}}
	ob, _ := json.Marshal(other)
	_ = os.WriteFile(filepath.Join(sdir, "x.json"), ob, 0o644)

	ad := New(home)
	ds, err := ad.Discover(context.Background(), repo)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(ds) != 1 || ds[0].ExternalID != "cont-1" {
		t.Fatalf("discover = %+v", ds)
	}
	ing, err := ad.Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if ing.Session.MessageCount != 2 {
		t.Fatalf("message count = %d, want 2", ing.Session.MessageCount)
	}
	if ing.Events[0].Type != domain.SessionEventTypeUserMessage || ing.Events[1].Content != "Done." {
		t.Fatalf("events = %+v", ing.Events)
	}
}

func TestContinueWorkspaceAltAndFlatMessages(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	sdir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// "workspace" key (not workspaceDirectory) + flat "messages" with string content.
	sess := map[string]any{
		"sessionId": "c2",
		"workspace": repo,
		"messages": []any{
			map[string]any{"role": "user", "content": "add a test"},
			map[string]any{"role": "assistant", "content": "added it"},
		},
	}
	b, _ := json.Marshal(sess)
	if err := os.WriteFile(filepath.Join(sdir, "c2.json"), b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ds, err := New(home).Discover(context.Background(), repo)
	if err != nil || len(ds) != 1 || ds[0].ExternalID != "c2" {
		t.Fatalf("discover via alt workspace key failed: %v %+v", err, ds)
	}
	ing, err := New(home).Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if ing.Session.MessageCount != 2 || len(ing.Events) != 2 || ing.Events[1].Content != "added it" {
		t.Fatalf("flat messages not parsed: %+v", ing.Events)
	}
}

func TestContinueWatchDirs(t *testing.T) {
	home := t.TempDir()
	dirs, err := New(home).WatchDirs(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("watch dirs: %v", err)
	}
	want := filepath.Join(home, "sessions")
	if len(dirs) != 1 || dirs[0] != want {
		t.Fatalf("watch dirs = %+v, want [%s]", dirs, want)
	}
}
