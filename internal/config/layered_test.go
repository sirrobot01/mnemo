package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateHome points the global Mnemo home at a temp dir so tests never read
// or write the developer's real ~/.mnemo.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(HomeEnv, home)
	return home
}

func TestLoadLayeredProjectOverridesGlobal(t *testing.T) {
	home := isolateHome(t)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// Global: database (machine-level) only.
	global := "database:\n  type: sqlite\n  dsn: " + GlobalDBPath() + "\n"
	if err := os.WriteFile(GlobalConfigPath(), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}

	repoRoot := t.TempDir()
	projectPath := DefaultPath(repoRoot)
	project := `privacy:
  allow_cross_vendor_egress: true
agents:
  - name: claude
    kind: claude
    sources: ["~/.claude/projects/{repo}/*.jsonl"]
    capabilities: [resume.cli]
`
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadLayered(repoRoot)
	if err != nil {
		t.Fatalf("LoadLayered: %v", err)
	}
	if cfg.Database.Type != "sqlite" || cfg.Database.DSN != GlobalDBPath() {
		t.Fatalf("database not inherited from global: %+v", cfg.Database)
	}
	if !cfg.Privacy.AllowCrossVendorEgress {
		t.Fatalf("project privacy not applied")
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0].Name != "claude" {
		t.Fatalf("project agents not parsed: %+v", cfg.Agents)
	}
}

func TestLoadLayeredDefaultsWithNoFiles(t *testing.T) {
	isolateHome(t)
	cfg, err := LoadLayered(t.TempDir())
	if err != nil {
		t.Fatalf("LoadLayered with no files: %v", err)
	}
	if cfg.Database.Type != "sqlite" || cfg.Database.DSN != GlobalDBPath() {
		t.Fatalf("expected built-in defaults, got %+v", cfg.Database)
	}
}

func TestLoadLayeredStillRejectsUnknownKeys(t *testing.T) {
	isolateHome(t)
	repoRoot := t.TempDir()
	path := DefaultPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bogus:\n  x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLayered(repoRoot); err == nil {
		t.Fatal("expected unknown-key rejection in layered load")
	}
}

func TestSaveProjectOmitsMachineLevelKeys(t *testing.T) {
	isolateHome(t)
	path := filepath.Join(t.TempDir(), ".mnemo", "config.yaml")
	cfg := Config{
		Database: DatabaseConfig{Type: "sqlite", DSN: "/should/not/appear"},
		Agents:   []AgentConfig{{Name: "codex", Kind: "codex"}},
	}
	if err := SaveProject(path, cfg); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "database") || strings.Contains(string(body), "should/not/appear") {
		t.Fatalf("project config leaked machine-level database section:\n%s", body)
	}
	if !strings.Contains(string(body), "codex") {
		t.Fatalf("project config missing agents:\n%s", body)
	}
}

func TestSaveGlobalIsIdempotent(t *testing.T) {
	isolateHome(t)
	path := GlobalConfigPath()
	if err := SaveGlobal(path, Default()); err != nil {
		t.Fatalf("first SaveGlobal: %v", err)
	}
	// A second init in another project must not clobber or error.
	if err := SaveGlobal(path, Default()); err != nil {
		t.Fatalf("second SaveGlobal should be a no-op, got: %v", err)
	}
}
