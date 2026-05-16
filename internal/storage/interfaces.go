package storage

import (
	"context"

	"github.com/sirrobot01/mnemo/internal/domain"
)

type RepositoryFilter struct {
	Limit  int
	Offset int
}

type RepositoryStore interface {
	CreateRepository(ctx context.Context, repository domain.Repository) error
	GetRepository(ctx context.Context, id domain.ID) (domain.Repository, error)
	GetRepositoryByRootPath(ctx context.Context, rootPath string) (domain.Repository, error)
	ListRepositories(ctx context.Context, filter RepositoryFilter) ([]domain.Repository, error)
}

type SessionFilter struct {
	RepoID domain.ID
	Tool   domain.SessionTool
	Status domain.SessionStatus
	Limit  int
	Offset int
}

type SessionEventFilter struct {
	SessionID domain.ID
	Type      domain.SessionEventType
	Limit     int
	Offset    int
}

// SessionStore persists raw session metadata and events ingested from AI
// coding tools. SaveSession upserts by ID so re-ingesting the same source
// file does not duplicate rows.
type SessionStore interface {
	SaveSession(ctx context.Context, session domain.Session) error
	GetSession(ctx context.Context, id domain.ID) (domain.Session, error)
	GetSessionBySource(ctx context.Context, tool domain.SessionTool, sourcePath string) (domain.Session, error)
	ListSessions(ctx context.Context, filter SessionFilter) ([]domain.Session, error)
	DeleteSession(ctx context.Context, id domain.ID) error
	AppendSessionEvents(ctx context.Context, sessionID domain.ID, events []domain.SessionEvent) error
	ListSessionEvents(ctx context.Context, filter SessionEventFilter) ([]domain.SessionEvent, error)
}

type TaskFilter struct {
	RepoID domain.ID
	Status domain.TaskStatus
	Limit  int
	Offset int
}

// TaskStore persists tasks and the sessions threaded into them. SaveTask
// upserts by ID. AttachSession/DetachSession are idempotent.
type TaskStore interface {
	SaveTask(ctx context.Context, task domain.Task) error
	GetTask(ctx context.Context, id domain.ID) (domain.Task, error)
	ListTasks(ctx context.Context, filter TaskFilter) ([]domain.Task, error)
	AttachSession(ctx context.Context, taskID, sessionID domain.ID) error
	DetachSession(ctx context.Context, taskID, sessionID domain.ID) error
	ListTaskSessions(ctx context.Context, taskID domain.ID) ([]domain.ID, error)
	DeleteTask(ctx context.Context, id domain.ID) error
}

// WorkingStateStore persists the versioned compiled state of play per task.
type WorkingStateStore interface {
	SaveWorkingState(ctx context.Context, state domain.WorkingState) error
	GetLatestWorkingState(ctx context.Context, taskID domain.ID) (domain.WorkingState, error)
	ListWorkingStates(ctx context.Context, taskID domain.ID) ([]domain.WorkingState, error)
	DeleteWorkingStates(ctx context.Context, taskID domain.ID) error
}
