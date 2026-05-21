package migrations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanForSQLite(t *testing.T) {
	plan := PlanFor("sqlite")

	if plan.DatabaseType != "sqlite" {
		t.Fatalf("expected sqlite plan, got %q", plan.DatabaseType)
	}

	if plan.Strategy != StrategyEmbeddedSQL {
		t.Fatalf("unexpected strategy: %q", plan.Strategy)
	}
}

func TestPlanForUnknownDefaultsToSQLite(t *testing.T) {
	plan := PlanFor("unknown")

	if plan.DatabaseType != "sqlite" {
		t.Fatalf("expected sqlite default, got %q", plan.DatabaseType)
	}
}

func TestListSQLiteMigrations(t *testing.T) {
	migrations, err := List("sqlite")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	if len(migrations) != 1 {
		t.Fatalf("expected one squashed migration, got %d", len(migrations))
	}

	if migrations[0].Version != "001" || migrations[0].Name != "init" {
		t.Fatalf("expected 001_init migration, got %s_%s", migrations[0].Version, migrations[0].Name)
	}
}

func TestListPostgresMigrations(t *testing.T) {
	migrations, err := List("postgres")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	if len(migrations) != 1 {
		t.Fatalf("expected one squashed migration, got %d", len(migrations))
	}

	if migrations[0].Version != "001" || migrations[0].Name != "init" {
		t.Fatalf("expected 001_init migration, got %s_%s", migrations[0].Version, migrations[0].Name)
	}
}

func TestApplySQLite(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "mnemo.db")

	result, err := ApplySQLite(context.Background(), dsn)
	if err != nil {
		t.Fatalf("ApplySQLite returned error: %v", err)
	}

	if len(result.Applied) == 0 {
		t.Fatal("expected migrations to apply")
	}

	status, err := StatusSQLite(context.Background(), dsn)
	if err != nil {
		t.Fatalf("StatusSQLite returned error: %v", err)
	}

	if len(status.Pending) != 0 {
		t.Fatalf("expected no pending migrations, got %d", len(status.Pending))
	}

	result, err = ApplySQLite(context.Background(), dsn)
	if err != nil {
		t.Fatalf("second ApplySQLite returned error: %v", err)
	}

	if len(result.Applied) != 0 {
		t.Fatalf("expected no migrations on second apply, got %d", len(result.Applied))
	}
}

func TestStatusSQLiteMissingDatabaseDoesNotCreateFile(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "missing", "mnemo.db")

	status, err := StatusSQLite(context.Background(), dsn)
	if err != nil {
		t.Fatalf("StatusSQLite returned error: %v", err)
	}
	if len(status.Pending) == 0 {
		t.Fatal("expected missing database to report pending migrations")
	}
	if _, err := os.Stat(dsn); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("StatusSQLite should not create missing database, stat err = %v", err)
	}
}
