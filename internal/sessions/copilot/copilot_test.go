package copilot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirrobot01/mnemo/internal/domain"
)

func TestCopilotDiscoverAndIngest(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	sdir := filepath.Join(home, "session-state", "session-1")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"workspace": repo,
		"messages": []any{
			map[string]any{"role": "user", "content": "fix the failing test"},
			map[string]any{"role": "assistant", "content": "updated the assertion"},
		},
	})
	if err := os.WriteFile(filepath.Join(sdir, "events.json"), body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	other := filepath.Join(home, "session-state", "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	_ = os.WriteFile(filepath.Join(other, "events.json"), []byte(`{"workspace":"/elsewhere","messages":[]}`), 0o644)

	ad := New(home)
	if ad.Kind() != domain.SessionKindCopilot {
		t.Fatalf("kind = %s", ad.Kind())
	}
	ds, err := ad.Discover(context.Background(), repo)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(ds) != 1 || ds[0].ExternalID != "session-1" {
		t.Fatalf("discover = %+v", ds)
	}
	ing, err := ad.Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ing.Events) != 2 || ing.Events[0].Type != domain.SessionEventTypeUserMessage || ing.Events[1].Content != "updated the assertion" {
		t.Fatalf("events = %+v", ing.Events)
	}
}

func TestCopilotWatchDirs(t *testing.T) {
	home := t.TempDir()
	dirs, err := New(home).WatchDirs(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("watch dirs: %v", err)
	}
	want := filepath.Join(home, "session-state")
	if len(dirs) != 1 || dirs[0] != want {
		t.Fatalf("watch dirs = %+v, want [%s]", dirs, want)
	}
}
