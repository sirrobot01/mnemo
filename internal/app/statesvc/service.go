// Package statesvc compiles a task's ordered session events into a
// versioned WorkingState — the compact "state of play" the next agent
// inherits. Compilation is deterministic and heuristic-first: it reads
// signals already present in transcripts (corrections, plan/done language,
// uncertainty, file mentions) rather than calling an LLM.
package statesvc

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/storage"
)

// Enricher is the optional, disabled-by-default seam for LLM-backed
// summarization. A concrete provider can refine the deterministic heuristic
// output; it is never required and any error falls back to the heuristic
// state.
type Enricher interface {
	Enrich(ctx context.Context, state domain.WorkingState, events []domain.SessionEvent) (domain.WorkingState, error)
}

type Service struct {
	tasks    storage.TaskStore
	sessions storage.SessionStore
	ws       storage.WorkingStateStore
	enricher Enricher
	now      func() time.Time
}

func New(tasks storage.TaskStore, sessions storage.SessionStore, ws storage.WorkingStateStore) *Service {
	return &Service{tasks: tasks, sessions: sessions, ws: ws, now: func() time.Time { return time.Now().UTC() }}
}

// SetEnricher wires an optional enrichment provider. Nil (the default)
// keeps compilation purely deterministic and offline.
func (s *Service) SetEnricher(e Enricher) { s.enricher = e }

func (s *Service) Latest(ctx context.Context, taskID domain.ID) (domain.WorkingState, error) {
	return s.ws.GetLatestWorkingState(ctx, taskID)
}

type orderedEvent struct {
	sessionStart time.Time
	event        domain.SessionEvent
}

// Compile builds and persists the next WorkingState version for the task.
func (s *Service) Compile(ctx context.Context, taskID domain.ID) (domain.WorkingState, error) {
	task, err := s.tasks.GetTask(ctx, taskID)
	if err != nil {
		return domain.WorkingState{}, err
	}
	sessionIDs, err := s.tasks.ListTaskSessions(ctx, taskID)
	if err != nil {
		return domain.WorkingState{}, err
	}

	ordered := []orderedEvent{}
	for _, sid := range sessionIDs {
		sess, err := s.sessions.GetSession(ctx, sid)
		if err != nil {
			return domain.WorkingState{}, err
		}
		evs, err := s.sessions.ListSessionEvents(ctx, storage.SessionEventFilter{SessionID: sid})
		if err != nil {
			return domain.WorkingState{}, err
		}
		for _, e := range evs {
			ordered = append(ordered, orderedEvent{sessionStart: sess.StartedAt, event: e})
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].sessionStart.Equal(ordered[j].sessionStart) {
			return ordered[i].sessionStart.Before(ordered[j].sessionStart)
		}
		return ordered[i].event.Sequence < ordered[j].event.Sequence
	})

	state := extract(task, ordered)

	if s.enricher != nil {
		flat := make([]domain.SessionEvent, 0, len(ordered))
		for _, oe := range ordered {
			flat = append(flat, oe.event)
		}
		if enriched, err := s.enricher.Enrich(ctx, state, flat); err == nil {
			state = enriched
		}
		// On enricher error: keep the deterministic heuristic state.
	}

	prevVersion := 0
	if latest, err := s.ws.GetLatestWorkingState(ctx, taskID); err == nil {
		prevVersion = latest.Version
	} else if !errors.Is(err, storage.ErrNotFound) {
		return domain.WorkingState{}, err
	}
	now := s.now()
	state.Version = prevVersion + 1
	state.ID = domain.DeterministicID(domain.PrefixWorkingState, string(taskID), strconv.Itoa(state.Version))
	state.TaskID = taskID
	state.CompiledAt = now
	state.CreatedAt = now
	if n := len(ordered); n > 0 {
		state.SourceWatermark = string(ordered[n-1].event.ID)
	}

	if err := s.ws.SaveWorkingState(ctx, state); err != nil {
		return domain.WorkingState{}, err
	}
	return state, nil
}

