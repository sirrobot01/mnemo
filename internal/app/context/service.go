// Package contextsvc resolves the project's configured contexts into
// already-scrubbed text for the resume handoff.
//
// A context is non-session knowledge: a file (AGENTS.md), a directory of
// docs, a URL, or a reference to another context. Context-type entries form a
// DAG resolved with cycle detection and a depth cap. URL fetches are egress
// and are gated off by default. All gathered content is secret-scanned before
// it can leave the local process — contexts are inputs only; Mnemo never
// writes back into the source.
package contextsvc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/safety"
)

const (
	maxDepth        = 8
	maxEntryChars   = 8000
	maxTotalChars   = 32000
	maxDirFiles     = 20
	urlFetchTimeout = 10 * time.Second
)

// Resolved is one context's scrubbed content.
type Resolved struct {
	Name    string
	Type    string
	Content string
}

// Service resolves contexts for a repository.
type Service struct {
	repoRoot string
	byName   map[string]config.ContextConfig
	allowURL bool
	client   *http.Client
}

// New builds a resolver over the configured contexts. allowURL gates the
// network: when false (the default), url contexts resolve to a placeholder
// rather than fetching.
func New(repoRoot string, contexts []config.ContextConfig, allowURL bool) *Service {
	byName := make(map[string]config.ContextConfig, len(contexts))
	for _, c := range contexts {
		byName[c.Name] = c
	}
	return &Service{
		repoRoot: repoRoot,
		byName:   byName,
		allowURL: allowURL,
		client:   &http.Client{Timeout: urlFetchTimeout},
	}
}

// Resolve expands every configured context (following context refs as a
// cycle-checked DAG) and returns scrubbed content in declaration order.
func (s *Service) Resolve(ctx context.Context, contexts []config.ContextConfig) ([]Resolved, error) {
	var out []Resolved
	total := 0
	for _, c := range contexts {
		content, err := s.resolveOne(ctx, c, map[string]bool{}, 0)
		if err != nil {
			return nil, fmt.Errorf("context %q: %w", c.Name, err)
		}
		content = scrub(content, maxEntryChars)
		if content == "" {
			continue
		}
		if total+len(content) > maxTotalChars {
			room := maxTotalChars - total
			if room < 0 {
				room = 0
			}
			content = content[:room]
		}
		total += len(content)
		out = append(out, Resolved{Name: c.Name, Type: string(c.Type), Content: content})
		if total >= maxTotalChars {
			break
		}
	}
	return out, nil
}

// Render flattens resolved contexts into the block resumesvc appends.
func (s *Service) Render(ctx context.Context, contexts []config.ContextConfig) (string, error) {
	resolved, err := s.Resolve(ctx, contexts)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, r := range resolved {
		fmt.Fprintf(&b, "### %s (%s)\n%s\n", r.Name, r.Type, strings.TrimSpace(r.Content))
	}
	return b.String(), nil
}

func (s *Service) resolveOne(ctx context.Context, c config.ContextConfig, seen map[string]bool, depth int) (string, error) {
	if depth > maxDepth {
		return "", fmt.Errorf("context nesting exceeds depth %d", maxDepth)
	}
	switch config.ContextType(strings.ToLower(strings.TrimSpace(string(c.Type)))) {
	case config.ContextFile:
		return readFileCapped(s.abs(c.Path))
	case config.ContextDir:
		return s.readDir(s.abs(c.Path))
	case config.ContextURL:
		if !s.allowURL {
			return fmt.Sprintf("(url context %q withheld — URL egress disabled; set privacy.allow_context_url_egress)", c.URL), nil
		}
		return s.fetchURL(ctx, c.URL)
	case config.ContextReference:
		if seen[c.Name] {
			return "", fmt.Errorf("context cycle detected at %q", c.Name)
		}
		seen[c.Name] = true
		ref, ok := s.byName[c.Ref]
		if !ok {
			return "", fmt.Errorf("unknown context ref %q", c.Ref)
		}
		return s.resolveOne(ctx, ref, seen, depth+1)
	default:
		return "", fmt.Errorf("unknown context type %q (want file|dir|url|context)", c.Type)
	}
}

func (s *Service) abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.repoRoot, p)
}

func readFileCapped(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxEntryChars+1))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Service) readDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for i, name := range names {
		if i >= maxDirFiles {
			break
		}
		content, err := readFileCapped(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "--- %s ---\n%s\n", name, content)
	}
	return b.String(), nil
}

func (s *Service) fetchURL(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("url context returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxEntryChars+1))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// scrub drops any line that trips the secret scanner, then truncates. This is
// the same boundary ingestion and enrichment enforce.
func scrub(value string, maxChars int) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if safety.RejectIfSecret(line) != nil {
			lines[i] = "[REDACTED]"
		}
	}
	out := strings.TrimSpace(strings.Join(lines, "\n"))
	if maxChars > 0 && len(out) > maxChars {
		out = strings.TrimSpace(out[:maxChars])
	}
	return out
}
