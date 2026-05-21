package sessions

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/domain"
)

// Agent is a configured, named instance of a kind. Sources, when set,
// override the built-in provider's discovery with explicit globs.
type Agent struct {
	Name         string
	Kind         domain.SessionKind
	Capabilities []Capability
	Sources      []string

	parser   Parser
	discover Discoverer // built-in layout knowledge; nil for pure-custom
}

// Has reports whether the agent declares the given capability.
func (a *Agent) Has(c Capability) bool {
	for _, have := range a.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// Registry is the project's effective agent set: config layered over the
// built-in kind providers.
type Registry struct {
	agents []*Agent
}

// NewRegistry builds the registry from the configured agents. providers maps
// a known kind to its built-in Provider (injected by the caller so the
// sessions package never imports the adapter packages — that would cycle).
// A custom agent (kind without a built-in provider) must declare a parser
// kind and explicit sources.
func NewRegistry(cfgs []config.AgentConfig, providers map[domain.SessionKind]Provider) (*Registry, error) {
	reg := &Registry{}
	seen := map[string]bool{}
	for _, c := range cfgs {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			return nil, fmt.Errorf("agent entry is missing a name")
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate agent name %q", name)
		}
		seen[name] = true

		kind := domain.SessionKind(strings.TrimSpace(string(c.Kind)))
		if kind == "" {
			return nil, fmt.Errorf("agent %q is missing a kind", name)
		}

		agent := &Agent{
			Name:         name,
			Kind:         kind,
			Capabilities: parseCapabilities(c.Capabilities),
			Sources:      c.Sources,
		}

		if p, ok := providers[kind]; ok {
			agent.parser = p
			agent.discover = p
		} else {
			// Custom agent: parser kind selects a generic parser, and it
			// must point at its own transcripts.
			parserKind := domain.SessionKind(strings.TrimSpace(string(c.Parser)))
			if parserKind == "" {
				return nil, fmt.Errorf("custom agent %q must set a parser", name)
			}
			gp, err := genericParser(parserKind)
			if err != nil {
				return nil, fmt.Errorf("agent %q: %w", name, err)
			}
			agent.parser = gp
			if len(agent.Sources) == 0 {
				return nil, fmt.Errorf("custom agent %q must declare sources", name)
			}
		}
		reg.agents = append(reg.agents, agent)
	}
	return reg, nil
}

// SingleAgentRegistry wraps one built-in provider as a one-agent registry.
// It is the convenient form for callers and tests that drive a single kind.
func SingleAgentRegistry(name string, p Provider) *Registry {
	return &Registry{agents: []*Agent{{
		Name:     name,
		Kind:     p.Kind(),
		parser:   p,
		discover: p,
	}}}
}

// Agents returns the configured agents in declaration order.
func (r *Registry) Agents() []*Agent { return r.agents }

// Discover returns every transcript across all agents. An agent with
// explicit Sources globs the filesystem; otherwise it delegates to the
// built-in provider's repo-scoped discovery (which knows tool-specific
// layout and cwd matching).
func (r *Registry) Discover(ctx context.Context, repoRoot string) ([]Discovery, error) {
	var out []Discovery
	for _, agent := range r.agents {
		ds, err := agent.Discover(ctx, repoRoot)
		if err != nil {
			return nil, fmt.Errorf("agent %q: %w", agent.Name, err)
		}
		out = append(out, ds...)
	}
	return out, nil
}

