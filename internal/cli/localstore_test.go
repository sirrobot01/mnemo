package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sirrobot01/mnemo/internal/config"
)

func TestOpenLocalStoreRequiresCurrentSchema(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	ctx := context.Background()
	repoRoot := t.TempDir()

	store, cleanup, err := openLocalStore(ctx, repoRoot)
	if err == nil {
		cleanup()
		t.Fatal("expected openLocalStore to reject an unmigrated database")
	}
	if !strings.Contains(err.Error(), "pending migration") {
		t.Fatalf("expected pending migration guidance, got: %v", err)
	}
	if _, statErr := os.Stat(config.GlobalDBPath()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("openLocalStore should not create an unmigrated database, stat err = %v", statErr)
	}

	cfg, err := config.LoadLayered(repoRoot)
	if err != nil {
		t.Fatalf("LoadLayered: %v", err)
	}
	if err := migrateLocalStore(ctx, cfg); err != nil {
		t.Fatalf("migrateLocalStore: %v", err)
	}

	store, cleanup, err = openLocalStore(ctx, repoRoot)
	if err != nil {
		t.Fatalf("openLocalStore after migration: %v", err)
	}
	defer cleanup()
	if store.repo.RootPath == "" {
		t.Fatal("expected repository row to be ensured")
	}
	if store.registry != nil {
		t.Fatal("openLocalStore should not build the agent registry by default")
	}

	store, cleanup, err = openLocalStoreWithRegistry(ctx, repoRoot)
	if err != nil {
		t.Fatalf("openLocalStoreWithRegistry: %v", err)
	}
	defer cleanup()
	if store.registry == nil {
		t.Fatal("expected registry for session-discovery store")
	}
}
