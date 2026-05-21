package aider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirrobot01/mnemo/internal/domain"
)

func TestAiderDiscoverAndIngest(t *testing.T) {
	repo := t.TempDir()
	md := "# aider chat started\n\n#### add retry to billing.go\n\nSure, here is the change.\nMore detail.\n\n#### no, use the existing helper\n\nUpdated to reuse it.\n"
	if err := os.WriteFile(filepath.Join(repo, historyFile), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ad := New()
	if ad.Kind() != domain.SessionKindAider {
		t.Fatalf("tool = %s", ad.Kind())
	}
	ds, err := ad.Discover(context.Background(), repo)
	if err != nil || len(ds) != 1 {
		t.Fatalf("discover = %+v err=%v", ds, err)
	}
	ing, err := ad.Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if ing.Session.MessageCount < 3 {
		t.Fatalf("expected >=3 messages, got %d", ing.Session.MessageCount)
	}
	if ing.Events[0].Type != domain.SessionEventTypeUserMessage || ing.Events[0].Content != "add retry to billing.go" {
		t.Fatalf("event0 = %+v", ing.Events[0])
	}
}

func TestAiderNoHistory(t *testing.T) {
	ds, err := New().Discover(context.Background(), t.TempDir())
	if err != nil || len(ds) != 0 {
		t.Fatalf("expected no discoveries, got %+v err=%v", ds, err)
	}
}

func TestAiderWatchDirs(t *testing.T) {
	repo := t.TempDir()
	dirs, err := New().WatchDirs(context.Background(), repo)
	if err != nil {
		t.Fatalf("watch dirs: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("watch dirs = %+v", dirs)
	}
	if got, want := dirs[0], repo; got != want {
		t.Fatalf("watch dir = %q, want %q", got, want)
	}
}

func TestAiderKeepsAssistantCodeAndQuotes(t *testing.T) {
	repo := t.TempDir()
	md := "# aider chat started at 2026-05-18\n" +
		"> Tokens: 1.2k sent\n" +
		"> Added internal/x.go to the chat\n" +
		"#### add a guard clause\n" +
		"Here is the change:\n" +
		"```go\n" +
		"if x == nil {\n" +
		"> not a banner, a diff quote\n" +
		"}\n" +
		"```\n" +
		"Done.\n"
	if err := os.WriteFile(filepath.Join(repo, historyFile), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ds, err := New().Discover(context.Background(), repo)
	if err != nil || len(ds) != 1 {
		t.Fatalf("discover: %v %+v", err, ds)
	}
	ing, err := New().Ingest(context.Background(), ds[0].SourcePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ing.Events) != 2 {
		t.Fatalf("want user+assistant = 2 events, got %d: %+v", len(ing.Events), ing.Events)
	}
	if ing.Events[0].Type != domain.SessionEventTypeUserMessage || ing.Events[0].Content != "add a guard clause" {
		t.Fatalf("user turn wrong: %+v", ing.Events[0])
	}
	a := ing.Events[1].Content
	if ing.Events[1].Type != domain.SessionEventTypeAssistantMessage {
		t.Fatalf("want assistant, got %s", ing.Events[1].Type)
	}
	// The fenced code and the in-reply "> " quote must survive; the
	// pre-turn "> Tokens:" / "> Added" banners must not appear.
	for _, must := range []string{"```go", "if x == nil", "> not a banner, a diff quote", "Done."} {
		if !strings.Contains(a, must) {
			t.Fatalf("assistant reply lost %q:\n%s", must, a)
		}
	}
	if strings.Contains(a, "Tokens:") || strings.Contains(a, "Added internal/x.go") {
		t.Fatalf("pre-turn banner leaked into assistant reply:\n%s", a)
	}
}
