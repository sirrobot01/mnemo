// Package ingestsvc owns session ingestion: it drives per-tool
// sessions.Adapter implementations, runs every event through the secret
// scanner before persistence, and writes idempotently so re-ingesting the
// same source file is a no-op.
package ingestsvc

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/safety"
	"github.com/sirrobot01/mnemo/internal/sessions"
	"github.com/sirrobot01/mnemo/internal/storage"
)

// Service ingests sessions for one repository, driven by the configured
// agent registry.
type Service struct {
	repo     domain.Repository
	store    storage.SessionStore
	registry *sessions.Registry
}

func New(repo domain.Repository, store storage.SessionStore, registry *sessions.Registry) *Service {
	return &Service{repo: repo, store: store, registry: registry}
}

// ImportResult summarizes one agent sweep.
type ImportResult struct {
	Agent            string `json:"agent"`
	Kind             string `json:"kind"`
	Discovered       int    `json:"discovered"`
	Imported         int    `json:"imported"`
	Unchanged        int    `json:"unchanged"`
	Skipped          int    `json:"skipped"`
	RedactedEvents   int    `json:"redacted_events"`
	RedactedSessions int    `json:"redacted_sessions"`
}

// fingerprint is "<modUnixNano>:<size>" of a transcript file, or "" if it
// cannot be stat'd (in which case ingestion does not skip — it parses to be
// safe).
func fingerprint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
}

// Import sweeps every configured agent for the repository root. One broken
// agent does not abort the others — its error is returned alongside the
// successful results so the caller can surface it without losing progress.
func (s *Service) Import(ctx context.Context) ([]ImportResult, error) {
	ignore, err := config.LoadIgnore(s.repo.RootPath)
	if err != nil {
		return nil, err
	}
	agents := s.registry.Agents()
	results := make([]ImportResult, 0, len(agents))
	var firstErr error
	for _, agent := range agents {
		if ignore.SkipAgent(agent.Name) || ignore.SkipAgent(string(agent.Kind)) {
			continue
		}
		res, err := s.importAgent(ctx, agent, ignore)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		results = append(results, res)
	}
	return results, firstErr
}

func (s *Service) importAgent(ctx context.Context, agent *sessions.Agent, ignore *config.Ignore) (ImportResult, error) {
	res := ImportResult{Agent: agent.Name, Kind: string(agent.Kind)}
	discoveries, err := agent.Discover(ctx, s.repo.RootPath)
	if err != nil {
		return res, err
	}
	res.Discovered = len(discoveries)

	for _, d := range discoveries {
		if ignore.SkipPath(d.SourcePath) {
			res.Skipped++
			continue
		}

		sessionID := domain.DeterministicID(domain.PrefixSession, agent.Name, d.SourcePath)
		fp := fingerprint(d.SourcePath)
		if fp != "" {
			if existing, err := s.store.GetSession(ctx, sessionID); err == nil && existing.SourceFingerprint == fp {
				// Source file unchanged since last ingest — skip the parse
				// and all the redundant insert-or-ignore writes. This is
				// what keeps `mnemo watch` cheap.
				res.Unchanged++
				continue
			}
		}

		ing, err := agent.Ingest(ctx, d.SourcePath)
		if err != nil {
			return res, err
		}

		sess := ing.Session
		sess.RepoID = s.repo.ID
		sess.ID = sessionID
		sess.Agent = agent.Name
		sess.Kind = agent.Kind
		sess.SourceFingerprint = fp

		kept := make([]domain.SessionEvent, 0, len(ing.Events))
		messageCount := 0
		for _, ev := range ing.Events {
			if safety.RejectIfSecret(ev.Content) != nil {
				res.RedactedEvents++
				continue
			}
			ev.SessionID = sess.ID
			ev.ID = domain.DeterministicID(domain.PrefixSessionEvent, string(sess.ID), strconv.Itoa(ev.Sequence))
			if ev.Type == domain.SessionEventTypeUserMessage || ev.Type == domain.SessionEventTypeAssistantMessage {
				messageCount++
			}
			kept = append(kept, ev)
		}

		sess.MessageCount = messageCount
		if len(ing.Events) > 0 && len(kept) == 0 {
			sess.Status = domain.SessionStatusRedacted
		}

		if err := s.store.SaveSession(ctx, sess); err != nil {
			return res, err
		}
		if err := s.store.AppendSessionEvents(ctx, sess.ID, kept); err != nil {
			return res, err
		}
		res.Imported++
		if sess.Status == domain.SessionStatusRedacted {
			res.RedactedSessions++
		}
	}
	return res, nil
}

func (s *Service) List(ctx context.Context) ([]domain.Session, error) {
	return s.store.ListSessions(ctx, storage.SessionFilter{RepoID: s.repo.ID})
}

func (s *Service) Get(ctx context.Context, id domain.ID) (domain.Session, error) {
	return s.store.GetSession(ctx, id)
}

func (s *Service) Events(ctx context.Context, sessionID domain.ID) ([]domain.SessionEvent, error) {
	return s.store.ListSessionEvents(ctx, storage.SessionEventFilter{SessionID: sessionID})
}

// Forget removes a session and its events completely. The source transcript
// on disk is never touched — Mnemo never owned it.
func (s *Service) Forget(ctx context.Context, id domain.ID) error {
	return s.store.DeleteSession(ctx, id)
}
