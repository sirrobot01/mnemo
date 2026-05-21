// Package copilot implements the sessions.Adapter contract for GitHub
// Copilot CLI.
//
// Copilot CLI stores local session state under ~/.copilot/session-state. The
// public docs describe the location, but not a stable event schema, so this
// adapter uses Mnemo's tolerant structured extractor and only discovers
// candidates that mention the current repository path.
package copilot

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/sessions"
)

type Adapter struct {
	HomeDir string // defaults to ~/.copilot
}

func New(homeDir string) *Adapter { return &Adapter{HomeDir: homeDir} }

func (a *Adapter) Kind() domain.SessionKind { return domain.SessionKindCopilot }

func (a *Adapter) dir() (string, error) {
	if a.HomeDir != "" {
		return filepath.Join(a.HomeDir, "session-state"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".copilot", "session-state"), nil
}

func (a *Adapter) Discover(ctx context.Context, repoRoot string) ([]sessions.Discovery, error) {
	dir, err := a.dir()
	if err != nil {
		return nil, err
	}
	return sessions.DiscoverStructured(ctx, dir, repoRoot, domain.SessionKindCopilot)
}

func (a *Adapter) Ingest(ctx context.Context, sourcePath string) (sessions.Ingestion, error) {
	return sessions.IngestStructured(ctx, sourcePath, domain.SessionKindCopilot)
}

func (a *Adapter) WatchDirs(ctx context.Context, repoRoot string) ([]string, error) {
	dir, err := a.dir()
	if err != nil {
		return nil, err
	}
	return []string{dir}, nil
}

var _ sessions.Provider = (*Adapter)(nil)
var _ sessions.DirWatcher = (*Adapter)(nil)
