package domain

import (
	"fmt"
	"strings"
)

func (s Scope) Valid() bool {
	switch s {
	case ScopeGlobal, ScopeUser, ScopeRepository:
		return true
	default:
		return false
	}
}

func (t SessionTool) Valid() bool {
	switch t {
	case SessionToolClaude,
		SessionToolCodex,
		SessionToolCursor,
		SessionToolWindsurf,
		SessionToolAider,
		SessionToolContinue:
		return true
	default:
		return false
	}
}

func (s SessionStatus) Valid() bool {
	switch s {
	case SessionStatusIngested, SessionStatusIgnored, SessionStatusRedacted:
		return true
	default:
		return false
	}
}

func (t SessionEventType) Valid() bool {
	switch t {
	case SessionEventTypeUserMessage,
		SessionEventTypeAssistantMessage,
		SessionEventTypeToolCall,
		SessionEventTypeToolResult,
		SessionEventTypeThinking,
		SessionEventTypeSystem:
		return true
	default:
		return false
	}
}

func (s Session) Validate() error {
	if strings.TrimSpace(string(s.ID)) == "" {
		return fmt.Errorf("session id is required")
	}
	if strings.TrimSpace(string(s.RepoID)) == "" {
		return fmt.Errorf("session repo_id is required")
	}
	if !s.Tool.Valid() {
		return fmt.Errorf("invalid session tool %q", s.Tool)
	}
	if strings.TrimSpace(s.SourcePath) == "" {
		return fmt.Errorf("session source_path is required")
	}
	if !s.Status.Valid() {
		return fmt.Errorf("invalid session status %q", s.Status)
	}
	if s.MessageCount < 0 {
		return fmt.Errorf("session message_count must be non-negative")
	}
	if s.EndedAt != nil && !s.StartedAt.IsZero() && s.EndedAt.Before(s.StartedAt) {
		return fmt.Errorf("session ended_at cannot be before started_at")
	}
	return nil
}

func (e SessionEvent) Validate() error {
	if strings.TrimSpace(string(e.ID)) == "" {
		return fmt.Errorf("session event id is required")
	}
	if strings.TrimSpace(string(e.SessionID)) == "" {
		return fmt.Errorf("session event session_id is required")
	}
	if !e.Type.Valid() {
		return fmt.Errorf("invalid session event type %q", e.Type)
	}
	if e.Sequence < 0 {
		return fmt.Errorf("session event sequence must be non-negative")
	}
	return nil
}

func (s TaskStatus) Valid() bool {
	switch s {
	case TaskStatusActive, TaskStatusPaused, TaskStatusDone:
		return true
	default:
		return false
	}
}

// CanTransitionTo enforces the task lifecycle. A task moves freely between
// active and paused; done is terminal — the user starts a new task rather
// than reopening a finished one.
func (s TaskStatus) CanTransitionTo(next TaskStatus) bool {
	if !s.Valid() || !next.Valid() {
		return false
	}
	if s == next {
		return true
	}
	switch s {
	case TaskStatusActive:
		return next == TaskStatusPaused || next == TaskStatusDone
	case TaskStatusPaused:
		return next == TaskStatusActive || next == TaskStatusDone
	default:
		return false
	}
}

func (t Task) Validate() error {
	if strings.TrimSpace(string(t.ID)) == "" {
		return fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(string(t.RepoID)) == "" {
		return fmt.Errorf("task repo_id is required")
	}
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("task title is required")
	}
	if !t.Status.Valid() {
		return fmt.Errorf("invalid task status %q", t.Status)
	}
	return nil
}

func (w WorkingState) Validate() error {
	if strings.TrimSpace(string(w.ID)) == "" {
		return fmt.Errorf("working state id is required")
	}
	if strings.TrimSpace(string(w.TaskID)) == "" {
		return fmt.Errorf("working state task_id is required")
	}
	if w.Version < 1 {
		return fmt.Errorf("working state version must be >= 1")
	}
	return nil
}
