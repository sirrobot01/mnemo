// Package repo resolves a stable repository root and identity from any
// working directory inside a project.
//
// The previous design keyed every store on os.Getwd(), so running mnemo from
// a subdirectory created a second, empty brain for the same project. This
// package walks up to the git toplevel and derives an identity that is stable
// across subdirectories (and across clones, when a remote URL is present), so
// one logical repository maps to exactly one set of rows in the single DB.
package repo

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Info is the resolved identity of a project.
type Info struct {
	// Root is the canonical git toplevel (or the canonical start dir when
	// the path is not inside a git work tree).
	Root string
	// Identity is the stable key the repository ID is derived from: the
	// origin remote URL when available, otherwise the canonical Root.
	Identity string
}

// Resolve walks up from start to find the git toplevel. When start is not
// inside a git work tree it falls back to the canonical start directory. The
// returned Root is always an absolute, symlink-resolved path.
func Resolve(start string) (Info, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return Info{}, err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	gitRoot, ok := findGitRoot(abs)
	if !ok {
		return Info{Root: abs, Identity: abs}, nil
	}
	identity := gitRoot
	if remote := originRemote(gitRoot); remote != "" {
		identity = normalizeRemote(remote)
	}
	return Info{Root: gitRoot, Identity: identity}, nil
}

// findGitRoot returns the nearest ancestor (inclusive) containing a .git
// entry. .git may be a directory (normal clone) or a file (worktree/submodule).
func findGitRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// originRemote reads .git/config and returns the origin fetch URL if set. It
// is intentionally a tiny hand parser: shelling out to git would add a
// process dependency to a path that runs on every command.
func originRemote(gitRoot string) string {
	file, err := os.Open(filepath.Join(gitRoot, ".git", "config"))
	if err != nil {
		return ""
	}
	defer file.Close()

	inOrigin := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if inOrigin && strings.HasPrefix(line, "url") {
			if _, value, ok := strings.Cut(line, "="); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

// normalizeRemote canonicalizes a remote URL so https and ssh forms of the
// same repo collapse to one identity: host/path, no scheme, no .git suffix,
// lowercased.
func normalizeRemote(remote string) string {
	r := strings.TrimSpace(remote)
	r = strings.TrimSuffix(r, ".git")
	if i := strings.Index(r, "://"); i >= 0 {
		r = r[i+3:]
		if at := strings.LastIndex(r, "@"); at >= 0 {
			r = r[at+1:]
		}
	} else if at := strings.Index(r, "@"); at >= 0 {
		// scp-like syntax: git@host:owner/repo
		r = r[at+1:]
		r = strings.Replace(r, ":", "/", 1)
	}
	return strings.ToLower(strings.Trim(r, "/"))
}
