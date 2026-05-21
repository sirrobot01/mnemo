package contextsvc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirrobot01/mnemo/internal/config"
)

func TestResolveFileAndDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("house rules: be terse"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgs := []config.ContextConfig{
		{Name: "rules", Type: "file", Path: "AGENTS.md"},
		{Name: "docs", Type: "dir", Path: "docs"},
	}
	svc := New(root, cfgs, false)
	out, err := svc.Resolve(context.Background(), cfgs)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(out) != 2 || !strings.Contains(out[0].Content, "be terse") || !strings.Contains(out[1].Content, "alpha") {
		t.Fatalf("unexpected resolved content: %+v", out)
	}
}

func TestContextRefDAGAndCycle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "base.md"), []byte("base content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// shared -> ref base (valid DAG).
	cfgs := []config.ContextConfig{
		{Name: "base", Type: "file", Path: "base.md"},
		{Name: "shared", Type: "context", Ref: "base"},
	}
	svc := New(root, cfgs, false)
	out, err := svc.Resolve(context.Background(), cfgs[1:])
	if err != nil {
		t.Fatalf("DAG resolve: %v", err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Content, "base content") {
		t.Fatalf("ref not followed: %+v", out)
	}

	// self-cycle must be rejected.
	cyc := []config.ContextConfig{{Name: "loop", Type: "context", Ref: "loop"}}
	scyc := New(root, cyc, false)
	if _, err := scyc.Resolve(context.Background(), cyc); err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestSecretsScrubbed(t *testing.T) {
	root := t.TempDir()
	body := "safe line\nAWS key: AKIAIOSFODNN7EXAMPLE\nmore safe"
	if err := os.WriteFile(filepath.Join(root, "c.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgs := []config.ContextConfig{{Name: "c", Type: "file", Path: "c.md"}}
	out, err := New(root, cfgs, false).Resolve(context.Background(), cfgs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out[0].Content, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("secret leaked into context: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %q", out[0].Content)
	}
}

func TestURLEgressGatedOffByDefault(t *testing.T) {
	cfgs := []config.ContextConfig{{Name: "remote", Type: "url", URL: "http://127.0.0.1:0/never"}}
	out, err := New(t.TempDir(), cfgs, false).Resolve(context.Background(), cfgs)
	if err != nil {
		t.Fatalf("Resolve should not error when URL egress is off: %v", err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Content, "withheld") {
		t.Fatalf("url context should be withheld, got: %+v", out)
	}
}