// Discover returns the transcripts for this single agent.
func (a *Agent) Discover(ctx context.Context, repoRoot string) ([]Discovery, error) {
	if len(a.Sources) > 0 {
		paths, err := expandSources(a.Sources, a.Kind, repoRoot)
		if err != nil {
			return nil, err
		}
		out := make([]Discovery, 0, len(paths))
		for _, p := range paths {
			out = append(out, Discovery{Agent: a.Name, Kind: a.Kind, SourcePath: p})
		}
		return out, nil
	}
	if a.discover == nil {
		return nil, fmt.Errorf("no sources and no built-in discovery")
	}
	ds, err := a.discover.Discover(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	for i := range ds {
		ds[i].Agent = a.Name
		ds[i].Kind = a.Kind
	}
	return ds, nil
}

// Ingest parses one discovered transcript with the agent's parser.
func (a *Agent) Ingest(ctx context.Context, sourcePath string) (Ingestion, error) {
	return a.parser.Ingest(ctx, sourcePath)
}

// WatchTargets returns directories `mnemo watch` should tail across all
// agents. Built-in providers that implement DirWatcher contribute their
// known dirs; agents with explicit sources contribute each glob's base dir.
func (r *Registry) WatchTargets(ctx context.Context, repoRoot string) ([]string, error) {
	seen := map[string]bool{}
	var dirs []string
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	for _, agent := range r.agents {
		if len(agent.Sources) > 0 {
			for _, src := range agent.Sources {
				expanded, err := expandToken(src, agent.Kind, repoRoot)
				if err != nil {
					return nil, err
				}
				add(globBase(expanded))
			}
			continue
		}
		if w, ok := agent.discover.(DirWatcher); ok {
			ds, err := w.WatchDirs(ctx, repoRoot)
			if err != nil {
				return nil, err
			}
			for _, d := range ds {
				add(d)
			}
		}
	}
	return dirs, nil
}

func parseCapabilities(in []config.AgentCapability) []Capability {
	out := make([]Capability, 0, len(in))
	for _, c := range in {
		if value := strings.TrimSpace(string(c)); value != "" {
			out = append(out, Capability(value))
		}
	}
	return out
}

// expandSources expands ~ and the {repo} token, then globs (supporting a
// trailing/embedded ** segment for recursive matches).
func expandSources(sources []string, kind domain.SessionKind, repoRoot string) ([]string, error) {
	var matched []string
	for _, src := range sources {
		pattern, err := expandToken(src, kind, repoRoot)
		if err != nil {
			return nil, err
		}
		paths, err := globRecursive(pattern)
		if err != nil {
			return nil, err
		}
		matched = append(matched, paths...)
	}
	return matched, nil
}

func expandToken(src string, kind domain.SessionKind, repoRoot string) (string, error) {
	s := strings.TrimSpace(src)
	if strings.HasPrefix(s, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		s = filepath.Join(home, strings.TrimPrefix(s, "~"))
	}
	if strings.Contains(s, "{repo}") {
		abs, err := filepath.Abs(repoRoot)
		if err != nil {
			return "", err
		}
		token := abs
		if kind == domain.SessionKindClaude {
			// Claude names its projects dir after the repo path with every
			// "/" replaced by "-".
			token = strings.ReplaceAll(abs, "/", "-")
		}
		s = strings.ReplaceAll(s, "{repo}", token)
	}
	return s, nil
}

// globRecursive supports filepath.Glob plus a single "**" segment meaning
// "this directory and any descendant".
func globRecursive(pattern string) ([]string, error) {
	idx := strings.Index(pattern, "**")
	if idx < 0 {
		return filepath.Glob(pattern)
	}
	base := filepath.Dir(pattern[:idx])
	tail := strings.TrimLeft(pattern[idx+2:], string(os.PathSeparator))
	var out []string
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if tail == "" {
			out = append(out, path)
			return nil
		}
		if ok, _ := filepath.Match(tail, filepath.Base(path)); ok {
			out = append(out, path)
		}
		return nil
	})
	return out, nil
}

// globBase is the deepest non-glob ancestor directory of a pattern — the dir
// `mnemo watch` should observe.
func globBase(pattern string) string {
	if i := strings.IndexAny(pattern, "*?["); i >= 0 {
		return filepath.Dir(pattern[:i])
	}
	return filepath.Dir(pattern)
}
