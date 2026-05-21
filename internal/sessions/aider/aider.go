// Package aider implements the sessions.Adapter contract for Aider.
//
// Aider keeps a per-repository markdown chat log at
// <repoRoot>/.aider.chat.history.md. It is loosely structured: user turns
// are lines beginning "#### "; assistant prose follows until the next user
// turn or a "> " session banner. This parser is deliberately tolerant.
package aider

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/sessions"
)

const historyFile = ".aider.chat.history.md"

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Kind() domain.SessionKind { return domain.SessionKindAider }

func (a *Adapter) Discover(ctx context.Context, repoRoot string) ([]sessions.Discovery, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(abs, historyFile)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return []sessions.Discovery{{
		Kind:       domain.SessionKindAider,
		SourcePath: path,
		ExternalID: "aider",
	}}, nil
}

func (a *Adapter) Ingest(ctx context.Context, sourcePath string) (sessions.Ingestion, error) {
	f, err := os.Open(sourcePath)
	if err != nil {
		return sessions.Ingestion{}, err
	}
	defer f.Close()

	now := time.Now().UTC()
	session := domain.Session{
		SourcePath: sourcePath,
		Kind:       domain.SessionKindAider,
		ExternalID: "aider",
		Status:     domain.SessionStatusIngested,
		StartedAt:  now,
		IngestedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	var events []domain.SessionEvent
	seq, msgs := 0, 0

	var role domain.SessionEventType
	var buf []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = buf[:0]
		if role == "" || text == "" {
			return
		}
		seq++
		if role == domain.SessionEventTypeUserMessage || role == domain.SessionEventTypeAssistantMessage {
			msgs++
		}
		events = append(events, domain.SessionEvent{
			ID:        domain.NewID(domain.PrefixSessionEvent),
			Sequence:  seq,
			Type:      role,
			Content:   text,
			Timestamp: now,
			CreatedAt: now,
		})
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return sessions.Ingestion{}, err
		}
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "#### "):
			// Aider prefixes each user input line with "#### ". Consecutive
			// #### lines are one user turn; flush only when switching INTO
			// the user role from assistant/none.
			if role != domain.SessionEventTypeUserMessage {
				flush()
				role = domain.SessionEventTypeUserMessage
			}
			buf = append(buf, strings.TrimPrefix(line, "#### "))
		case strings.HasPrefix(line, "# "):
			// Markdown H1 = aider session banner — a hard turn boundary.
			flush()
			role = ""
		default:
			// "> " is aider's status banner ("> Tokens:", "> Added X") OR a
			// quote/diff line inside an assistant reply. Only treat it as
			// noise BETWEEN turns; inside an assistant turn it is content
			// (dropping it mid-reply would truncate code/diffs).
			if strings.HasPrefix(line, "> ") && role == "" {
				continue
			}
			if role == domain.SessionEventTypeUserMessage {
				flush()
				role = domain.SessionEventTypeAssistantMessage
			} else if role == "" && strings.TrimSpace(line) != "" {
				role = domain.SessionEventTypeAssistantMessage
			}
			if role != "" {
				buf = append(buf, line)
			}
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return sessions.Ingestion{}, err
	}
	session.MessageCount = msgs
	return sessions.Ingestion{Session: session, Events: events}, nil
}

// WatchDirs returns the repository root. Aider writes its chat history file
// directly there, so watching the root catches changes to
// .aider.chat.history.md without tailing the whole tree recursively.
func (a *Adapter) WatchDirs(ctx context.Context, repoRoot string) ([]string, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	return []string{abs}, nil
}

var _ sessions.Provider = (*Adapter)(nil)
var _ sessions.DirWatcher = (*Adapter)(nil)
