package windsurf

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirrobot01/mnemo/internal/domain"
)

func TestWindsurfDiscoverAndIngest(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	body, _ := json.Marshal(map[string]any{
		"workspace": repo,
		"turns": []any{
			map[string]any{"role": "user", "content": "wire the billing route"},
			map[string]any{"role": "assistant", "content": "added the handler"},
		},
	})
	if err := os.WriteFile(filepath.Join(home, "devin-session.json"), body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = os.WriteFile(filepath.Join(home, "other.json"), []byte(`{"workspace":"/elsewhere","turns":[]}`), 0o644)

	ad := New(home)
	if ad.Kind() != domain.SessionKindWindsurf {
		t.Fatalf("kind = %s", ad.Kind())
	}
	ds, err := ad.Discover(context.Background(), repo)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(ds) != 1 || ds[0].ExternalID != "devin-session" {
		t.Fatalf("discover = %+v", ds)
	}
	ing, err := ad.Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ing.Events) != 2 || ing.Events[0].Type != domain.SessionEventTypeUserMessage || ing.Events[1].Content != "added the handler" {
		t.Fatalf("events = %+v", ing.Events)
	}
}

func TestWindsurfWatchDirs(t *testing.T) {
	home := t.TempDir()
	dirs, err := New(home).WatchDirs(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("watch dirs: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != home {
		t.Fatalf("watch dirs = %+v, want [%s]", dirs, home)
	}
}
