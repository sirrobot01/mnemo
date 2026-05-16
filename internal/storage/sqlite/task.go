package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"database/sql"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/storage"
)

func (a *Adapter) SaveTask(ctx context.Context, task domain.Task) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	if err := task.Validate(); err != nil {
		return err
	}
	_, err := a.db.ExecContext(
		ctx,
		`INSERT INTO tasks (id, repo_id, title, goal, status, branch, pinned, created_at, updated_at, last_active_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			repo_id = excluded.repo_id,
			title = excluded.title,
			goal = excluded.goal,
			status = excluded.status,
			branch = excluded.branch,
			pinned = excluded.pinned,
			updated_at = excluded.updated_at,
			last_active_at = excluded.last_active_at`,
		task.ID,
		task.RepoID,
		task.Title,
		task.Goal,
		task.Status,
		task.Branch,
		boolToInt(task.Pinned),
		formatTime(task.CreatedAt),
		formatTime(task.UpdatedAt),
		formatTime(task.LastActiveAt),
	)
	return err
}

func (a *Adapter) GetTask(ctx context.Context, id domain.ID) (domain.Task, error) {
	if a.db == nil {
		return domain.Task{}, fmt.Errorf("sqlite adapter is not open")
	}
	return scanTask(a.db.QueryRowContext(
		ctx,
		`SELECT id, repo_id, title, goal, status, branch, pinned, created_at, updated_at, last_active_at
		FROM tasks WHERE id = ?`,
		id,
	))
}

func (a *Adapter) ListTasks(ctx context.Context, filter storage.TaskFilter) ([]domain.Task, error) {
	if a.db == nil {
		return nil, fmt.Errorf("sqlite adapter is not open")
	}
	where := []string{}
	args := []any{}
	if filter.RepoID != "" {
		where = append(where, "repo_id = ?")
		args = append(args, filter.RepoID)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := a.db.QueryContext(
		ctx,
		`SELECT id, repo_id, title, goal, status, branch, pinned, created_at, updated_at, last_active_at
		FROM tasks `+whereSQL+`
		ORDER BY last_active_at DESC, id ASC
		LIMIT ? OFFSET ?`,
		append(args, limit, filter.Offset)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []domain.Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (a *Adapter) AttachSession(ctx context.Context, taskID, sessionID domain.ID) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	_, err := a.db.ExecContext(
		ctx,
		`INSERT INTO task_sessions (task_id, session_id, attached_at)
		VALUES (?, ?, ?)
		ON CONFLICT(task_id, session_id) DO NOTHING`,
		taskID, sessionID, formatTime(time.Now().UTC()),
	)
	return err
}

func (a *Adapter) DetachSession(ctx context.Context, taskID, sessionID domain.ID) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	_, err := a.db.ExecContext(ctx, `DELETE FROM task_sessions WHERE task_id = ? AND session_id = ?`, taskID, sessionID)
	return err
}

func (a *Adapter) ListTaskSessions(ctx context.Context, taskID domain.ID) ([]domain.ID, error) {
	if a.db == nil {
		return nil, fmt.Errorf("sqlite adapter is not open")
	}
	rows, err := a.db.QueryContext(
		ctx,
		`SELECT session_id FROM task_sessions WHERE task_id = ? ORDER BY attached_at ASC, session_id ASC`,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []domain.ID{}
	for rows.Next() {
		var id domain.ID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (a *Adapter) DeleteTask(ctx context.Context, id domain.ID) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	if _, err := a.db.ExecContext(ctx, `DELETE FROM task_sessions WHERE task_id = ?`, id); err != nil {
		return err
	}
	_, err := a.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	return err
}

func scanTask(scanner rowScanner) (domain.Task, error) {
	var task domain.Task
	var goal sql.NullString
	var branch sql.NullString
	var pinned int
	var createdAt, updatedAt, lastActiveAt string
	err := scanner.Scan(
		&task.ID,
		&task.RepoID,
		&task.Title,
		&goal,
		&task.Status,
		&branch,
		&pinned,
		&createdAt,
		&updatedAt,
		&lastActiveAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, ErrNotFound
	}
	if err != nil {
		return domain.Task{}, err
	}
	task.Goal = goal.String
	task.Branch = branch.String
	task.Pinned = pinned != 0
	var parseErr error
	if task.CreatedAt, parseErr = parseTime(createdAt); parseErr != nil {
		return domain.Task{}, parseErr
	}
	if task.UpdatedAt, parseErr = parseTime(updatedAt); parseErr != nil {
		return domain.Task{}, parseErr
	}
	if task.LastActiveAt, parseErr = parseTime(lastActiveAt); parseErr != nil {
		return domain.Task{}, parseErr
	}
	return task, nil
}
