package sessions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
)

const (
	structuredMaxFileSize = 8 * 1024 * 1024
	structuredMaxFiles    = 400
)

// DiscoverStructured returns candidate transcript files or directories under
// root that mention repoRoot. It is intentionally bounded so built-in agents
// with less stable local storage layouts cannot accidentally crawl a whole
// application cache.
func DiscoverStructured(ctx context.Context, root, repoRoot string, kind domain.SessionKind) ([]Discovery, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if pathMentions(ctx, root, absRepo) {
			return []Discovery{{Kind: kind, SourcePath: root, ExternalID: strings.TrimSuffix(filepath.Base(root), filepath.Ext(root))}}, nil
		}
		return nil, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Discovery
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(root, entry.Name())
		if !entry.IsDir() && !structuredCandidate(path) {
			continue
		}
		if pathMentions(ctx, path, absRepo) {
			out = append(out, Discovery{
				Kind:       kind,
				SourcePath: path,
				ExternalID: strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			})
		}
	}
	return out, nil
}

// IngestStructured parses a candidate transcript file or directory using a
// tolerant JSON/JSONL/text event extractor. It is for tools whose public docs
// expose local session storage but not a stable event schema.
func IngestStructured(ctx context.Context, sourcePath string, kind domain.SessionKind) (Ingestion, error) {
	now := time.Now().UTC()
	session := domain.Session{
		SourcePath: sourcePath,
		Kind:       kind,
		ExternalID: strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath)),
		Status:     domain.SessionStatusIngested,
		StartedAt:  now,
		IngestedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	files, err := structuredFiles(sourcePath)
	if err != nil {
		return Ingestion{}, err
	}
	var events []domain.SessionEvent
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return Ingestion{}, err
		}
		extracted := extractStructuredFile(ctx, file)
		for _, ev := range extracted {
			ev.Sequence = len(events)
			if ev.Timestamp.IsZero() {
				ev.Timestamp = now
			}
			ev.CreatedAt = now
			events = append(events, ev)
			if session.StartedAt.Equal(now) || ev.Timestamp.Before(session.StartedAt) {
				session.StartedAt = ev.Timestamp
			}
			if session.EndedAt == nil || ev.Timestamp.After(*session.EndedAt) {
				ts := ev.Timestamp
				session.EndedAt = &ts
			}
		}
	}
	return Ingestion{Session: session, Events: events}, nil
}

func pathMentions(ctx context.Context, path, repoRoot string) bool {
	needles := []string{repoRoot, filepath.ToSlash(repoRoot)}
	files, err := structuredFiles(path)
	if err != nil {
		return false
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return false
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > structuredMaxFileSize {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, needle := range needles {
			if needle != "" && bytes.Contains(body, []byte(needle)) {
				return true
			}
		}
	}
	return false
}

func structuredFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if structuredCandidate(path) {
			return []string{path}, nil
		}
		return nil, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if len(files) >= structuredMaxFiles {
			if d.IsDir() && p != path {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if structuredCandidate(p) {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func structuredCandidate(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".jsonl", ".md", ".txt", ".log":
		return true
	default:
		return false
	}
}

func extractStructuredFile(ctx context.Context, path string) []domain.SessionEvent {
	info, err := os.Stat(path)
	if err != nil || info.Size() > structuredMaxFileSize {
		return nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return extractJSONFile(path)
	case ".jsonl", ".log":
		if events := extractJSONLFile(ctx, path); len(events) > 0 {
			return events
		}
	}
	return extractPrefixedTextFile(ctx, path)
}

func extractJSONFile(path string) []domain.SessionEvent {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil
	}
	var events []domain.SessionEvent
	walkStructuredJSON(value, &events)
	return events
}

func extractJSONLFile(ctx context.Context, path string) []domain.SessionEvent {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var events []domain.SessionEvent
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 64*1024), structuredMaxFileSize)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return events
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var value any
		dec := json.NewDecoder(strings.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&value); err != nil {
			continue
		}
		walkStructuredJSON(value, &events)
	}
	return events
}

