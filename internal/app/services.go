package app

import "github.com/sirrobot01/mnemo/internal/storage"

// Services groups application storage dependencies. Concrete application
// services (ingestsvc, tasksvc, statesvc, resumesvc) are introduced from
// Milestone R1 onward and will compose these.
type Services struct {
	RepositoryStore   storage.RepositoryStore
	SessionStore      storage.SessionStore
	TaskStore         storage.TaskStore
	WorkingStateStore storage.WorkingStateStore
}
