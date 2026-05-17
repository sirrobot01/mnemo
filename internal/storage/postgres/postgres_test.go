package postgres

import (
	"testing"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
)

// These cover the shared-backend privacy invariant directly (no DB needed):
// the shared backend must never persist raw content or absolute paths.

func TestSanitizeSessionStripsAbsolutePath(t *testing.T) {
	in := domain.Session{
		ID: "sess_1", RepoID: "repo_1", Tool: domain.SessionToolClaude,
		SourcePath: "/Users/alice/secret-project/.claude/projects/x/s.jsonl",
		Status:     domain.SessionStatusIngested, Branch: "main",
		StartedAt: time.Now(), IngestedAt: time.Now(),
	}
	out := sanitizeSession(in)
	if out.SourcePath != "s.jsonl" {
		t.Fatalf("source path not stripped: %q", out.SourcePath)
	}
	if out.Branch != "main" || out.Tool != domain.SessionToolClaude {
		t.Fatalf("non-sensitive metadata must be preserved: %+v", out)
	}
	// Must still satisfy domain validation (SaveSession validates first).
	if err := out.Validate(); err != nil {
		t.Fatalf("sanitized session no longer valid: %v", err)
	}
}

func TestSanitizeEventDropsContent(t *testing.T) {
	in := domain.SessionEvent{
		ID: "sev_1", SessionID: "sess_1", Sequence: 3,
		Type:            domain.SessionEventTypeUserMessage,
		Content:         "my AWS key is AKIA... and a long secret",
		StructuredValue: map[string]any{"snippet": "sensitive"},
		Timestamp:       time.Now(),
	}
	out := sanitizeEvent(in)
	if out.Content != "" {
		t.Fatalf("content must be dropped, got %q", out.Content)
	}
	if out.StructuredValue != nil {
		t.Fatalf("structured value must be dropped, got %+v", out.StructuredValue)
	}
	if out.ID != "sev_1" || out.Sequence != 3 || out.Type != domain.SessionEventTypeUserMessage {
		t.Fatalf("event shape metadata must be preserved: %+v", out)
	}
}
