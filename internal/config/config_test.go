package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDefaultSchema(t *testing.T) {
	cfg, err := Parse(strings.NewReader(`database:
  type: sqlite
  dsn: .mnemo/mnemo.db
`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if cfg.Database.Type != "sqlite" {
		t.Fatalf("expected sqlite database type, got %q", cfg.Database.Type)
	}

	if cfg.Database.DSN != ".mnemo/mnemo.db" {
		t.Fatalf("unexpected DSN: %q", cfg.Database.DSN)
	}
}

func TestParseRejectsUnknownSection(t *testing.T) {
	_, err := Parse(strings.NewReader("unknown:\n  value: test\n"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSaveWritesConfigWithoutOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mnemo", "config.yaml")

	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if loaded.Database.Type != "sqlite" {
		t.Fatalf("expected sqlite database type, got %q", loaded.Database.Type)
	}

	if err := Save(path, Default()); err == nil {
		t.Fatal("expected overwrite protection error")
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to exist: %v", err)
	}
}
