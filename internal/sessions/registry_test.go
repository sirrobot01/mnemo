package sessions

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/domain"
)

type fakeProvider struct {
	kind       domain.SessionKind
	discovered []Discovery
}

func (f fakeProvider) Kind() domain.SessionKind { return f.kind }
func (f fakeProvider) Ingest(context.Context, string) (Ingestion, error) {
	return Ingestion{Session: domain.Session{Kind: f.kind}}, nil
}
func (f fakeProvider) Discover(context.Context, string) ([]Discovery, error) {
	return f.discovered, nil
}

func TestKnownAgentDelegatesToProvider(t *testing.T) {
	prov := fakeProvider{kind: domain.SessionKindClaude, discovered: []Discovery{{SourcePath: "/x/s1.jsonl"}}}
	reg, err := NewRegistry(
		[]config.AgentConfig{{
			Name:         "claude",
			Kind:         "claude",
			Capabilities: []config.AgentCapability{config.CapabilityResumeCLI},
		}},
		map[domain.SessionKind]Provider{domain.SessionKindClaude: prov},
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	ds, err := reg.Discover(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(ds) != 1 || ds[0].Agent != "claude" || ds[0].Kind != domain.SessionKindClaude {
		t.Fatalf("delegated discovery wrong: %+v", ds)
	}
	if !reg.Agents()[0].Has(CapResumeCLI) {
		t.Fatal("capability not parsed")
	}
}

func TestCustomAgentRequiresParserAndSources(t *testing.T) {
	if _, err := NewRegistry([]config.AgentConfig{{Name: "bot", Kind: "weird"}}, nil); err == nil {
		t.Fatal("custom agent without parser should error")
	}
	if _, err := NewRegistry([]config.AgentConfig{{Name: "bot", Kind: "weird", Parser: "jsonl"}}, nil); err == nil {
		t.Fatal("custom agent without sources should error")
	}
	if _, err := NewRegistry([]config.AgentConfig{{Name: "bot", Kind: "weird", Parser: "nope", Sources: []string{"/x/*"}}}, nil); err == nil {
		t.Fatal("unknown parser should error")
	}
}

func TestCustomAgentGlobAndParse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	body := `{"role":"user","content":"hello"}` + "\n" + `{"role":"assistant","content":[{"type":"text","text":"hi"}]}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry([]config.AgentConfig{{
		Name: "bot", Kind: "jsonl-openai", Parser: "jsonl-openai",
		Sources: []string{filepath.Join(dir, "*.jsonl")},
	}}, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	ds, err := reg.Discover(context.Background(), dir)
	if err != nil || len(ds) != 1 || ds[0].SourcePath != path {
		t.Fatalf("custom glob discovery wrong: %+v err=%v", ds, err)
	}
	ing, err := reg.Agents()[0].Ingest(context.Background(), path)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(ing.Events) != 2 || ing.Events[0].Type != domain.SessionEventTypeUserMessage {
		t.Fatalf("generic parse wrong: %+v", ing.Events)
	}
}

func TestRepoTokenExpansion(t *testing.T) {
	repoRoot := "/home/u/proj"
	got, err := expandToken("~/.claude/projects/{repo}/*.jsonl", domain.SessionKindClaude, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Claude encodes the repo path with "/" → "-".
	if want := "-home-u-proj"; !contains(got, want) {
		t.Fatalf("claude {repo} expansion = %q, want substring %q", got, want)
	}
	got, err = expandToken("{repo}/.aider.chat.history.md", domain.SessionKindAider, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/u/proj/.aider.chat.history.md"; got != want {
		t.Fatalf("aider {repo} expansion = %q, want %q", got, want)
	}
}

func TestDuplicateAndMissingErrors(t *testing.T) {
	if _, err := NewRegistry([]config.AgentConfig{{Name: "a", Kind: "claude"}, {Name: "a", Kind: "claude"}},
		map[domain.SessionKind]Provider{domain.SessionKindClaude: fakeProvider{kind: domain.SessionKindClaude}}); err == nil {
		t.Fatal("duplicate agent name should error")
	}
	if _, err := NewRegistry([]config.AgentConfig{{Name: "", Kind: "claude"}}, nil); err == nil {
		t.Fatal("missing name should error")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
