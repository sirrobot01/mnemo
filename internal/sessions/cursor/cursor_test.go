package cursor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirrobot01/mnemo/internal/domain"
)

func TestCursorDiscoverAndIngest(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	body, _ := json.Marshal(map[string]any{
		"cwd": repo,
		"messages": []any{
			map[string]any{"role": "user", "content": "inspect the sidebar"},
			map[string]any{"role": "assistant", "content": "found the layout issue"},
		},
	})
	if err := os.WriteFile(filepath.Join(home, "cursor-session.json"), body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = os.WriteFile(filepath.Join(home, "other.json"), []byte(`{"cwd":"/elsewhere","messages":[]}`), 0o644)

	ad := New(home)
	if ad.Kind() != domain.SessionKindCursor {
		t.Fatalf("kind = %s", ad.Kind())
	}
	ds, err := ad.Discover(context.Background(), repo)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(ds) != 1 || ds[0].ExternalID != "cursor-session" {
		t.Fatalf("discover = %+v", ds)
	}
	ing, err := ad.Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ing.Events) != 2 || ing.Events[0].Content != "inspect the sidebar" || ing.Events[1].Type != domain.SessionEventTypeAssistantMessage {
		t.Fatalf("events = %+v", ing.Events)
	}
}

func TestCursorWatchDirs(t *testing.T) {
	home := t.TempDir()
	dirs, err := New(home).WatchDirs(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("watch dirs: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != home {
		t.Fatalf("watch dirs = %+v, want [%s]", dirs, home)
	}
}
