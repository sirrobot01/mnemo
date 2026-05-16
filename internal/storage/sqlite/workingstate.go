package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sirrobot01/mnemo/internal/domain"
)

// workingStatePayload is the JSON-serialized structured body of a
// WorkingState. The scalar/version columns stay relational for ordering and
// lookup; the rest travels as one opaque payload.
type workingStatePayload struct {
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

func (a *Adapter) SaveWorkingState(ctx context.Context, state domain.WorkingState) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	if err := state.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(workingStatePayload{
		Goal:          state.Goal,
		Done:          state.Done,
		InProgress:    state.InProgress,
		NextSteps:     state.NextSteps,
		Rejected:      state.Rejected,
		Decisions:     state.Decisions,
		OpenQuestions: state.OpenQuestions,
		FilesTouched:  state.FilesTouched,
		Hypotheses:    state.Hypotheses,
	})
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(
		ctx,
		`INSERT INTO working_states (id, task_id, version, compiled_at, source_watermark, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			task_id = excluded.task_id,
			version = excluded.version,
			compiled_at = excluded.compiled_at,
			source_watermark = excluded.source_watermark,
			payload = excluded.payload`,
		state.ID,
		state.TaskID,
		state.Version,
		formatTime(state.CompiledAt),
		state.SourceWatermark,
		string(payload),
		formatTime(state.CreatedAt),
	)
	return err
}

func (a *Adapter) GetLatestWorkingState(ctx context.Context, taskID domain.ID) (domain.WorkingState, error) {
	if a.db == nil {
		return domain.WorkingState{}, fmt.Errorf("sqlite adapter is not open")
	}
	return scanWorkingState(a.db.QueryRowContext(
		ctx,
		`SELECT id, task_id, version, compiled_at, source_watermark, payload, created_at
		FROM working_states WHERE task_id = ?
		ORDER BY version DESC, created_at DESC
		LIMIT 1`,
		taskID,
	))
}

func (a *Adapter) ListWorkingStates(ctx context.Context, taskID domain.ID) ([]domain.WorkingState, error) {
	if a.db == nil {
		return nil, fmt.Errorf("sqlite adapter is not open")
	}
	rows, err := a.db.QueryContext(
		ctx,
		`SELECT id, task_id, version, compiled_at, source_watermark, payload, created_at
		FROM working_states WHERE task_id = ?
		ORDER BY version DESC, created_at DESC`,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := []domain.WorkingState{}
	for rows.Next() {
		state, err := scanWorkingState(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (a *Adapter) DeleteWorkingStates(ctx context.Context, taskID domain.ID) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	_, err := a.db.ExecContext(ctx, `DELETE FROM working_states WHERE task_id = ?`, taskID)
	return err
}

func scanWorkingState(scanner rowScanner) (domain.WorkingState, error) {
	var state domain.WorkingState
	var watermark sql.NullString
	var payload string
	var compiledAt, createdAt string
	err := scanner.Scan(
		&state.ID,
		&state.TaskID,
		&state.Version,
		&compiledAt,
		&watermark,
		&payload,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkingState{}, ErrNotFound
	}
	if err != nil {
		return domain.WorkingState{}, err
	}
	state.SourceWatermark = watermark.String
	var parseErr error
	if state.CompiledAt, parseErr = parseTime(compiledAt); parseErr != nil {
		return domain.WorkingState{}, parseErr
	}
	if state.CreatedAt, parseErr = parseTime(createdAt); parseErr != nil {
		return domain.WorkingState{}, parseErr
	}
	var body workingStatePayload
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		return domain.WorkingState{}, err
	}
	state.Goal = body.Goal
	state.Done = body.Done
	state.InProgress = body.InProgress
	state.NextSteps = body.NextSteps
	state.Rejected = body.Rejected
	state.Decisions = body.Decisions
	state.OpenQuestions = body.OpenQuestions
	state.FilesTouched = body.FilesTouched
	state.Hypotheses = body.Hypotheses
	return state, nil
}
