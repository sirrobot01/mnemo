package sessions

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
)

// genericParser returns a parser for a custom agent's declared parser kind.
// jsonl-openai and jsonl-anthropic both read newline-delimited JSON objects
// with a role/content shape; they differ only in how content is structured,
// which normalizeContent flattens.
func genericParser(kind domain.SessionKind) (Parser, error) {
	switch kind {
	case "jsonl-openai", "jsonl-anthropic", "jsonl":
		return jsonlParser{kind: kind}, nil
	default:
		return nil, fmt.Errorf("unknown custom parser %q (supported: jsonl, jsonl-openai, jsonl-anthropic)", kind)
	}
}

type jsonlParser struct{ kind domain.SessionKind }

func (p jsonlParser) Kind() domain.SessionKind { return p.kind }

// jsonlLine is the permissive shape accepted from custom transcripts: a role
// plus content that is either a plain string or OpenAI/Anthropic-style parts.
type jsonlLine struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Timestamp string          `json:"timestamp"`
}

func (p jsonlParser) Ingest(ctx context.Context, sourcePath string) (Ingestion, error) {
	file, err := os.Open(sourcePath)
	if err != nil {
		return Ingestion{}, err
	}
	defer file.Close()

	var events []domain.SessionEvent
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	seq := 0
	var started, ended time.Time
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw jsonlLine
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue // tolerate non-conforming lines rather than fail ingest
		}
		content := normalizeContent(raw.Content)
		if strings.TrimSpace(content) == "" {
			continue
		}
		ts := parseTimestamp(raw.Timestamp)
		if started.IsZero() || (!ts.IsZero() && ts.Before(started)) {
			started = ts
		}
		if !ts.IsZero() {
			ended = ts
		}
		events = append(events, domain.SessionEvent{
			Type:      roleToEventType(raw.Role),
			Sequence:  seq,
			Content:   content,
			Timestamp: ts,
		})
		seq++
	}
	if err := scanner.Err(); err != nil {
		return Ingestion{}, err
	}

	if started.IsZero() {
		started = time.Now().UTC()
	}
	session := domain.Session{
		Kind:       p.kind,
		SourcePath: sourcePath,
		StartedAt:  started,
		Status:     domain.SessionStatusIngested,
	}
	if !ended.IsZero() {
		session.EndedAt = &ended
	}
	return Ingestion{Session: session, Events: events}, nil
}

func roleToEventType(role string) domain.SessionEventType {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "human":
		return domain.SessionEventTypeUserMessage
	case "assistant", "ai", "model":
		return domain.SessionEventTypeAssistantMessage
	case "tool", "tool_result", "function":
		return domain.SessionEventTypeToolResult
	default:
		return domain.SessionEventTypeSystem
	}
}

// normalizeContent flattens a string, an OpenAI parts array
// ([{type,text}...]), or an arbitrary JSON value into plain text.
func normalizeContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			if part.Text != "" {
				b.WriteString(part.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return string(raw)
}

func parseTimestamp(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
