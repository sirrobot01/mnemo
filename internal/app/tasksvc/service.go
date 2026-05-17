// Package tasksvc owns the task lifecycle and the threading heuristic that
// attaches ingested sessions to the task the user is actually working on.
// A wrong session→task association produces a confidently wrong resume, so
// threading is deliberately conservative: never merge across branches.
package tasksvc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/storage"
)

// DefaultIdleWindow is the activity gap after which a new session on the
// same (repo, branch) starts a fresh task instead of extending the old one.
const DefaultIdleWindow = 45 * time.Minute

// DefaultColdAfter is the no-activity window after which a non-pinned task
// is "cold": it stops surfacing as the active task and is auto-paused by
// Decay. Pinned (explicitly chosen) tasks never decay.
const DefaultColdAfter = 14 * 24 * time.Hour

type Service struct {
	repo       domain.Repository
	tasks      storage.TaskStore
	sessions   storage.SessionStore
	ws         storage.WorkingStateStore
	idleWindow time.Duration
	coldAfter  time.Duration
	now        func() time.Time
}

func New(repo domain.Repository, tasks storage.TaskStore, sessions storage.SessionStore, ws storage.WorkingStateStore, idleWindow time.Duration) *Service {
	if idleWindow <= 0 {
		idleWindow = DefaultIdleWindow
	}
	return &Service{repo: repo, tasks: tasks, sessions: sessions, ws: ws, idleWindow: idleWindow, coldAfter: DefaultColdAfter, now: func() time.Time { return time.Now().UTC() }}
}

// SetColdAfter overrides the decay window. Non-positive values are ignored
// so decay can never be accidentally disabled.
func (s *Service) SetColdAfter(d time.Duration) {
	if d > 0 {
		s.coldAfter = d
	}
}

// isCold reports whether a task should be treated as decayed. Pinned tasks
// (explicit user choice) and done tasks are never "cold" by this rule.
func (s *Service) isCold(t domain.Task) bool {
	if t.Pinned || t.Status == domain.TaskStatusDone {
		return false
	}
	return s.now().Sub(t.LastActiveAt) > s.coldAfter
}

func (s *Service) Start(ctx context.Context, title, goal, branch string) (domain.Task, error) {
	if strings.TrimSpace(title) == "" {
		return domain.Task{}, fmt.Errorf("task title is required")
	}
	now := s.now()
	task := domain.Task{
		ID:           domain.NewID(domain.PrefixTask),
		RepoID:       s.repo.ID,
		Title:        strings.TrimSpace(title),
		Goal:         strings.TrimSpace(goal),
		Status:       domain.TaskStatusActive,
		Branch:       branch,
		Pinned:       true, // explicit start → this is the override task
		CreatedAt:    now,
		UpdatedAt:    now,
		LastActiveAt: now,
	}
	if err := s.tasks.SaveTask(ctx, task); err != nil {
		return domain.Task{}, err
	}
	if err := s.unpinOthers(ctx, task.ID); err != nil {
		return domain.Task{}, err
	}
	return task, nil
}

// unpinOthers clears Pinned on every other task so at most one task in the
// repository is the explicit override at a time.
func (s *Service) unpinOthers(ctx context.Context, keep domain.ID) error {
	tasks, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.ID == keep || !t.Pinned {
			continue
		}
		t.Pinned = false
		t.UpdatedAt = s.now()
		if err := s.tasks.SaveTask(ctx, t); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]domain.Task, error) {
	return s.tasks.ListTasks(ctx, storage.TaskFilter{RepoID: s.repo.ID})
}

func (s *Service) Get(ctx context.Context, id domain.ID) (domain.Task, error) {
	return s.tasks.GetTask(ctx, id)
}

func (s *Service) Sessions(ctx context.Context, taskID domain.ID) ([]domain.ID, error) {
	return s.tasks.ListTaskSessions(ctx, taskID)
}