func extractPrefixedTextFile(ctx context.Context, path string) []domain.SessionEvent {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var events []domain.SessionEvent
	var typ domain.SessionEventType
	var lines []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(lines, "\n"))
		if typ != "" && text != "" {
			events = append(events, domain.SessionEvent{Type: typ, Content: text})
		}
		lines = lines[:0]
	}
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 64*1024), structuredMaxFileSize)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return events
		}
		line := sc.Text()
		if next, rest, ok := prefixedRole(line); ok {
			flush()
			typ = roleToEventType(next)
			lines = append(lines, rest)
			continue
		}
		if typ != "" {
			lines = append(lines, line)
		}
	}
	flush()
	return events
}

func prefixedRole(line string) (role, rest string, ok bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	role = strings.ToLower(strings.TrimSpace(parts[0]))
	switch role {
	case "user", "human", "assistant", "agent", "copilot", "cursor", "windsurf", "devin", "tool", "system":
		return role, strings.TrimSpace(parts[1]), true
	default:
		return "", "", false
	}
}

func walkStructuredJSON(value any, events *[]domain.SessionEvent) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			walkStructuredJSON(item, events)
		}
	case map[string]any:
		if appendPromptResponse(v, events) {
			return
		}
		if appendRoleContent(v, events) {
			return
		}
		for _, item := range v {
			walkStructuredJSON(item, events)
		}
	}
}

func appendPromptResponse(m map[string]any, events *[]domain.SessionEvent) bool {
	prompt := textFromMap(m, "prompt", "input", "question", "query", "userPrompt", "user_prompt")
	response := textFromMap(m, "response", "answer", "output", "result", "assistantResponse", "assistant_response")
	if prompt == "" || response == "" {
		return false
	}
	ts := timestampFromMap(m)
	*events = append(*events,
		domain.SessionEvent{Type: domain.SessionEventTypeUserMessage, Content: prompt, Timestamp: ts},
		domain.SessionEvent{Type: domain.SessionEventTypeAssistantMessage, Content: response, Timestamp: ts},
	)
	return true
}

func appendRoleContent(m map[string]any, events *[]domain.SessionEvent) bool {
	role := textFromMap(m, "role", "speaker", "sender", "author", "source", "type")
	content := textFromMap(m, "content", "text", "message", "body", "data", "value")
	if role == "" || content == "" {
		return false
	}
	*events = append(*events, domain.SessionEvent{
		Type:      roleToEventType(role),
		Content:   content,
		Timestamp: timestampFromMap(m),
	})
	return true
}

func textFromMap(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := lookupFold(m, key); ok {
			if text := textValue(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func lookupFold(m map[string]any, want string) (any, bool) {
	for key, value := range m {
		if strings.EqualFold(key, want) {
			return value, true
		}
	}
	return nil, false
}

func textValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case map[string]any:
		for _, key := range []string{"text", "content", "message", "value"} {
			if item, ok := lookupFold(v, key); ok {
				if text := textValue(item); text != "" {
					return text
				}
			}
		}
	case []any:
		var parts []string
		for _, item := range v {
			if text := textValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}

func timestampFromMap(m map[string]any) time.Time {
	for _, key := range []string{"timestamp", "time", "createdAt", "created_at", "date"} {
		if value, ok := lookupFold(m, key); ok {
			switch v := value.(type) {
			case string:
				if ts := parseTimestamp(v); !ts.IsZero() {
					return ts
				}
			case json.Number:
				if n, err := v.Int64(); err == nil {
					return unixTime(n)
				}
			case float64:
				return unixTime(int64(v))
			}
		}
	}
	return time.Time{}
}

func unixTime(value int64) time.Time {
	if value > 1_000_000_000_000 {
		return time.UnixMilli(value).UTC()
	}
	if value > 0 {
		return time.Unix(value, 0).UTC()
	}
	return time.Time{}
}
