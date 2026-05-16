package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/storage"
)

var ErrNotFound = storage.ErrNotFound

// Adapter holds SQLite-backed storage implementations.
type Adapter struct {
	DSN string
	db  *sql.DB
}

func New(dsn string) *Adapter {
	return &Adapter{DSN: dsn}
}

func Open(ctx context.Context, dsn string) (*Adapter, error) {
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL;`); err != nil {
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

func (a *Adapter) CreateRepository(ctx context.Context, repository domain.Repository) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}

	_, err := a.db.ExecContext(
		ctx,
		`INSERT INTO repositories (
			id,
			name,
			root_path,
			remote_url,
			default_branch,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		repository.ID,
		repository.Name,
		repository.RootPath,
		repository.RemoteURL,
		repository.DefaultBranch,
		formatTime(repository.CreatedAt),
		formatTime(repository.UpdatedAt),
	)
	return err
}

func (a *Adapter) GetRepository(ctx context.Context, id domain.ID) (domain.Repository, error) {
	if a.db == nil {
		return domain.Repository{}, fmt.Errorf("sqlite adapter is not open")
	}

	return scanRepository(a.db.QueryRowContext(
		ctx,
		`SELECT id, name, root_path, remote_url, default_branch, created_at, updated_at
		FROM repositories
		WHERE id = ?`,
		id,
	))
}

func (a *Adapter) GetRepositoryByRootPath(ctx context.Context, rootPath string) (domain.Repository, error) {
	if a.db == nil {
		return domain.Repository{}, fmt.Errorf("sqlite adapter is not open")
	}

	return scanRepository(a.db.QueryRowContext(
		ctx,
		`SELECT id, name, root_path, remote_url, default_branch, created_at, updated_at
		FROM repositories
		WHERE root_path = ?`,
		rootPath,
	))
}

func (a *Adapter) ListRepositories(ctx context.Context, filter storage.RepositoryFilter) ([]domain.Repository, error) {
	if a.db == nil {
		return nil, fmt.Errorf("sqlite adapter is not open")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	rows, err := a.db.QueryContext(
		ctx,
		`SELECT id, name, root_path, remote_url, default_branch, created_at, updated_at
		FROM repositories
		ORDER BY name ASC, id ASC
		LIMIT ? OFFSET ?`,
		limit,
		filter.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	repositories := []domain.Repository{}
	for rows.Next() {
		repository, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, repository)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return repositories, nil
}

type repositoryScanner interface {
	Scan(dest ...any) error
}

func scanRepository(scanner repositoryScanner) (domain.Repository, error) {
	var repository domain.Repository
	var createdAt string
	var updatedAt string

	err := scanner.Scan(
		&repository.ID,
		&repository.Name,
		&repository.RootPath,
		&repository.RemoteURL,
		&repository.DefaultBranch,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Repository{}, ErrNotFound
	}
	if err != nil {
		return domain.Repository{}, err
	}

	repository.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Repository{}, err
	}
	repository.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.Repository{}, err
	}

	return repository, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// marshalObject serializes a structured map for a NOT NULL JSON text column.
// nil/empty becomes "{}" so the column always holds valid JSON.
func marshalObject(value map[string]any) (string, error) {
	if len(value) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// unmarshalObject parses a JSON text column into target. Empty input is a
// no-op so callers get a nil map rather than an error.
func unmarshalObject(value string, target any) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return json.Unmarshal([]byte(value), target)
}

func formatOptionalTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return formatTime(*value)
}

func parseOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

var _ storage.RepositoryStore = (*Adapter)(nil)
var _ storage.SessionStore = (*Adapter)(nil)
var _ storage.TaskStore = (*Adapter)(nil)
var _ storage.WorkingStateStore = (*Adapter)(nil)
