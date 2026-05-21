// Package postgres is the PostgreSQL storage adapter for repositories,
// sessions, tasks, and working states.
//
// PostgreSQL is the metadata backend. It must NEVER receive raw transcript
// content or absolute paths — only metadata and the compiled,
// already-secret-scanned working state. That invariant is
// enforced here at the persistence boundary (see sanitizeSession /
// sanitizeEvent), independent of any caller, so a misconfigured surface
// cannot leak. Build-validated; live integration needs a configured DB.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/storage"
)

var ErrNotFound = storage.ErrNotFound

type Adapter struct {
	DSN string
	db  *sql.DB
}

func New(dsn string) *Adapter { return &Adapter{DSN: dsn} }

func Open(ctx context.Context, dsn string) (*Adapter, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Adapter{DSN: dsn, db: db}, nil
}

func (a *Adapter) Close() error {
	if a.db == nil {
		return nil
	}
	return a.db.Close()
}

func (a *Adapter) conn() (*sql.DB, error) {
	if a.db == nil {
		return nil, fmt.Errorf("postgres adapter is not open")
	}
	return a.db, nil
}

// ---------- repositories ----------

func (a *Adapter) CreateRepository(ctx context.Context, r domain.Repository) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO repositories (id, name, root_path, remote_url, default_branch, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		r.ID, r.Name, r.RootPath, r.RemoteURL, r.DefaultBranch, r.CreatedAt.UTC(), r.UpdatedAt.UTC(),
	)
	return err
}

func (a *Adapter) GetRepository(ctx context.Context, id domain.ID) (domain.Repository, error) {
	db, err := a.conn()
	if err != nil {
		return domain.Repository{}, err
	}
	return scanRepository(db.QueryRowContext(
		ctx,
		`SELECT id, name, root_path, remote_url, default_branch, created_at, updated_at
		FROM repositories WHERE id = $1`, id,
	))
}

func (a *Adapter) GetRepositoryByRootPath(ctx context.Context, rootPath string) (domain.Repository, error) {
	db, err := a.conn()
	if err != nil {
		return domain.Repository{}, err
	}
	return scanRepository(db.QueryRowContext(
		ctx,
		`SELECT id, name, root_path, remote_url, default_branch, created_at, updated_at
		FROM repositories WHERE root_path = $1`, rootPath,
	))
}

