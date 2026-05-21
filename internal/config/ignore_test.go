package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIgnoreMissingFileAllowsAll(t *testing.T) {
	ig, err := LoadIgnore(t.TempDir())
	if err != nil {
		t.Fatalf("LoadIgnore: %v", err)
	}
	if ig.SkipAgent("claude") || ig.SkipPath("/x/s.jsonl") {
		t.Fatal("missing .mnemo/ignore must allow everything")
	}
}

func TestLoadIgnoreParsesToolsAndGlobs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DefaultDir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "# comment\n\ncodex\n*-experiment.jsonl\nsessions/skip/*\n"
	if err := os.WriteFile(filepath.Join(root, DefaultDir, "ignore"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ig, err := LoadIgnore(root)
	if err != nil {
		t.Fatalf("LoadIgnore: %v", err)
	}
	if !ig.SkipAgent("codex") || ig.SkipAgent("Claude") {
		t.Fatalf("tool skip wrong: codex=%v claude=%v", ig.SkipAgent("codex"), ig.SkipAgent("Claude"))
	}
	if !ig.SkipPath("/home/u/foo-experiment.jsonl") {
		t.Fatal("filename glob should match")
	}
	if !ig.SkipPath("sessions/skip/a.jsonl") {
		t.Fatal("path glob with / should match full path")
	}
	if ig.SkipPath("/home/u/keep.jsonl") {
		t.Fatal("non-matching path must not be skipped")
	}
}
