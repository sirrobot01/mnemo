// Package codex implements the sessions.Adapter contract for the Codex CLI.
//
// Codex stores date-partitioned rollout files at
// ~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<id>.jsonl. Line 1 is a
// session_meta record carrying cwd + git {commit_hash, branch,
// repository_url}; subsequent lines are turn_context / event_msg /
// response_item records. Because rollouts are partitioned by date (not by
// repo), Discover peeks each file's session_meta to match the repository.
//
// This adapter targets the documented Codex rollout shape; it is tolerant
// of unknown record/payload types and skips what it does not recognize.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/sessions"
)

type Adapter struct {
	// HomeDir is the user's Codex home. Defaults to ~/.codex.
	HomeDir string
}

func New(homeDir string) *Adapter { return &Adapter{HomeDir: homeDir} }

func (a *Adapter) Kind() domain.SessionKind { return domain.SessionKindCodex }

func (a *Adapter) home() (string, error) {
	if a.HomeDir != "" {
		return a.HomeDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

func (a *Adapter) sessionsDir() (string, error) {
	home, err := a.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "sessions"), nil
}

// normalizeRemote strips a trailing .git and normalizes scp-style and https
// git URLs to a comparable host/path form.
func normalizeRemote(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimPrefix(u, "git+")
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if at := strings.LastIndex(u, "@"); at >= 0 {
		u = u[at+1:]
	}
	u = strings.ReplaceAll(u, ":", "/")
	return strings.Trim(strings.ToLower(u), "/")
}

type metaPayload struct {
	ID           string `json:"id,omitempty"`
	Timestamp    string `json:"timestamp,omitempty"`
	CWD          string `json:"cwd,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Git          struct {
		CommitHash    string `json:"commit_hash,omitempty"`
		Branch        string `json:"branch,omitempty"`
		RepositoryURL string `json:"repository_url,omitempty"`
	} `json:"git,omitempty"`
}

type rawLine struct {
	Type      string          `json:"type,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// peekMeta reads the first JSON line and returns its session_meta payload.
func peekMeta(path string) (metaPayload, bool) {
	f, err := os.Open(path)
	if err != nil {
		return metaPayload{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if !sc.Scan() {
		return metaPayload{}, false
	}
	var line rawLine
	if err := json.Unmarshal(sc.Bytes(), &line); err != nil || line.Type != "session_meta" {
		return metaPayload{}, false
	}
	var p metaPayload
	if err := json.Unmarshal(line.Payload, &p); err != nil {
		return metaPayload{}, false
	}
	return p, true
}

func (a *Adapter) Discover(ctx context.Context, repoRoot string) ([]sessions.Discovery, error) {
	if repoRoot == "" {
		return nil, fmt.Errorf("codex: repo root is required")
	}
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	dir, err := a.sessionsDir()
	if err != nil {
		return nil, err
	}

	var discoveries []sessions.Discovery
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		meta, ok := peekMeta(path)
		if !ok {
			return nil
		}
		if !sameRepo(meta, abs) {
			return nil
		}
		id := meta.ID
		if id == "" {
			id = strings.TrimSuffix(d.Name(), ".jsonl")
		}
		discoveries = append(discoveries, sessions.Discovery{
			Kind:       domain.SessionKindCodex,
			SourcePath: path,
			ExternalID: id,
		})
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return nil, walkErr
	}
	sort.Slice(discoveries, func(i, j int) bool {
		return discoveries[i].SourcePath < discoveries[j].SourcePath
	})
	return discoveries, nil
}

// sameRepo matches by cwd first; repository_url is normalized and compared
// against the repo path's basename as a weak fallback (the adapter does not
// know the local checkout's remote).
func sameRepo(meta metaPayload, repoAbs string) bool {
	if meta.CWD != "" {
		if c, err := filepath.Abs(meta.CWD); err == nil && c == repoAbs {
			return true
		}
	}
	if meta.Git.RepositoryURL != "" {
		nr := normalizeRemote(meta.Git.RepositoryURL)
		if nr != "" && strings.HasSuffix(nr, "/"+strings.ToLower(filepath.Base(repoAbs))) {
			return true
		}
	}
	return false
}

type itemPayload struct {
	Type    string          `json:"type,omitempty"`
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	// Summary carries reasoning text in newer Codex rollouts
	// (`reasoning` items put text under `summary`, not `content`).
	Summary json.RawMessage `json:"summary,omitempty"`
	Text    string          `json:"text,omitempty"`
	Name    string          `json:"name,omitempty"`
}

type contentBlock struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}

func (a *Adapter) Ingest(ctx context.Context, sourcePath string) (sessions.Ingestion, error) {
	f, err := os.Open(sourcePath)
	if err != nil {
		return sessions.Ingestion{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	now := time.Now().UTC()
	session := domain.Session{
		SourcePath: sourcePath,
		Kind:       domain.SessionKindCodex,
		ExternalID: strings.TrimSuffix(filepath.Base(sourcePath), ".jsonl"),
		Status:     domain.SessionStatusIngested,
		IngestedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	var events []domain.SessionEvent
	var firstTs, lastTs time.Time
	messageCount, sequence := 0, 0

	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return sessions.Ingestion{}, err
		}
		b := sc.Bytes()
		if len(b) == 0 {
			continue
		}
		var line rawLine
		if err := json.Unmarshal(b, &line); err != nil {
			continue
		}
		ts, _ := parseTime(line.Timestamp)
		if !ts.IsZero() {
			if firstTs.IsZero() || ts.Before(firstTs) {
				firstTs = ts
			}
			if ts.After(lastTs) {
				lastTs = ts
			}
		}

		switch line.Type {
		case "session_meta":
			var p metaPayload
			if json.Unmarshal(line.Payload, &p) == nil {
				if p.ID != "" {
					session.ExternalID = p.ID
				}
				session.Branch = p.Git.Branch
				session.CommitHash = p.Git.CommitHash
				if mt, err := parseTime(p.Timestamp); err == nil && !mt.IsZero() {
					firstTs = mt
				}
			}
			continue
		case "response_item":
			var p itemPayload
			if err := json.Unmarshal(line.Payload, &p); err != nil {
				continue
			}
			et := mapItemType(p)
			if et == "" {
				continue
			}
			content := extractText(p)
			if et == domain.SessionEventTypeUserMessage || et == domain.SessionEventTypeAssistantMessage {
				messageCount++
			}
			sequence++
			structured := map[string]any{"codex_item_type": p.Type}
			if p.Name != "" {
				structured["tool_name"] = p.Name
			}
			events = append(events, domain.SessionEvent{
				ID:              domain.NewID(domain.PrefixSessionEvent),
				Sequence:        sequence,
				Type:            et,
				Content:         content,
				Timestamp:       ts,
				StructuredValue: structured,
				CreatedAt:       now,
			})
		default:
			// turn_context, event_msg, and anything unrecognized are skipped.
			continue
		}
	}
	if err := sc.Err(); err != nil {
		return sessions.Ingestion{}, err
	}

	if !firstTs.IsZero() {
		session.StartedAt = firstTs
	} else {
		session.StartedAt = now
	}
	if !lastTs.IsZero() {
		ended := lastTs
		session.EndedAt = &ended
	}
	session.MessageCount = messageCount
	return sessions.Ingestion{Session: session, Events: events}, nil
}

func mapItemType(p itemPayload) domain.SessionEventType {
	switch p.Type {
	case "message":
		switch p.Role {
		case "user":
			return domain.SessionEventTypeUserMessage
		case "assistant":
			return domain.SessionEventTypeAssistantMessage
		default:
			return domain.SessionEventTypeSystem
		}
	case "reasoning":
		return domain.SessionEventTypeThinking
	case "function_call", "custom_tool_call":
		return domain.SessionEventTypeToolCall
	case "function_call_output", "custom_tool_call_output":
		return domain.SessionEventTypeToolResult
	default:
		return ""
	}
}

func extractText(p itemPayload) string {
	if p.Text != "" {
		return p.Text
	}
	if t := fromRaw(p.Content); t != "" {
		return t
	}
	// reasoning items: text lives under `summary`.
	return fromRaw(p.Summary)
}

// fromRaw decodes a Codex content field that may be a plain string or an
// array of {text} blocks.
func fromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func parseTime(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, v)
}

// WatchDirs returns the Codex sessions root. fsnotify is non-recursive, so
// `mnemo watch` tails the nearest existing ancestor; one-shot `mnemo ingest`
// always sees new date-partitioned rollouts regardless.
func (a *Adapter) WatchDirs(ctx context.Context, repoRoot string) ([]string, error) {
	dir, err := a.sessionsDir()
	if err != nil {
		return nil, err
	}
	return []string{dir}, nil
}

var _ sessions.Provider = (*Adapter)(nil)
var _ sessions.DirWatcher = (*Adapter)(nil)
