// Package claude implements the sessions.Adapter contract for Claude Code.
//
// Claude stores one JSONL file per session under
// ~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl, where encoded-cwd
// replaces "/" with "-". Every line carries cwd, sessionId, timestamp, and
// gitBranch fields; assistant lines carry a structured message.content
// block list.
package claude

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

// Adapter reads Claude Code session logs from disk.
type Adapter struct {
	// HomeDir is the user's Claude home directory. Defaults to ~/.claude.
	HomeDir string
}

// New returns a Claude adapter rooted at the given home directory. An empty
// home falls back to $HOME/.claude.
func New(homeDir string) *Adapter {
	return &Adapter{HomeDir: homeDir}
}

func (a *Adapter) Kind() domain.SessionKind { return domain.SessionKindClaude }

func (a *Adapter) home() (string, error) {
	if a.HomeDir != "" {
		return a.HomeDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// EncodeRepoRoot converts an absolute repository path to Claude's
// projects/<encoded-cwd> directory name. Claude replaces every "/" with "-".
func EncodeRepoRoot(repoRoot string) string {
	return strings.ReplaceAll(repoRoot, "/", "-")
}

func (a *Adapter) Discover(ctx context.Context, repoRoot string) ([]sessions.Discovery, error) {
	if repoRoot == "" {
		return nil, fmt.Errorf("claude: repo root is required")
	}
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	home, err := a.home()
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(home, "projects", EncodeRepoRoot(abs))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var discoveries []sessions.Discovery
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		external := strings.TrimSuffix(entry.Name(), ".jsonl")
		discoveries = append(discoveries, sessions.Discovery{
			Kind:       domain.SessionKindClaude,
			SourcePath: filepath.Join(dir, entry.Name()),
			ExternalID: external,
		})
	}
	sort.Slice(discoveries, func(i, j int) bool {
		return discoveries[i].SourcePath < discoveries[j].SourcePath
	})
	return discoveries, nil
}

type rawLine struct {
	Type        string          `json:"type"`
	Subtype     string          `json:"subtype,omitempty"`
	SessionID   string          `json:"sessionId,omitempty"`
	Timestamp   string          `json:"timestamp,omitempty"`
	CWD         string          `json:"cwd,omitempty"`
	GitBranch   string          `json:"gitBranch,omitempty"`
	Version     string          `json:"version,omitempty"`
	UUID        string          `json:"uuid,omitempty"`
	Content     json.RawMessage `json:"content,omitempty"`
	Message     json.RawMessage `json:"message,omitempty"`
	IsMeta      bool            `json:"isMeta,omitempty"`
	IsSidechain bool            `json:"isSidechain,omitempty"`
}

type rawMessage struct {
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

type contentBlock struct {
	Type string          `json:"type,omitempty"`
	Text string          `json:"text,omitempty"`
	Name string          `json:"name,omitempty"`
	ID   string          `json:"id,omitempty"`
	Raw  json.RawMessage `json:"-"`
}

func (a *Adapter) Ingest(ctx context.Context, sourcePath string) (sessions.Ingestion, error) {
	file, err := os.Open(sourcePath)
	if err != nil {
		return sessions.Ingestion{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	now := time.Now().UTC()
	session := domain.Session{
		SourcePath: sourcePath,
		Kind:       domain.SessionKindClaude,
		ExternalID: strings.TrimSuffix(filepath.Base(sourcePath), ".jsonl"),
		Status:     domain.SessionStatusIngested,
		IngestedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	var events []domain.SessionEvent

	var firstTs, lastTs time.Time
	messageCount := 0
	sequence := 0

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return sessions.Ingestion{}, err
		}
		bytes := scanner.Bytes()
		if len(bytes) == 0 {
			continue
		}
		var line rawLine
		if err := json.Unmarshal(bytes, &line); err != nil {
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
		if line.SessionID != "" && session.ExternalID == "" {
			session.ExternalID = line.SessionID
		}
		if line.GitBranch != "" && session.Branch == "" {
			session.Branch = line.GitBranch
		}

		eventType := mapEventType(line.Type)
		if eventType == "" {
			continue
		}
		content := extractContent(line)
		if eventType == domain.SessionEventTypeUserMessage || eventType == domain.SessionEventTypeAssistantMessage {
			messageCount++
		}

		sequence++
		structured := map[string]any{}
		if line.Subtype != "" {
			structured["subtype"] = line.Subtype
		}
		if line.IsMeta {
			structured["is_meta"] = true
		}
		if line.IsSidechain {
			structured["is_sidechain"] = true
		}
		if line.UUID != "" {
			structured["claude_uuid"] = line.UUID
		}

		event := domain.SessionEvent{
			ID:              domain.NewID(domain.PrefixSessionEvent),
			Sequence:        sequence,
			Type:            eventType,
			Content:         content,
			Timestamp:       ts,
			StructuredValue: structured,
			CreatedAt:       now,
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
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

func mapEventType(claudeType string) domain.SessionEventType {
	switch claudeType {
	case "user":
		return domain.SessionEventTypeUserMessage
	case "assistant":
		return domain.SessionEventTypeAssistantMessage
	case "system":
		return domain.SessionEventTypeSystem
	case "tool_use":
		return domain.SessionEventTypeToolCall
	case "tool_result":
		return domain.SessionEventTypeToolResult
	case "thinking":
		return domain.SessionEventTypeThinking
	default:
		return ""
	}
}

func extractContent(line rawLine) string {
	// `system` and similar lines carry a top-level "content" string.
	if len(line.Content) > 0 {
		var asString string
		if err := json.Unmarshal(line.Content, &asString); err == nil {
			return asString
		}
	}
	if len(line.Message) == 0 {
		return ""
	}
	var msg rawMessage
	if err := json.Unmarshal(line.Message, &msg); err != nil {
		return ""
	}
	if len(msg.Content) == 0 {
		return ""
	}
	// Content may be a plain string (user) or an array of blocks (assistant).
	var asString string
	if err := json.Unmarshal(msg.Content, &asString); err == nil {
		return asString
	}
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, value)
}

// WatchDirs returns the Claude projects directory for this repository so
// `mnemo watch` can tail it. The directory may not exist yet (Claude
// creates it on first session); the caller watches the nearest ancestor.
func (a *Adapter) WatchDirs(ctx context.Context, repoRoot string) ([]string, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	home, err := a.home()
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(home, "projects", EncodeRepoRoot(abs))}, nil
}

var _ sessions.Provider = (*Adapter)(nil)
var _ sessions.DirWatcher = (*Adapter)(nil)
