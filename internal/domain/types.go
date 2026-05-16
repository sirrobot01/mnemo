package domain

import "time"

type ID string

// Scope is retained for Setting records (the only remaining scoped entity).
type Scope string

const (
	ScopeGlobal     Scope = "global"
	ScopeUser       Scope = "user"
	ScopeRepository Scope = "repository"
)

type Repository struct {
	ID            ID        `json:"id"`
	Name          string    `json:"name"`
	RootPath      string    `json:"root_path"`
	RemoteURL     string    `json:"remote_url"`
	DefaultBranch string    `json:"default_branch"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Setting struct {
	ID        ID             `json:"id"`
	Scope     Scope          `json:"scope"`
	Key       string         `json:"key"`
	Value     map[string]any `json:"value,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// ---------- Sessions (ingestion substrate) ----------

type SessionTool string

const (
	SessionToolClaude   SessionTool = "claude"
	SessionToolCodex    SessionTool = "codex"
	SessionToolCursor   SessionTool = "cursor"
	SessionToolWindsurf SessionTool = "windsurf"
	SessionToolAider    SessionTool = "aider"
	SessionToolContinue SessionTool = "continue"
)

type SessionStatus string

const (
	SessionStatusIngested SessionStatus = "ingested"
	SessionStatusIgnored  SessionStatus = "ignored"
	SessionStatusRedacted SessionStatus = "redacted"
)

type SessionEventType string

const (
	SessionEventTypeUserMessage      SessionEventType = "user_message"
	SessionEventTypeAssistantMessage SessionEventType = "assistant_message"
	SessionEventTypeToolCall         SessionEventType = "tool_call"
	SessionEventTypeToolResult       SessionEventType = "tool_result"
	SessionEventTypeThinking         SessionEventType = "thinking"
	SessionEventTypeSystem           SessionEventType = "system"
)

type Session struct {
	ID           ID            `json:"id"`
	RepoID       ID            `json:"repo_id"`
	Tool         SessionTool   `json:"tool"`
	SourcePath   string        `json:"source_path"`
	ExternalID   string        `json:"external_id,omitempty"`
	// SourceFingerprint is "<modUnixNano>:<size>" of the transcript file at
	// last ingest. Ingestion skips re-parsing a file whose fingerprint is
	// unchanged, which is what keeps `mnemo watch` cheap.
	SourceFingerprint string    `json:"source_fingerprint,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	EndedAt      *time.Time    `json:"ended_at,omitempty"`
	Branch       string        `json:"branch,omitempty"`
	CommitHash   string        `json:"commit_hash,omitempty"`
	MessageCount int           `json:"message_count"`
	Status       SessionStatus `json:"status"`
	IngestedAt   time.Time     `json:"ingested_at"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type SessionEvent struct {
	ID              ID               `json:"id"`
	SessionID       ID               `json:"session_id"`
	Sequence        int              `json:"sequence"`
	Type            SessionEventType `json:"type"`
	Content         string           `json:"content"`
	Timestamp       time.Time        `json:"timestamp"`
	StructuredValue map[string]any   `json:"structured_value,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
}

// ---------- Tasks (the unit of cross-agent continuity) ----------

type TaskStatus string

const (
	TaskStatusActive TaskStatus = "active"
	TaskStatusPaused TaskStatus = "paused"
	TaskStatusDone   TaskStatus = "done"
)

// Task is one in-progress unit of work that many tool sessions attach to.
type Task struct {
	ID     ID         `json:"id"`
	RepoID ID         `json:"repo_id"`
	Title  string     `json:"title"`
	Goal   string     `json:"goal,omitempty"`
	Status TaskStatus `json:"status"`
	Branch string     `json:"branch,omitempty"`
	// Pinned marks the explicitly chosen active task. While a non-done
	// pinned task exists, threading attaches new sessions to it regardless
	// of branch — "explicit override always wins".
	Pinned       bool      `json:"pinned"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastActiveAt time.Time `json:"last_active_at"`
}

// ---------- Working state (the compiled, injectable state of play) ----------

type RejectedApproach struct {
	Approach string `json:"approach"`
	Reason   string `json:"reason,omitempty"`
}

type Decision struct {
	Decision  string `json:"decision"`
	Rationale string `json:"rationale,omitempty"`
}

type FileTouched struct {
	Path    string `json:"path"`
	Summary string `json:"summary,omitempty"`
}

// Hypothesis is an in-flight belief. Confirmed defaults to false so the
// resume render can label it as unconfirmed.
type Hypothesis struct {
	Claim      string  `json:"claim"`
	Confidence float64 `json:"confidence,omitempty"`
	Confirmed  bool    `json:"confirmed"`
}

// WorkingState is the versioned, compiled state of play for a Task. It is
// derived from the task's session events; later versions supersede earlier
// ones.
type WorkingState struct {
	ID              ID                 `json:"id"`
	TaskID          ID                 `json:"task_id"`
	Version         int                `json:"version"`
	CompiledAt      time.Time          `json:"compiled_at"`
	SourceWatermark string             `json:"source_watermark,omitempty"`
	Goal            string             `json:"goal,omitempty"`
	Done            []string           `json:"done,omitempty"`
	InProgress      string             `json:"in_progress,omitempty"`
	NextSteps       []string           `json:"next_steps,omitempty"`
	Rejected        []RejectedApproach `json:"rejected,omitempty"`
	Decisions       []Decision         `json:"decisions,omitempty"`
	OpenQuestions   []string           `json:"open_questions,omitempty"`
	FilesTouched    []FileTouched      `json:"files_touched,omitempty"`
	Hypotheses      []Hypothesis       `json:"hypotheses,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
}
