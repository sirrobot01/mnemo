package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRootCommandSurface(t *testing.T) {
	cmd, err := NewRootCommand()
	if err != nil {
		t.Fatalf("NewRootCommand: %v", err)
	}
	got := map[string]bool{}
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}
	for _, want := range []string{"init", "ingest", "watch", "task", "resume", "status", "forget", "serve", "db", "version"} {
		if !got[want] {
			t.Errorf("missing command %q", want)
		}
	}
	// Only the current command surface should be registered.
	for _, gone := range []string{"scan", "propose", "approve", "remember", "sync", "sessions"} {
		if got[gone] {
			t.Errorf("removed command %q still registered", gone)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	cmd, err := NewRootCommand()
	if err != nil {
		t.Fatalf("NewRootCommand: %v", err)
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.Contains(out.String(), "mnemo") {
		t.Errorf("unexpected version output: %q", out.String())
	}
}