func (s *Service) transition(ctx context.Context, id domain.ID, next domain.TaskStatus) (domain.Task, error) {
	task, err := s.tasks.GetTask(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	if !task.Status.CanTransitionTo(next) {
		return domain.Task{}, fmt.Errorf("task %s cannot transition from %s to %s", id, task.Status, next)
	}
	task.Status = next
	task.UpdatedAt = s.now()
	switch next {
	case domain.TaskStatusActive:
		// `task switch` makes this the explicit override.
		task.LastActiveAt = task.UpdatedAt
		task.Pinned = true
	case domain.TaskStatusPaused, domain.TaskStatusDone:
		// Leaving active relinquishes the override; threading falls back
		// to the branch heuristic.
		task.Pinned = false
	}
	if err := s.tasks.SaveTask(ctx, task); err != nil {
		return domain.Task{}, err
	}
	if next == domain.TaskStatusActive {
		if err := s.unpinOthers(ctx, task.ID); err != nil {
			return domain.Task{}, err
		}
	}
	return task, nil
}

func (s *Service) Switch(ctx context.Context, id domain.ID) (domain.Task, error) {
	return s.transition(ctx, id, domain.TaskStatusActive)
}

func (s *Service) Pause(ctx context.Context, id domain.ID) (domain.Task, error) {
	return s.transition(ctx, id, domain.TaskStatusPaused)
}

func (s *Service) Done(ctx context.Context, id domain.ID) (domain.Task, error) {
	return s.transition(ctx, id, domain.TaskStatusDone)
}

// Active returns the most-recently-active non-done task for the repository.
func (s *Service) Active(ctx context.Context) (domain.Task, bool, error) {
	tasks, err := s.List(ctx)
	if err != nil {
		return domain.Task{}, false, err
	}
	var best domain.Task
	found := false
	for _, t := range tasks {
		if t.Status == domain.TaskStatusDone || s.isCold(t) {
			continue
		}
		if !found || t.LastActiveAt.After(best.LastActiveAt) {
			best, found = t, true
		}
	}
	return best, found, nil
}

// Decay auto-pauses cold, active, non-pinned tasks so their stale state of
// play stops being offered. It is idempotent and safe to call on every
// watch sweep. Returns the number of tasks paused.
func (s *Service) Decay(ctx context.Context) (int, error) {
	tasks, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	paused := 0
	for _, t := range tasks {
		if t.Status != domain.TaskStatusActive || !s.isCold(t) {
			continue
		}
		t.Status = domain.TaskStatusPaused
		t.UpdatedAt = s.now()
		if err := s.tasks.SaveTask(ctx, t); err != nil {
			return paused, err
		}
		paused++
	}
	return paused, nil
}

// ForgetTask removes the task, its working states, and its session links.
// Sessions themselves and their on-disk source are untouched.
func (s *Service) ForgetTask(ctx context.Context, id domain.ID) error {
	if err := s.ws.DeleteWorkingStates(ctx, id); err != nil {
		return err
	}
	return s.tasks.DeleteTask(ctx, id)
}

// Thread attaches every not-yet-threaded session in the repository to a
// task using the (repo + branch + idle window) heuristic. It returns the
// number of sessions newly attached.
func (s *Service) Thread(ctx context.Context) (int, error) {
	all, err := s.sessions.ListSessions(ctx, storage.SessionFilter{RepoID: s.repo.ID})
	if err != nil {
		return 0, err
	}
	tasks, err := s.List(ctx)
	if err != nil {
		return 0, err
	}

	attached := map[domain.ID]bool{}
	for _, t := range tasks {
		ids, err := s.tasks.ListTaskSessions(ctx, t.ID)
		if err != nil {
			return 0, err
		}
		for _, id := range ids {
			attached[id] = true
		}
	}

	// Oldest sessions first so a task accretes in chronological order.
	sort.Slice(all, func(i, j int) bool { return all[i].StartedAt.Before(all[j].StartedAt) })

	// Explicit override wins: if a non-done pinned task exists, every
	// un-threaded session attaches to it regardless of branch.
	for i := range tasks {
		pinned := tasks[i]
		if !pinned.Pinned || pinned.Status == domain.TaskStatusDone {
			continue
		}
		count := 0
		for _, sess := range all {
			if attached[sess.ID] {
				continue
			}
			if err := s.tasks.AttachSession(ctx, pinned.ID, sess.ID); err != nil {
				return count, err
			}
			activity := sess.StartedAt
			if sess.EndedAt != nil && sess.EndedAt.After(activity) {
				activity = *sess.EndedAt
			}
			if activity.After(pinned.LastActiveAt) {
				if err := s.touch(ctx, &pinned, activity, tasks); err != nil {
					return count, err
				}
			}
			attached[sess.ID] = true
			count++
		}
		return count, nil
	}

	count := 0
	for _, sess := range all {
		if attached[sess.ID] {
			continue
		}
		activity := sess.StartedAt
		if sess.EndedAt != nil && sess.EndedAt.After(activity) {
			activity = *sess.EndedAt
		}

		target, ok := s.candidate(tasks, sess.Branch, activity)
		if !ok {
			now := s.now()
			target = domain.Task{
				ID:           domain.NewID(domain.PrefixTask),
				RepoID:       s.repo.ID,
				Title:        titleFor(sess),
				Status:       domain.TaskStatusActive,
				Branch:       sess.Branch,
				CreatedAt:    now,
				UpdatedAt:    now,
				LastActiveAt: activity,
			}
			if err := s.tasks.SaveTask(ctx, target); err != nil {
				return count, err
			}
			tasks = append(tasks, target)
		}

		if err := s.tasks.AttachSession(ctx, target.ID, sess.ID); err != nil {
			return count, err
		}
		if activity.After(target.LastActiveAt) {
			if err := s.touch(ctx, &target, activity, tasks); err != nil {
				return count, err
			}
		}
		attached[sess.ID] = true
		count++
	}
	return count, nil
}

// candidate finds the most-recently-active non-done task on the same branch
// whose last activity is within the idle window of the session's activity.
func (s *Service) candidate(tasks []domain.Task, branch string, activity time.Time) (domain.Task, bool) {
	var best domain.Task
	found := false
	for _, t := range tasks {
		if t.Status == domain.TaskStatusDone || t.Branch != branch {
			continue
		}
		gap := activity.Sub(t.LastActiveAt)
		if gap < 0 {
			gap = -gap
		}
		if gap > s.idleWindow {
			continue
		}
		if !found || t.LastActiveAt.After(best.LastActiveAt) {
			best, found = t, true
		}
	}
	return best, found
}

func (s *Service) touch(ctx context.Context, target *domain.Task, activity time.Time, tasks []domain.Task) error {
	target.LastActiveAt = activity
	target.UpdatedAt = s.now()
	for i := range tasks {
		if tasks[i].ID == target.ID {
			tasks[i] = *target
		}
	}
	return s.tasks.SaveTask(ctx, *target)
}

func titleFor(sess domain.Session) string {
	if strings.TrimSpace(sess.Branch) != "" {
		return "Work on " + sess.Branch
	}
	id := sess.ExternalID
	if len(id) > 12 {
		id = id[:12]
	}
	if id == "" {
		id = string(sess.Tool)
	}
	return "Session " + id
}

// ErrNoActiveTask is returned when an operation needs an active task but the
// repository has none.
var ErrNoActiveTask = errors.New("no active task for this repository")
