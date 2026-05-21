package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	postgresmigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	sqlitemigrate "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

//go:embed sqlite/*.sql postgres/*.sql
var files embed.FS

type Strategy string

const (
	StrategyEmbeddedSQL Strategy = "embedded-sql"
)

// Plan describes how migrations will be executed for a database backend.
type Plan struct {
	DatabaseType string
	Strategy     Strategy
	Description  string
}

type Migration struct {
	Version string
	Name    string
	SQL     string
}

type Status struct {
	Applied []Migration
	Pending []Migration
	Version uint
	Dirty   bool
}

type ApplyResult struct {
	Applied []Migration
	Skipped []Migration
}

// PlanFor returns the migration plan for the configured database backend.
func PlanFor(databaseType string) Plan {
	switch databaseType {
	case "postgres":
		return Plan{
			DatabaseType: databaseType,
			Strategy:     StrategyEmbeddedSQL,
			Description:  "embedded ordered SQL migrations for PostgreSQL",
		}
	default:
		return Plan{
			DatabaseType: "sqlite",
			Strategy:     StrategyEmbeddedSQL,
			Description:  "embedded ordered SQL migrations for SQLite",
		}
	}
}

func List(databaseType string) ([]Migration, error) {
	plan := PlanFor(databaseType)
	dir := plan.DatabaseType

	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}

		content, err := files.ReadFile(fmt.Sprintf("%s/%s", dir, entry.Name()))
		if err != nil {
			return nil, err
		}

		version, name := parseMigrationName(entry.Name())
		migrations = append(migrations, Migration{
			Version: version,
			Name:    name,
			SQL:     string(content),
		})
	}

	return migrations, nil
}

func PendingCount(databaseType string, version uint, hasVersion bool) (int, error) {
	plan := PlanFor(databaseType)
	entries, err := fs.ReadDir(files, plan.DatabaseType)
	if err != nil {
		return 0, err
	}

	pending := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		migrationVersion, err := strconv.ParseUint(parseMigrationVersion(entry.Name()), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse migration version %q: %w", entry.Name(), err)
		}
		if !hasVersion || uint(migrationVersion) > version {
			pending++
		}
	}
	return pending, nil
}

func ApplySQLite(ctx context.Context, dsn string) (ApplyResult, error) {
	before, err := StatusSQLite(ctx, dsn)
	if err != nil {
		return ApplyResult{}, err
	}

	instance, closeFn, err := newSQLiteMigrator(ctx, dsn)
	if err != nil {
		return ApplyResult{}, err
	}
	defer closeFn()

	if err := instance.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return ApplyResult{}, err
	}

	after, err := StatusSQLite(ctx, dsn)
	if err != nil {
		return ApplyResult{}, err
	}

	return ApplyResult{
		Applied: migrationsAppliedBetween(before.Version, after.Version, before.Pending),
		Skipped: before.Applied,
	}, nil
}

func ApplyPostgres(ctx context.Context, dsn string) (ApplyResult, error) {
	before, err := StatusPostgres(ctx, dsn)
	if err != nil {
		return ApplyResult{}, err
	}

	instance, closeFn, err := newPostgresMigrator(ctx, dsn)
	if err != nil {
		return ApplyResult{}, err
	}
	defer closeFn()

	if err := instance.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return ApplyResult{}, err
	}

	after, err := StatusPostgres(ctx, dsn)
	if err != nil {
		return ApplyResult{}, err
	}

	return ApplyResult{
		Applied: migrationsAppliedBetween(before.Version, after.Version, before.Pending),
		Skipped: before.Applied,
	}, nil
}

func StatusSQLite(ctx context.Context, dsn string) (Status, error) {
	all, err := List("sqlite")
	if err != nil {
		return Status{}, err
	}

	if _, err := os.Stat(dsn); errors.Is(err, os.ErrNotExist) {
		return statusFromVersion(all, 0, false, false)
	} else if err != nil {
		return Status{}, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return Status{}, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return Status{}, err
	}

	var tableCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&tableCount); err != nil {
		return Status{}, err
	}
	if tableCount == 0 {
		return statusFromVersion(all, 0, false, false)
	}

	var version uint
	var dirty bool
	if err := db.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); errors.Is(err, sql.ErrNoRows) {
		return statusFromVersion(all, 0, false, false)
	} else if err != nil {
		return Status{}, err
	}

	return statusFromVersion(all, version, dirty, true)
}

func StatusPostgres(ctx context.Context, dsn string) (Status, error) {
	instance, closeFn, err := newPostgresMigrator(ctx, dsn)
	if err != nil {
		return Status{}, err
	}
	defer closeFn()

	all, err := List("postgres")
	if err != nil {
		return Status{}, err
	}

	version, dirty, err := instance.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return Status{}, err
	}

	return statusFromVersion(all, version, dirty, !errors.Is(err, migrate.ErrNilVersion))
}

func statusFromVersion(all []Migration, version uint, dirty bool, hasVersion bool) (Status, error) {
	status := Status{Version: version, Dirty: dirty}
	for _, migration := range all {
		migrationVersion, err := strconv.ParseUint(migration.Version, 10, 64)
		if err != nil {
			return Status{}, fmt.Errorf("parse migration version %q: %w", migration.Version, err)
		}

		if hasVersion && uint(migrationVersion) <= version {
			status.Applied = append(status.Applied, migration)
		} else {
			status.Pending = append(status.Pending, migration)
		}
	}

	return status, nil
}

func parseMigrationName(filename string) (string, string) {
	for i, r := range filename {
		if r == '_' {
			return filename[:i], strings.TrimSuffix(strings.TrimSuffix(filename[i+1:], ".up.sql"), ".down.sql")
		}
	}

	return filename, filename
}

func parseMigrationVersion(filename string) string {
	version, _ := parseMigrationName(filename)
	return version
}

func newSQLiteMigrator(ctx context.Context, dsn string) (*migrate.Migrate, func(), error) {
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return nil, nil, err
	}

	source, err := iofs.New(files, "sqlite")
	if err != nil {
		return nil, nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		source.Close()
		return nil, nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		source.Close()
		db.Close()
		return nil, nil, err
	}

	driver, err := sqlitemigrate.WithInstance(db, &sqlitemigrate.Config{})
	if err != nil {
		source.Close()
		db.Close()
		return nil, nil, err
	}

	instance, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		source.Close()
		db.Close()
		return nil, nil, err
	}

	closeFn := func() {
		instance.Close()
		db.Close()
	}

	return instance, closeFn, nil
}

func newPostgresMigrator(ctx context.Context, dsn string) (*migrate.Migrate, func(), error) {
	source, err := iofs.New(files, "postgres")
	if err != nil {
		return nil, nil, err
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		source.Close()
		return nil, nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		source.Close()
		db.Close()
		return nil, nil, err
	}

	driver, err := postgresmigrate.WithInstance(db, &postgresmigrate.Config{})
	if err != nil {
		source.Close()
		db.Close()
		return nil, nil, err
	}

	instance, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		source.Close()
		db.Close()
		return nil, nil, err
	}

	closeFn := func() {
		instance.Close()
		db.Close()
	}

	return instance, closeFn, nil
}

func migrationsAppliedBetween(before uint, after uint, pending []Migration) []Migration {
	applied := []Migration{}
	for _, migration := range pending {
		version, err := strconv.ParseUint(migration.Version, 10, 64)
		if err != nil {
			continue
		}
		if uint(version) > before && uint(version) <= after {
			applied = append(applied, migration)
		}
	}
	return applied
}
