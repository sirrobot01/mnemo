// Package continueide implements the sessions.Adapter contract for Continue.
//
// Continue stores session JSON under ~/.continue/sessions/*.json. The schema
// has shifted across versions, so this parser is tolerant: it accepts either
// a `history[].message` or a flat `messages[]` array, and a string or
// block-array content. A session is only attributed to a repo when its
// recorded workspace directory matches — never guessed — to avoid
// cross-repo bleed.
package continueide

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/sessions"
)

type Adapter struct {
	HomeDir string // defaults to ~/.continue
}

func New(homeDir string) *Adapter { return &Adapter{HomeDir: homeDir} }

func (a *Adapter) Tool() domain.SessionTool { return domain.SessionToolContinue }

func (a *Adapter) dir() (string, error) {
	if a.HomeDir != "" {
		return filepath.Join(a.HomeDir, "sessions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".continue", "sessions"), nil
}

type rawMessage struct {
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

type rawSession struct {
	SessionID    string `json:"sessionId,omitempty"`
	Title        string `json:"title,omitempty"`
	Workspace    string `json:"workspaceDirectory,omitempty"`
	WorkspaceAlt string `json:"workspace,omitempty"` // older/newer schema variant
	History      []struct {
		Message rawMessage `json:"message"`
	} `json:"history,omitempty"`
	Messages []rawMessage `json:"messages,omitempty"`
}

// workspaceDir is the recorded workspace under either schema key.
func (s rawSession) workspaceDir() string {
	if s.Workspace != "" {
		return s.Workspace
	}
	return s.WorkspaceAlt
}

func (s rawSession) turns() []rawMessage {
	if len(s.Messages) > 0 {
		return s.Messages
	}
	out := make([]rawMessage, 0, len(s.History))
	for _, h := range s.History {
		out = append(out, h.Message)
	}
	return out
}

func parseFile(path string) (rawSession, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return rawSession{}, false
	}
	var s rawSession
	if json.Unmarshal(b, &s) != nil {
		return rawSession{}, false
	}
	return s, true
}

func (a *Adapter) Discover(ctx context.Context, repoRoot string) ([]sessions.Discovery, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	dir, err := a.dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []sessions.Discovery{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		s, ok := parseFile(path)
		ws := s.workspaceDir()
		if !ok || ws == "" {
			continue
		}
		if w, err := filepath.Abs(ws); err != nil || w != abs {
			continue
		}
		id := s.SessionID
		if id == "" {
			id = strings.TrimSuffix(e.Name(), ".json")
		}
		out = append(out, sessions.Discovery{Tool: domain.SessionToolContinue, SourcePath: path, ExternalID: id})
	}
	return out, nil
}

func (a *Adapter) Ingest(ctx context.Context, sourcePath string) (sessions.Ingestion, error) {
	now := time.Now().UTC()
	session := domain.Session{
		SourcePath: sourcePath,
		Tool:       domain.SessionToolContinue,
		ExternalID: strings.TrimSuffix(filepath.Base(sourcePath), ".json"),
		Status:     domain.SessionStatusIngested,
		StartedAt:  now,
		IngestedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s, ok := parseFile(sourcePath)
	if !ok {
		return sessions.Ingestion{Session: session}, nil
	}
	if s.SessionID != "" {
		session.ExternalID = s.SessionID
	}
	events := []domain.SessionEvent{}
	seq, msgs := 0, 0
	for _, m := range s.turns() {
		content := extractContent(m.Content)
		if content == "" {
			continue
		}
		var et domain.SessionEventType
		switch m.Role {
		case "user":
			et = domain.SessionEventTypeUserMessage
		case "assistant":
			et = domain.SessionEventTypeAssistantMessage
		default:
			et = domain.SessionEventTypeSystem
		}
		if et == domain.SessionEventTypeUserMessage || et == domain.SessionEventTypeAssistantMessage {
			msgs++
		}
		seq++
		events = append(events, domain.SessionEvent{
			ID:        domain.NewID(domain.PrefixSessionEvent),
			Sequence:  seq,
			Type:      et,
			Content:   content,
			Timestamp: now,
			CreatedAt: now,
		})
	}
	session.MessageCount = msgs
	return sessions.Ingestion{Session: session, Events: events}, nil
}

type block struct {
	Text string `json:"text,omitempty"`
}

func extractContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) == nil {
		parts := []string{}
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}

var _ sessions.Adapter = (*Adapter)(nil)
