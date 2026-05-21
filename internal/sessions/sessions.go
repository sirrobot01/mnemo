// Package sessions defines the agent contract Mnemo ingests from.
//
// A kind provider knows the on-disk transcript layout of one coding tool
// (~/.claude, ~/.codex, ...). The Registry wraps providers with the
// project's configured agent list: an agent is a named instance of a kind,
// optionally pointed at custom source globs and tagged with capabilities.
// Known kinds ship built-in providers; custom agents declare their own parser
// and sources.
//
// Providers are observation-only: they never modify the original session
// files.
package sessions

import (
	"context"

	"github.com/sirrobot01/mnemo/internal/domain"
)

// Capability is a declarative tag describing what an agent supports. Resume
// targeting consults these (e.g. only a resume.cli agent is offered the CLI
// launcher form).
type Capability string

const (
	CapResumeCLI   Capability = "resume.cli"
	CapResumeStdin Capability = "resume.stdin"
	CapResumeFile  Capability = "resume.file"
	CapReadsFiles  Capability = "reads.files"
	CapRunsCmds    Capability = "runs.commands"
)

// Discovery is one transcript file found for an agent.
type Discovery struct {
	Agent      string
	Kind       domain.SessionKind
	SourcePath string
	ExternalID string
}

// Ingestion is the parsed representation of a single session file.
type Ingestion struct {
	Session domain.Session
	Events  []domain.SessionEvent
}

// Parser turns one transcript file into a canonical (Session, []Event). The
// returned Session.RepoID/Agent/ID are left for the caller to populate.
type Parser interface {
	Kind() domain.SessionKind
	Ingest(ctx context.Context, sourcePath string) (Ingestion, error)
}

// Discoverer is implemented by built-in kind providers that know their tool's
// on-disk layout (Claude's encoded projects dir, Codex's date tree, ...).
// Agents with explicit config `sources` bypass this and glob instead.
type Discoverer interface {
	Discover(ctx context.Context, repoRoot string) ([]Discovery, error)
}

// DirWatcher is the optional capability that lets `mnemo watch` tail a tool's
// directories. Providers without it are still ingestible via `mnemo ingest`.
type DirWatcher interface {
	WatchDirs(ctx context.Context, repoRoot string) ([]string, error)
}

// Provider is the full built-in contract for a known kind: parse + discover,
// optionally watch.
type Provider interface {
	Parser
	Discoverer
}
