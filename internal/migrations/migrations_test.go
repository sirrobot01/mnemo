package migrations

import (
	"context"
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

	if len(migrations) == 0 {
		t.Fatal("expected at least one migration")
	}

	if migrations[0].Version != "001" {
		t.Fatalf("expected first migration version 001, got %q", migrations[0].Version)
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
