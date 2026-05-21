package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sirrobot01/mnemo/internal/app/enrich"
	"github.com/sirrobot01/mnemo/internal/app/statesvc"
	"github.com/sirrobot01/mnemo/internal/app/tasksvc"
	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	reporoot "github.com/sirrobot01/mnemo/internal/repo"
	"github.com/sirrobot01/mnemo/internal/sessions"
	"github.com/sirrobot01/mnemo/internal/storage"
	"github.com/sirrobot01/mnemo/internal/storage/sqlite"
)

type localProject struct {
	info reporoot.Info
	cfg  config.Config
	dsn  string
}

type localStoreOptions struct {
	withRegistry bool
}

// localStore holds the per-invocation SQLite adapter and repository record.
// The agent registry is populated only for commands that explicitly need
// session discovery.
type localStore struct {
	adapter  *sqlite.Adapter
	repo     domain.Repository
	cfg      config.Config
	registry *sessions.Registry
}

func resolveLocalProject(start string) (localProject, error) {
	info, err := reporoot.Resolve(start)
	if err != nil {
		return localProject{}, err
	}
	cfg, err := config.LoadLayered(info.Root)
	if err != nil {
		return localProject{}, fmt.Errorf("load config: %w", err)
	}
	return localProject{info: info, cfg: cfg, dsn: resolveDSN(cfg.Database.DSN)}, nil
}

// openLocalStore resolves the project, opens the configured SQLite adapter,
// verifies migration metadata from that adapter, and ensures the repository row
// exists. It intentionally does not build the agent registry by default.
func openLocalStore(ctx context.Context, start string) (localStore, func(), error) {
	project, err := resolveLocalProject(start)
	if err != nil {
		return localStore{}, nil, err
	}
	return openResolvedLocalStore(ctx, project, localStoreOptions{})
}

func openLocalStoreWithRegistry(ctx context.Context, start string) (localStore, func(), error) {
	project, err := resolveLocalProject(start)
	if err != nil {
		return localStore{}, nil, err
	}
	return openResolvedLocalStore(ctx, project, localStoreOptions{withRegistry: true})
}

func openResolvedLocalStore(ctx context.Context, project localProject, opts localStoreOptions) (localStore, func(), error) {
	plan := migrations.PlanFor(string(project.cfg.Database.Type))
	if plan.DatabaseType != "sqlite" {
		return localStore{}, nil, fmt.Errorf("local CLI workflow is not implemented for %s yet", plan.DatabaseType)
	}
	if err := ensureLocalDatabaseExists(project.dsn); err != nil {
		return localStore{}, nil, err
	}

	adapter, err := sqlite.Open(ctx, project.dsn)
	if err != nil {
		return localStore{}, nil, err
	}
	if err := ensureLocalSchemaCurrent(ctx, adapter); err != nil {
		adapter.Close()
		return localStore{}, nil, err
	}

	repository, err := ensureRepository(ctx, adapter, project.info)
	if err != nil {
		adapter.Close()
		return localStore{}, nil, err
	}

	var registry *sessions.Registry
	if opts.withRegistry {
		registry, err = buildRegistry(project.cfg)
		if err != nil {
			adapter.Close()
			return localStore{}, nil, fmt.Errorf("build agent registry: %w", err)
		}
	}

	return localStore{adapter: adapter, repo: repository, cfg: project.cfg, registry: registry}, func() { _ = adapter.Close() }, nil
}

func migrateLocalStore(ctx context.Context, cfg config.Config) error {
	plan := migrations.PlanFor(string(cfg.Database.Type))
	if plan.DatabaseType != "sqlite" {
		return fmt.Errorf("local CLI workflow is not implemented for %s yet", plan.DatabaseType)
	}
	_, err := migrations.ApplySQLite(ctx, resolveDSN(cfg.Database.DSN))
	return err
}

func ensureLocalDatabaseExists(dsn string) error {
	if dsn == ":memory:" {
		return nil
	}
	if _, err := os.Stat(dsn); errors.Is(err, os.ErrNotExist) {
		pending, pendingErr := migrations.PendingCount("sqlite", 0, false)
		if pendingErr != nil {
			return pendingErr
		}
		return fmt.Errorf("database has %d pending migration(s); run `mnemo db migrate`", pending)
	} else if err != nil {
		return err
	}
	return nil
}

func ensureLocalSchemaCurrent(ctx context.Context, adapter *sqlite.Adapter) error {
	state, err := adapter.MigrationState(ctx)
	if err != nil {
		return fmt.Errorf("check database migrations: %w", err)
	}
	if state.Dirty {
		return fmt.Errorf("database migration is dirty at version %d; repair it before continuing", state.Version)
	}
	pending, err := migrations.PendingCount("sqlite", state.Version, state.Exists)
	if err != nil {
		return err
	}
	if pending > 0 {
		return fmt.Errorf("database has %d pending migration(s); run `mnemo db migrate`", pending)
	}
	return nil
}

// ensureRepository looks the project up by its stable identity (git remote or
// git-root path), so invoking mnemo from any subdirectory of a repo always
// resolves to the same rows in the single DB.
func ensureRepository(ctx context.Context, adapter *sqlite.Adapter, info reporoot.Info) (domain.Repository, error) {
	id := repositoryID(info.Identity)
	if existing, err := adapter.GetRepository(ctx, id); err == nil {
		return existing, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return domain.Repository{}, err
	}

	now := time.Now().UTC()
	repository := domain.Repository{
		ID:        id,
		Name:      filepath.Base(info.Root),
		RootPath:  info.Root,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if repository.Name == "." || repository.Name == string(filepath.Separator) {
		repository.Name = "repository"
	}
	if err := adapter.CreateRepository(ctx, repository); err != nil {
		return domain.Repository{}, err
	}
	return repository, nil
}

func repositoryID(identity string) domain.ID {
	return domain.DeterministicID(domain.PrefixRepository, identity)
}

// newTaskSvc builds a tasksvc.Service for an opened store, applying the
// repository's configured decay window (config tasks.cold_after); an unset
// or invalid value keeps the tasksvc default.
func newTaskSvc(store localStore) *tasksvc.Service {
	svc := tasksvc.New(store.repo, store.adapter, store.adapter, store.adapter, tasksvc.DefaultIdleWindow)
	svc.SetColdAfter(store.cfg.ColdAfterDuration())
	return svc
}

func newStateSvc(store localStore) (*statesvc.Service, error) {
	svc := statesvc.New(store.adapter, store.adapter, store.adapter)
	enricher, err := enrich.New(store.cfg.Enrichment)
	if err != nil {
		return nil, err
	}
	if enricher != nil {
		svc.SetEnricher(enricher)
	}
	return svc, nil
}
