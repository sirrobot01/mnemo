package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Ignore is the parsed .mnemo/ignore opt-out. It excludes whole tools or
// specific session source paths from ingestion.
//
// Format (one entry per line; blank lines and `#` comments skipped):
//
//	claude                 # a bare known tool name → skip that tool entirely
//	*-experiment.jsonl     # a glob → matched against the session file name
//	sessions/2026/05/*      # a glob containing "/" → matched against full path
type Ignore struct {
	tools map[string]bool
	globs []string
}

var knownTools = map[string]bool{
	"claude": true, "codex": true, "cursor": true,
	"windsurf": true, "aider": true, "continue": true,
}

// LoadIgnore reads <repoRoot>/.mnemo/ignore. A missing file yields an empty
// (allow-all) Ignore, never an error.
func LoadIgnore(repoRoot string) (*Ignore, error) {
	ig := &Ignore{tools: map[string]bool{}}
	f, err := os.Open(filepath.Join(repoRoot, DefaultDir, "ignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return ig, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if knownTools[strings.ToLower(line)] {
			ig.tools[strings.ToLower(line)] = true
			continue
		}
		ig.globs = append(ig.globs, line)
	}
	return ig, sc.Err()
}

// SkipTool reports whether an entire tool's sessions are opted out.
func (i *Ignore) SkipTool(tool string) bool {
	if i == nil {
		return false
	}
	return i.tools[strings.ToLower(tool)]
}

// SkipPath reports whether a session source path matches an ignore glob.
// Patterns without "/" match the file name; patterns with "/" match the
// full path.
func (i *Ignore) SkipPath(p string) bool {
	if i == nil {
		return false
	}
	base := filepath.Base(p)
	for _, g := range i.globs {
		target := base
		if strings.Contains(g, "/") {
			target = p
		}
		if ok, err := filepath.Match(g, target); err == nil && ok {
			return true
		}
	}
	return false
}
