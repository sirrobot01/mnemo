package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/logging"
	"github.com/sirrobot01/mnemo/internal/storage"
)

func (a *Adapter) SaveSession(ctx context.Context, session domain.Session) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	if err := session.Validate(); err != nil {
		return err
	}

	_, err := a.db.ExecContext(
		ctx,
		`INSERT INTO sessions (
			id,
			repo_id,
			tool,
			source_path,
			external_id,
			started_at,
			ended_at,
			branch,
			commit_hash,
			message_count,
			status,
			ingested_at,
			created_at,
			updated_at,
			source_fingerprint
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			repo_id = excluded.repo_id,
			tool = excluded.tool,
			source_path = excluded.source_path,
			external_id = excluded.external_id,
			started_at = excluded.started_at,
			ended_at = excluded.ended_at,
			branch = excluded.branch,
			commit_hash = excluded.commit_hash,
			message_count = excluded.message_count,
			status = excluded.status,
			ingested_at = excluded.ingested_at,
			updated_at = excluded.updated_at,
			source_fingerprint = excluded.source_fingerprint`,
		session.ID,
		session.RepoID,
		session.Tool,
		session.SourcePath,
		session.ExternalID,
		formatTime(session.StartedAt),
		formatOptionalTime(session.EndedAt),
		session.Branch,
		session.CommitHash,
		session.MessageCount,
		session.Status,
		formatTime(session.IngestedAt),
		formatTime(session.CreatedAt),
		formatTime(session.UpdatedAt),
		session.SourceFingerprint,
	)
	return err
}

func (a *Adapter) GetSession(ctx context.Context, id domain.ID) (domain.Session, error) {
	if a.db == nil {
		return domain.Session{}, fmt.Errorf("sqlite adapter is not open")
	}
	return scanSession(a.db.QueryRowContext(
		ctx,
		`SELECT id, repo_id, tool, source_path, external_id, started_at, ended_at, branch, commit_hash, message_count, status, ingested_at, created_at, updated_at, source_fingerprint
		FROM sessions
		WHERE id = ?`,
		id,
	))
}

func (a *Adapter) GetSessionBySource(ctx context.Context, tool domain.SessionTool, sourcePath string) (domain.Session, error) {
	if a.db == nil {
		return domain.Session{}, fmt.Errorf("sqlite adapter is not open")
	}
	return scanSession(a.db.QueryRowContext(
		ctx,
		`SELECT id, repo_id, tool, source_path, external_id, started_at, ended_at, branch, commit_hash, message_count, status, ingested_at, created_at, updated_at, source_fingerprint
		FROM sessions
		WHERE tool = ? AND source_path = ?`,
		tool, sourcePath,
	))
}

