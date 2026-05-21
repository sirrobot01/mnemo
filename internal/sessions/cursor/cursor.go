// Package cursor implements the sessions.Adapter contract for Cursor.
//
// Cursor's public CLI docs expose launching/resuming agents, but do not
// document a stable local transcript schema. This adapter therefore uses a
// conservative structured extractor over a bounded Cursor agent-state
// directory and only accepts candidates that mention the current repository.
package cursor

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/sessions"
)

type Adapter struct {
	HomeDir string // defaults to ~/.cursor/agent
}

func New(homeDir string) *Adapter { return &Adapter{HomeDir: homeDir} }

func (a *Adapter) Kind() domain.SessionKind { return domain.SessionKindCursor }

func (a *Adapter) dir() (string, error) {
	if a.HomeDir != "" {
		return a.HomeDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "agent"), nil
}

func (a *Adapter) Discover(ctx context.Context, repoRoot string) ([]sessions.Discovery, error) {
	dir, err := a.dir()
	if err != nil {
		return nil, err
	}
	return sessions.DiscoverStructured(ctx, dir, repoRoot, domain.SessionKindCursor)
}

func (a *Adapter) Ingest(ctx context.Context, sourcePath string) (sessions.Ingestion, error) {
	return sessions.IngestStructured(ctx, sourcePath, domain.SessionKindCursor)
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