var (
	fileTokenPattern = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_./-]*\.[A-Za-z0-9]{1,6}`)
	// correctionPrefixes match only when the user message *starts* with
	// them — a real correction, not the substring "cannot"/"another".
	correctionPrefixes = []string{
		"no,", "no ", "nope", "actually", "don't", "do not", "stop",
		"not that", "instead", "we use", "we don't use", "rather,", "wrong",
	}
	doneMarkers        = []string{"done", "fixed", "implemented", "completed", "works now", "passing", "resolved"}
	nextMarkers        = []string{"next", "todo", "to do", "should ", "need to", "let's ", "plan:", "going to"}
	uncertaintyMarkers = []string{"i think", "probably", "might be", "may be", "likely", "not sure", "seems", "perhaps", "i suspect", "i guess"}
	knownExts          = map[string]bool{
		"go": true, "ts": true, "tsx": true, "js": true, "jsx": true, "py": true,
		"rs": true, "java": true, "rb": true, "sql": true, "md": true, "yaml": true,
		"yml": true, "json": true, "sh": true, "c": true, "h": true, "cpp": true,
		"css": true, "html": true, "toml": true, "proto": true, "tf": true,
	}
)

const (
	capList  = 12
	capSmall = 8
	capFiles = 25
)

func extract(task domain.Task, ordered []orderedEvent) domain.WorkingState {
	st := domain.WorkingState{Goal: strings.TrimSpace(task.Goal)}
	files := newDedupe()
	var lastAssistant string

	for _, oe := range ordered {
		e := oe.event
		content := strings.TrimSpace(e.Content)
		if content == "" {
			continue
		}
		low := strings.ToLower(content)

		for _, m := range fileTokenPattern.FindAllString(content, -1) {
			if looksLikeFile(m) && files.add(m) && len(st.FilesTouched) < capFiles {
				st.FilesTouched = append(st.FilesTouched, domain.FileTouched{Path: m})
			}
		}

		switch e.Type {
		case domain.SessionEventTypeUserMessage:
			if st.Goal == "" && !looksLikeCorrection(low) {
				st.Goal = truncate(content, 160)
			}
			if strings.Contains(content, "?") && len(st.OpenQuestions) < capList {
				st.OpenQuestions = appendUnique(st.OpenQuestions, truncate(content, 200))
			}
			if looksLikeCorrection(low) {
				if len(st.Decisions) < capList {
					st.Decisions = append(st.Decisions, domain.Decision{Decision: truncate(content, 200)})
				}
				if lastAssistant != "" && len(st.Rejected) < capSmall {
					st.Rejected = append(st.Rejected, domain.RejectedApproach{
						Approach: truncate(meaningfulSentence(lastAssistant, nil), 160),
						Reason:   truncate(content, 200),
					})
				}
			}
		case domain.SessionEventTypeAssistantMessage:
			lastAssistant = content
			st.InProgress = truncate(content, 200)
			if hasAny(low, doneMarkers) && len(st.Done) < capList {
				if s := meaningfulSentence(content, doneMarkers); s != "" {
					st.Done = appendUnique(st.Done, truncate(s, 160))
				}
			}
			if hasAny(low, nextMarkers) && len(st.NextSteps) < capList {
				if s := meaningfulSentence(content, nextMarkers); s != "" {
					st.NextSteps = appendUnique(st.NextSteps, truncate(s, 160))
				}
			}
			if hasAny(low, uncertaintyMarkers) && len(st.Hypotheses) < capSmall {
				if s := meaningfulSentence(content, uncertaintyMarkers); s != "" {
					st.Hypotheses = append(st.Hypotheses, domain.Hypothesis{Claim: truncate(s, 200), Confirmed: false})
				}
			}
		}
	}
	return st
}

// looksLikeFile filters file-token regex hits down to plausible paths:
// a known code extension, or a slash-bearing token with an alphabetic
// extension. Version/number tokens ("v2.0", "1.5", "3.11") are rejected.
func looksLikeFile(tok string) bool {
	base := tok
	if i := strings.LastIndex(tok, "/"); i >= 0 {
		base = tok[i+1:]
	}
	dot := strings.LastIndex(base, ".")
	if dot <= 0 || dot == len(base)-1 {
		return false
	}
	stem, ext := base[:dot], strings.ToLower(base[dot+1:])
	if isAllDigits(ext) || isAllDigits(strings.ReplaceAll(stem, ".", "")) {
		return false
	}
	if knownExts[ext] {
		return true
	}
	return strings.Contains(tok, "/") && isAlpha(ext)
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

func hasAny(low string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// looksLikeCorrection is true when a (lowered, trimmed) user message
// *starts* with a correction phrase — the user pushing back, not the
// substring "cannot"/"another".
func looksLikeCorrection(low string) bool {
	for _, p := range correctionPrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

// splitSentences breaks text on newlines and sentence-final periods (a
// period followed by space/EOL — not the dot inside "internal/x.go" or
// "e.g.").
func splitSentences(s string) []string {
	var out []string
	runes := []rune(s)
	var cur []rune
	flush := func() {
		if t := strings.TrimSpace(string(cur)); t != "" {
			out = append(out, t)
		}
		cur = cur[:0]
	}
	for i, r := range runes {
		cur = append(cur, r)
		if r == '\n' || ((r == '.' || r == '!' || r == ';') && (i+1 >= len(runes) || runes[i+1] == ' ')) {
			flush()
		}
	}
	flush()
	return out
}

// meaningfulSentence returns the first sentence that carries signal: if
// markers is non-nil, the first sentence containing a marker and long
// enough to be informative; otherwise the first non-trivial sentence.
// Falls back
// to the trimmed content so callers never get an empty/just-"Done" result.
func meaningfulSentence(content string, markers []string) string {
	for _, sent := range splitSentences(content) {
		clean := strings.TrimRight(sent, ".!;")
		if len(clean) < 8 || len(strings.Fields(clean)) < 3 {
			continue
		}
		if markers == nil || hasAny(strings.ToLower(sent), markers) {
			return clean
		}
	}
	return truncate(content, 160)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

type dedupe struct{ seen map[string]bool }

func newDedupe() *dedupe { return &dedupe{seen: map[string]bool{}} }

func (d *dedupe) add(v string) bool {
	if d.seen[v] {
		return false
	}
	d.seen[v] = true
	return true
}
