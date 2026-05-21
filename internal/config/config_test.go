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

func TestParseEnrichmentConfig(t *testing.T) {
	cfg, err := Parse(strings.NewReader(`database:
  type: sqlite
  dsn: .mnemo/mnemo.db
enrichment:
  enabled: true
  provider: openai_compatible
  base_url: http://localhost:1234/v1
  model: qwen2.5-coder
  timeout: 15s
  max_events: 40
  max_event_chars: 1200
  max_input_chars: 30000
  max_output_tokens: 900
  temperature: 0.1
`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !cfg.Enrichment.Enabled || cfg.Enrichment.Provider != "openai_compatible" {
		t.Fatalf("unexpected enrichment config: %+v", cfg.Enrichment)
	}
	if cfg.Enrichment.Model != "qwen2.5-coder" || cfg.Enrichment.MaxEvents != 40 {
		t.Fatalf("enrichment fields not parsed: %+v", cfg.Enrichment)
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