func (a *Adapter) ListSessions(ctx context.Context, filter storage.SessionFilter) ([]domain.Session, error) {
	if a.db == nil {
		return nil, fmt.Errorf("sqlite adapter is not open")
	}

	where := []string{}
	args := []any{}
	if filter.RepoID != "" {
		where = append(where, "repo_id = ?")
		args = append(args, filter.RepoID)
	}
	if filter.Tool != "" {
		where = append(where, "tool = ?")
		args = append(args, filter.Tool)
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
		`SELECT id, repo_id, tool, source_path, external_id, started_at, ended_at, branch, commit_hash, message_count, status, ingested_at, created_at, updated_at, source_fingerprint
		FROM sessions `+whereSQL+`
		ORDER BY started_at DESC, id ASC
		LIMIT ? OFFSET ?`,
		append(args, limit, filter.Offset)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []domain.Session{}
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (a *Adapter) DeleteSession(ctx context.Context, id domain.ID) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func(tx *sql.Tx) {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			logging.FromContext(ctx).ErrorContext(ctx, "transaction rollback failed", "error", err)
		}
	}(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM session_events WHERE session_id = ?`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (a *Adapter) AppendSessionEvents(ctx context.Context, sessionID domain.ID, events []domain.SessionEvent) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	if len(events) == 0 {
		return nil
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func(tx *sql.Tx) {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			logging.FromContext(ctx).ErrorContext(ctx, "transaction rollback failed", "error", err)
		}
	}(tx)

	for _, event := range events {
		event.SessionID = sessionID
		if err := event.Validate(); err != nil {
			return err
		}
		structured, err := marshalObject(event.StructuredValue)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO session_events (id, session_id, sequence, type, content, timestamp, structured_value, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING`,
			event.ID,
			event.SessionID,
			event.Sequence,
			event.Type,
			event.Content,
			formatTime(event.Timestamp),
			structured,
			formatTime(event.CreatedAt),
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *Adapter) ListSessionEvents(ctx context.Context, filter storage.SessionEventFilter) ([]domain.SessionEvent, error) {
	if a.db == nil {
		return nil, fmt.Errorf("sqlite adapter is not open")
	}

	where := []string{}
	args := []any{}
	if filter.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.Type != "" {
		where = append(where, "type = ?")
		args = append(args, filter.Type)
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 1000
	}

	rows, err := a.db.QueryContext(
		ctx,
		`SELECT id, session_id, sequence, type, content, timestamp, structured_value, created_at
		FROM session_events `+whereSQL+`
		ORDER BY sequence ASC, id ASC
		LIMIT ? OFFSET ?`,
		append(args, limit, filter.Offset)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []domain.SessionEvent{}
	for rows.Next() {
		event, err := scanSessionEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanSession(scanner rowScanner) (domain.Session, error) {
	var session domain.Session
	var startedAt string
	var endedAt sql.NullString
	var ingestedAt string
	var createdAt string
	var updatedAt string

	err := scanner.Scan(
		&session.ID,
		&session.RepoID,
		&session.Tool,
		&session.SourcePath,
		&session.ExternalID,
		&startedAt,
		&endedAt,
		&session.Branch,
		&session.CommitHash,
		&session.MessageCount,
		&session.Status,
		&ingestedAt,
		&createdAt,
		&updatedAt,
		&session.SourceFingerprint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Session{}, ErrNotFound
	}
	if err != nil {
		return domain.Session{}, err
	}

	var parseErr error
	session.StartedAt, parseErr = parseTime(startedAt)
	if parseErr != nil {
		return domain.Session{}, parseErr
	}
	session.EndedAt, parseErr = parseOptionalTime(endedAt)
	if parseErr != nil {
		return domain.Session{}, parseErr
	}
	session.IngestedAt, parseErr = parseTime(ingestedAt)
	if parseErr != nil {
		return domain.Session{}, parseErr
	}
	session.CreatedAt, parseErr = parseTime(createdAt)
	if parseErr != nil {
		return domain.Session{}, parseErr
	}
	session.UpdatedAt, parseErr = parseTime(updatedAt)
	if parseErr != nil {
		return domain.Session{}, parseErr
	}
	return session, nil
}

func scanSessionEvent(scanner rowScanner) (domain.SessionEvent, error) {
	var event domain.SessionEvent
	var structured string
	var timestamp string
	var createdAt string

	if err := scanner.Scan(
		&event.ID,
		&event.SessionID,
		&event.Sequence,
		&event.Type,
		&event.Content,
		&timestamp,
		&structured,
		&createdAt,
	); err != nil {
		return domain.SessionEvent{}, err
	}
	if err := unmarshalObject(structured, &event.StructuredValue); err != nil {
		return domain.SessionEvent{}, err
	}
	var parseErr error
	event.Timestamp, parseErr = parseTime(timestamp)
	if parseErr != nil {
		return domain.SessionEvent{}, parseErr
	}
	event.CreatedAt, parseErr = parseTime(createdAt)
	if parseErr != nil {
		return domain.SessionEvent{}, parseErr
	}
	return event, nil
}