func (a *Adapter) ListRepositories(ctx context.Context, filter storage.RepositoryFilter) ([]domain.Repository, error) {
	db, err := a.conn()
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.QueryContext(
		ctx,
		`SELECT id, name, root_path, remote_url, default_branch, created_at, updated_at
		FROM repositories ORDER BY name ASC, id ASC LIMIT $1 OFFSET $2`,
		limit, filter.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Repository{}
	for rows.Next() {
		r, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanRepository(s rowScanner) (domain.Repository, error) {
	var r domain.Repository
	err := s.Scan(&r.ID, &r.Name, &r.RootPath, &r.RemoteURL, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Repository{}, ErrNotFound
	}
	return r, err
}

// ---------- sessions ----------

func (a *Adapter) SaveSession(ctx context.Context, sess domain.Session) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	if err := sess.Validate(); err != nil {
		return err
	}
	sess = sanitizeSession(sess)
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO sessions (
			id, repo_id, agent, kind, source_path, external_id, started_at, ended_at,
			branch, commit_hash, message_count, status, ingested_at, created_at, updated_at,
			source_fingerprint
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (id) DO UPDATE SET
			repo_id = EXCLUDED.repo_id,
			agent = EXCLUDED.agent,
			kind = EXCLUDED.kind,
			source_path = EXCLUDED.source_path,
			external_id = EXCLUDED.external_id,
			started_at = EXCLUDED.started_at,
			ended_at = EXCLUDED.ended_at,
			branch = EXCLUDED.branch,
			commit_hash = EXCLUDED.commit_hash,
			message_count = EXCLUDED.message_count,
			status = EXCLUDED.status,
			ingested_at = EXCLUDED.ingested_at,
			updated_at = EXCLUDED.updated_at,
			source_fingerprint = EXCLUDED.source_fingerprint`,
		sess.ID, sess.RepoID, sess.Agent, sess.Kind, sess.SourcePath, sess.ExternalID,
		sess.StartedAt.UTC(), optionalTime(sess.EndedAt), sess.Branch, sess.CommitHash,
		sess.MessageCount, sess.Status, sess.IngestedAt.UTC(), sess.CreatedAt.UTC(), sess.UpdatedAt.UTC(),
		sess.SourceFingerprint,
	)
	return err
}

func (a *Adapter) GetSession(ctx context.Context, id domain.ID) (domain.Session, error) {
	db, err := a.conn()
	if err != nil {
		return domain.Session{}, err
	}
	return scanSession(db.QueryRowContext(ctx, sessionSelect+` WHERE id = $1`, id))
}

func (a *Adapter) GetSessionBySource(ctx context.Context, agent string, sourcePath string) (domain.Session, error) {
	db, err := a.conn()
	if err != nil {
		return domain.Session{}, err
	}
	return scanSession(db.QueryRowContext(ctx, sessionSelect+` WHERE agent = $1 AND source_path = $2`, agent, sourcePath))
}

func (a *Adapter) ListSessions(ctx context.Context, filter storage.SessionFilter) ([]domain.Session, error) {
	db, err := a.conn()
	if err != nil {
		return nil, err
	}
	where := []string{}
	args := []any{}
	if filter.RepoID != "" {
		args = append(args, filter.RepoID)
		where = append(where, fmt.Sprintf("repo_id = $%d", len(args)))
	}
	if filter.Agent != "" {
		args = append(args, filter.Agent)
		where = append(where, fmt.Sprintf("agent = $%d", len(args)))
	}
	if filter.Kind != "" {
		args = append(args, filter.Kind)
		where = append(where, fmt.Sprintf("kind = $%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit, filter.Offset)
	q := fmt.Sprintf(sessionSelect+"%s ORDER BY started_at DESC, id ASC LIMIT $%d OFFSET $%d", clause, len(args)-1, len(args))
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Session{}
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (a *Adapter) DeleteSession(ctx context.Context, id domain.ID) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM session_events WHERE session_id = $1`, id); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

func (a *Adapter) AppendSessionEvents(ctx context.Context, sessionID domain.ID, events []domain.SessionEvent) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	for _, e := range events {
		if err := e.Validate(); err != nil {
			return err
		}
		e = sanitizeEvent(e)
		sv := []byte("{}")
		if e.StructuredValue != nil {
			sv, err = json.Marshal(e.StructuredValue)
			if err != nil {
				return err
			}
		}
		_, err = db.ExecContext(
			ctx,
			`INSERT INTO session_events (id, session_id, sequence, type, content, timestamp, structured_value, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
			ON CONFLICT (id) DO NOTHING`,
			e.ID, sessionID, e.Sequence, e.Type, e.Content, e.Timestamp.UTC(), string(sv), e.CreatedAt.UTC(),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) ListSessionEvents(ctx context.Context, filter storage.SessionEventFilter) ([]domain.SessionEvent, error) {
	db, err := a.conn()
	if err != nil {
		return nil, err
	}
	where := []string{"session_id = $1"}
	args := []any{filter.SessionID}
	if filter.Type != "" {
		args = append(args, filter.Type)
		where = append(where, fmt.Sprintf("type = $%d", len(args)))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 1000
	}
	args = append(args, limit, filter.Offset)
	q := fmt.Sprintf(
		`SELECT id, session_id, sequence, type, content, timestamp, structured_value, created_at
		FROM session_events WHERE %s ORDER BY sequence ASC, id ASC LIMIT $%d OFFSET $%d`,
		strings.Join(where, " AND "), len(args)-1, len(args),
	)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SessionEvent{}
	for rows.Next() {
		var e domain.SessionEvent
		var raw []byte
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Sequence, &e.Type, &e.Content, &e.Timestamp, &raw, &e.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &e.StructuredValue)
		}
		// Normalize an empty/`{}` payload to nil so the Postgres and
		// SQLite adapters round-trip identically (jsonb defaults to '{}').
		if len(e.StructuredValue) == 0 {
			e.StructuredValue = nil
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

const sessionSelect = `SELECT id, repo_id, agent, kind, source_path, external_id, started_at, ended_at,
	branch, commit_hash, message_count, status, ingested_at, created_at, updated_at,
	source_fingerprint FROM sessions`

func scanSession(s rowScanner) (domain.Session, error) {
	var sess domain.Session
	var ended sql.NullTime
	err := s.Scan(
		&sess.ID, &sess.RepoID, &sess.Agent, &sess.Kind, &sess.SourcePath, &sess.ExternalID,
		&sess.StartedAt, &ended, &sess.Branch, &sess.CommitHash, &sess.MessageCount,
		&sess.Status, &sess.IngestedAt, &sess.CreatedAt, &sess.UpdatedAt,
		&sess.SourceFingerprint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Session{}, ErrNotFound
	}
	if err != nil {
		return domain.Session{}, err
	}
	if ended.Valid {
		t := ended.Time
		sess.EndedAt = &t
	}
	return sess, nil
}

// ---------- tasks ----------

func (a *Adapter) SaveTask(ctx context.Context, t domain.Task) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	if err := t.Validate(); err != nil {
		return err
	}
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO tasks (id, repo_id, title, goal, status, branch, pinned, created_at, updated_at, last_active_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET
			repo_id = EXCLUDED.repo_id,
			title = EXCLUDED.title,
			goal = EXCLUDED.goal,
			status = EXCLUDED.status,
			branch = EXCLUDED.branch,
			pinned = EXCLUDED.pinned,
			updated_at = EXCLUDED.updated_at,
			last_active_at = EXCLUDED.last_active_at`,
		t.ID, t.RepoID, t.Title, t.Goal, t.Status, t.Branch, t.Pinned,
		t.CreatedAt.UTC(), t.UpdatedAt.UTC(), t.LastActiveAt.UTC(),
	)
	return err
}

func (a *Adapter) GetTask(ctx context.Context, id domain.ID) (domain.Task, error) {
	db, err := a.conn()
	if err != nil {
		return domain.Task{}, err
	}
	return scanTask(db.QueryRowContext(
		ctx,
		`SELECT id, repo_id, title, goal, status, branch, pinned, created_at, updated_at, last_active_at
		FROM tasks WHERE id = $1`, id,
	))
}

func (a *Adapter) ListTasks(ctx context.Context, filter storage.TaskFilter) ([]domain.Task, error) {
	db, err := a.conn()
	if err != nil {
		return nil, err
	}
	where := []string{}
	args := []any{}
	if filter.RepoID != "" {
		args = append(args, filter.RepoID)
		where = append(where, fmt.Sprintf("repo_id = $%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit, filter.Offset)
	q := fmt.Sprintf(
		`SELECT id, repo_id, title, goal, status, branch, pinned, created_at, updated_at, last_active_at
		FROM tasks%s ORDER BY last_active_at DESC, id ASC LIMIT $%d OFFSET $%d`,
		clause, len(args)-1, len(args),
	)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (a *Adapter) AttachSession(ctx context.Context, taskID, sessionID domain.ID) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO task_sessions (task_id, session_id, attached_at) VALUES ($1,$2,$3)
		ON CONFLICT (task_id, session_id) DO NOTHING`,
		taskID, sessionID, time.Now().UTC(),
	)
	return err
}

func (a *Adapter) DetachSession(ctx context.Context, taskID, sessionID domain.ID) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM task_sessions WHERE task_id = $1 AND session_id = $2`, taskID, sessionID)
	return err
}

func (a *Adapter) ListTaskSessions(ctx context.Context, taskID domain.ID) ([]domain.ID, error) {
	db, err := a.conn()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(
		ctx,
		`SELECT session_id FROM task_sessions WHERE task_id = $1 ORDER BY attached_at ASC, session_id ASC`,
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
	db, err := a.conn()
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM task_sessions WHERE task_id = $1`, id); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM tasks WHERE id = $1`, id)
	return err
}

func scanTask(s rowScanner) (domain.Task, error) {
	var t domain.Task
	err := s.Scan(&t.ID, &t.RepoID, &t.Title, &t.Goal, &t.Status, &t.Branch, &t.Pinned, &t.CreatedAt, &t.UpdatedAt, &t.LastActiveAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, ErrNotFound
	}
	return t, err
}

// ---------- working states ----------

func (a *Adapter) SaveWorkingState(ctx context.Context, ws domain.WorkingState) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	if err := ws.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(workingStateBody(ws))
	if err != nil {
		return err
	}
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO working_states (id, task_id, version, compiled_at, source_watermark, payload, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (id) DO UPDATE SET
			task_id = EXCLUDED.task_id,
			version = EXCLUDED.version,
			compiled_at = EXCLUDED.compiled_at,
			source_watermark = EXCLUDED.source_watermark,
			payload = EXCLUDED.payload`,
		ws.ID, ws.TaskID, ws.Version, ws.CompiledAt.UTC(), ws.SourceWatermark, string(payload), ws.CreatedAt.UTC(),
	)
	return err
}

func (a *Adapter) GetLatestWorkingState(ctx context.Context, taskID domain.ID) (domain.WorkingState, error) {
	db, err := a.conn()
	if err != nil {
		return domain.WorkingState{}, err
	}
	return scanWorkingState(db.QueryRowContext(
		ctx,
		`SELECT id, task_id, version, compiled_at, source_watermark, payload, created_at
		FROM working_states WHERE task_id = $1 ORDER BY version DESC, created_at DESC LIMIT 1`,
		taskID,
	))
}

func (a *Adapter) ListWorkingStates(ctx context.Context, taskID domain.ID) ([]domain.WorkingState, error) {
	db, err := a.conn()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(
		ctx,
		`SELECT id, task_id, version, compiled_at, source_watermark, payload, created_at
		FROM working_states WHERE task_id = $1 ORDER BY version DESC, created_at DESC`,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.WorkingState{}
	for rows.Next() {
		ws, err := scanWorkingState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

func (a *Adapter) DeleteWorkingStates(ctx context.Context, taskID domain.ID) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM working_states WHERE task_id = $1`, taskID)
	return err
}

type wsBody struct {
	Goal          string                    `json:"goal,omitempty"`
	Done          []string                  `json:"done,omitempty"`
	InProgress    string                    `json:"in_progress,omitempty"`
	NextSteps     []string                  `json:"next_steps,omitempty"`
	Rejected      []domain.RejectedApproach `json:"rejected,omitempty"`
	Decisions     []domain.Decision         `json:"decisions,omitempty"`
	OpenQuestions []string                  `json:"open_questions,omitempty"`
	FilesTouched  []domain.FileTouched      `json:"files_touched,omitempty"`
	Hypotheses    []domain.Hypothesis       `json:"hypotheses,omitempty"`
}

func workingStateBody(ws domain.WorkingState) wsBody {
	return wsBody{
		Goal: ws.Goal, Done: ws.Done, InProgress: ws.InProgress, NextSteps: ws.NextSteps,
		Rejected: ws.Rejected, Decisions: ws.Decisions, OpenQuestions: ws.OpenQuestions,
		FilesTouched: ws.FilesTouched, Hypotheses: ws.Hypotheses,
	}
}

func scanWorkingState(s rowScanner) (domain.WorkingState, error) {
	var ws domain.WorkingState
	var payload string
	err := s.Scan(&ws.ID, &ws.TaskID, &ws.Version, &ws.CompiledAt, &ws.SourceWatermark, &payload, &ws.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkingState{}, ErrNotFound
	}
	if err != nil {
		return domain.WorkingState{}, err
	}
	var body wsBody
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		return domain.WorkingState{}, err
	}
	ws.Goal = body.Goal
	ws.Done = body.Done
	ws.InProgress = body.InProgress
	ws.NextSteps = body.NextSteps
	ws.Rejected = body.Rejected
	ws.Decisions = body.Decisions
	ws.OpenQuestions = body.OpenQuestions
	ws.FilesTouched = body.FilesTouched
	ws.Hypotheses = body.Hypotheses
	return ws, nil
}

// ---------- helpers ----------

type rowScanner interface {
	Scan(dest ...any) error
}

// sanitizeSession strips the absolute source path so the shared DB only holds
// a non-identifying basename. Session metadata (tool, branch, commit, counts,
// timestamps, status) is not sensitive.
func sanitizeSession(s domain.Session) domain.Session {
	if s.SourcePath != "" {
		s.SourcePath = filepath.Base(s.SourcePath)
	}
	return s
}

// sanitizeEvent drops raw transcript content and the structured payload.
// Only the non-sensitive shape (id, session, sequence, type, timestamp) of
// the event is shared; the substance never leaves the local SQLite store.
func sanitizeEvent(e domain.SessionEvent) domain.SessionEvent {
	e.Content = ""
	e.StructuredValue = nil
	return e
}

func optionalTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC()
}

var _ storage.RepositoryStore = (*Adapter)(nil)
var _ storage.SessionStore = (*Adapter)(nil)
var _ storage.TaskStore = (*Adapter)(nil)
var _ storage.WorkingStateStore = (*Adapter)(nil)
