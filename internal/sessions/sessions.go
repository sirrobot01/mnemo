// Package sessions defines the contract that session adapters implement.
//
// A session adapter knows the on-disk layout of one AI coding tool
// (~/.claude, ~/.codex, ~/.cursor, ...). It discovers which session files
// belong to a given repository working directory and parses each file into a
// unified (Session, []SessionEvent) pair that downstream services can persist
// and analyse.
//
// Adapters are observation-only. They never modify the original session
// files and they never project content directly into projection sidecars.
package sessions

import (
	"context"

	"github.com/sirrobot01/mnemo/internal/domain"
)

// Discovery is the result of scanning a tool's home directory for sessions
// belonging to a particular repository root.
type Discovery struct {
	Tool       domain.SessionTool
	SourcePath string
	ExternalID string
}

// Ingestion is the parsed representation of a single session file.
type Ingestion struct {
	Session domain.Session
	Events  []domain.SessionEvent
}

// DirWatcher is an optional capability: an adapter that knows which
// directories to watch for new/changed session files implements it so
// `mnemo watch` can tail them. Adapters without it are still ingestible via
// one-shot `mnemo ingest`.
type DirWatcher interface {
	// WatchDirs returns the directories whose changes indicate new session
	// activity for the given repository root. Returned paths may not exist
	// yet; the caller watches the nearest existing ancestor.
	WatchDirs(ctx context.Context, repoRoot string) ([]string, error)
}

// Adapter is the contract a per-tool session source must satisfy.
type Adapter interface {
	// Tool returns the SessionTool this adapter serves.
	Tool() domain.SessionTool

	// Discover returns every session file the adapter can find for the given
	// repository root. Implementations must be deterministic in ordering and
	// must not mutate the source files.
	Discover(ctx context.Context, repoRoot string) ([]Discovery, error)

	// Ingest parses one session source path discovered by Discover and
	// returns the canonical Session + ordered SessionEvent slice. The
	// returned Session.RepoID is left empty for the caller to populate.
	Ingest(ctx context.Context, sourcePath string) (Ingestion, error)
}
