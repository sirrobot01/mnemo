package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/sirrobot01/mnemo/internal/app/tasksvc"
	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/storage"
	"github.com/sirrobot01/mnemo/internal/storage/sqlite"
)

// localStore holds the per-invocation SQLite adapter and repository record.
type localStore struct {
	adapter *sqlite.Adapter
	repo    domain.Repository
}

// openLocalStore loads config, applies migrations, opens the SQLite adapter,
// and ensures the repository row exists. The local CLI workflow is
// SQLite-only; PostgreSQL is reserved for shared/team mode.
func openLocalStore(ctx context.Context, root string) (localStore, func(), error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return localStore{}, nil, err
	}

	cfg, err := config.Load(config.DefaultPath(root))
	if err != nil {
		return localStore{}, nil, fmt.Errorf("load config: %w", err)
	}
	plan := migrations.PlanFor(cfg.Database.Type)
	if plan.DatabaseType != "sqlite" {
		return localStore{}, nil, fmt.Errorf("local CLI workflow is not implemented for %s yet", plan.DatabaseType)
	}

	dsn := resolveDSN(root, cfg.Database.DSN)
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		return localStore{}, nil, err
	}

	adapter, err := sqlite.Open(ctx, dsn)
	if err != nil {
		return localStore{}, nil, err
	}

	repo, err := ensureRepository(ctx, adapter, root)
	if err != nil {
		adapter.Close()
		return localStore{}, nil, err
	}

	return localStore{adapter: adapter, repo: repo}, func() { _ = adapter.Close() }, nil
}

func ensureRepository(ctx context.Context, adapter *sqlite.Adapter, root string) (domain.Repository, error) {
	repo, err := adapter.GetRepositoryByRootPath(ctx, root)
	if err == nil {
		return repo, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return domain.Repository{}, err
	}

	now := time.Now().UTC()
	repo = domain.Repository{
		ID:        repositoryID(root),
		Name:      filepath.Base(root),
		RootPath:  root,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if repo.Name == "." || repo.Name == string(filepath.Separator) {
		repo.Name = "repository"
	}
	if err := adapter.CreateRepository(ctx, repo); err != nil {
		return domain.Repository{}, err
	}
	return repo, nil
}

func repositoryID(root string) domain.ID {
	return domain.DeterministicID(domain.PrefixRepository, root)
}

// newTaskSvc builds a tasksvc.Service for an opened store, applying the
// repository's configured decay window (config tasks.cold_after); an unset
// or invalid value keeps the tasksvc default.
func newTaskSvc(store localStore) *tasksvc.Service {
	svc := tasksvc.New(store.repo, store.adapter, store.adapter, store.adapter, tasksvc.DefaultIdleWindow)
	if cfg, err := config.Load(config.DefaultPath(store.repo.RootPath)); err == nil {
		svc.SetColdAfter(cfg.ColdAfterDuration())
	}
	return svc
}
