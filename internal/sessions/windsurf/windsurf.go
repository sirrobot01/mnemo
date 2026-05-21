// Package windsurf implements the sessions.Adapter contract for Windsurf's
// Devin-based terminal workflow.
//
// Devin/Windsurf CLI sessions do not currently have a documented stable local
// event schema, so discovery is bounded and repo-scoped and parsing uses
// Mnemo's tolerant structured extractor.
package windsurf

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/sessions"
)

type Adapter struct {
	HomeDir string // defaults to ~/.devin
}

func New(homeDir string) *Adapter { return &Adapter{HomeDir: homeDir} }

func (a *Adapter) Kind() domain.SessionKind { return domain.SessionKindWindsurf }

func (a *Adapter) dirs() ([]string, error) {
	if a.HomeDir != "" {
		return []string{a.HomeDir}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return []string{
		filepath.Join(home, ".devin"),
		filepath.Join(home, ".config", "devin"),
	}, nil
}

func (a *Adapter) Discover(ctx context.Context, repoRoot string) ([]sessions.Discovery, error) {
	dirs, err := a.dirs()
	if err != nil {
		return nil, err
	}
	var out []sessions.Discovery
	for _, dir := range dirs {
		found, err := sessions.DiscoverStructured(ctx, dir, repoRoot, domain.SessionKindWindsurf)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

func (a *Adapter) Ingest(ctx context.Context, sourcePath string) (sessions.Ingestion, error) {
	return sessions.IngestStructured(ctx, sourcePath, domain.SessionKindWindsurf)
}

func (a *Adapter) WatchDirs(ctx context.Context, repoRoot string) ([]string, error) {
	return a.dirs()
}

var _ sessions.Provider = (*Adapter)(nil)
var _ sessions.DirWatcher = (*Adapter)(nil)
