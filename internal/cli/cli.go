package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/logging"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/spf13/cobra"
)

// Execute is the production entrypoint for the mnemo binary. It builds the
// root command, runs it against context.Background(), and exits non-zero on
// failure. Errors are printed to the command's bound stderr (defaults to
// os.Stderr) so we don't duplicate Cobra's responsibility.
//
// Tests should NOT call Execute (it terminates the process). Use
// NewRootCommand to construct a fresh command tree and drive it with
// cmd.SetOut / cmd.SetErr / cmd.SetArgs / cmd.ExecuteContext.
func Execute() {
	cmd, err := NewRootCommand()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err)
		os.Exit(1)
	}
}

// NewRootCommand builds the Mnemo command tree. It is exported so tests can
// construct a fresh command per case and drive it via the standard Cobra
// surface (SetOut, SetErr, SetArgs, ExecuteContext).
//
// The returned command does not call SetOut/SetErr. Cobra's defaults
// (os.Stdout / os.Stderr) apply unless the caller overrides them.
func NewRootCommand() (*cobra.Command, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	logLevel := logging.DefaultLevel
	logFormat := logging.DefaultFormat
	outputFormat := string(output.FormatHuman)

	rootCmd := &cobra.Command{
		Use:           "mnemo",
		Short:         "cross-agent memory for AI coding — switch tools without losing your place",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       "dev",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			parsedOutputFormat, err := output.ParseFormat(outputFormat)
			if err != nil {
				return err
			}
			// Route slog through whichever writer Cobra has bound for
			// stderr. Production uses os.Stderr; tests override via
			// cmd.SetErr(buf) and the logger respects it automatically.
			logger, err := logging.New(cmd.ErrOrStderr(), logLevel, logFormat)
			if err != nil {
				return err
			}
			logger = logger.With("command", cmd.CommandPath())
			// slog.SetDefault is intentional: it ensures any third-party
			// code that calls slog.Info / slog.Error directly (without
			// going through our context) still respects the user's
			// --log-level and --log-format choices. Mnemo is a single-
			// process CLI, so the global mutation is harmless. Parallel
			// in-process tests should construct their own loggers and
			// avoid relying on slog.Default.
			slog.SetDefault(logger)
			ctx := output.WithFormat(cmd.Context(), parsedOutputFormat)
			cmd.SetContext(logging.WithLogger(ctx, logger))
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	rootCmd.SetVersionTemplate("mnemo {{.Version}}\n")
	rootCmd.PersistentFlags().StringVar(&root, "root", root, "repository root")
	rootCmd.PersistentFlags().StringVar(&outputFormat, "format", outputFormat, "output format: human or json")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", logLevel, "log level: debug, info, warn, or error")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", logFormat, "log format: text or json")

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Mnemo in a repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, root)
		},
	}

	dbCmd := &cobra.Command{
		Use:   "db",
		Short: "Database management commands",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return output.FromCommand(cmd).Line("mnemo db requires a subcommand: migrate, status, or reset")
		},
	}

	dbMigrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDBMigrate(cmd, root)
		},
	}

	dbStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show database migration status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDBStatus(cmd, root)
		},
	}

	dbResetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset database state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return output.FromCommand(cmd).Line("mnemo db reset is not implemented yet")
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return output.FromCommand(cmd).Line("mnemo dev")
		},
	}

	dbCmd.AddCommand(dbMigrateCmd, dbStatusCmd, dbResetCmd)
	rootCmd.AddCommand(
		initCmd,
		newIngestCommand(&root),
		newWatchCommand(&root),
		newTaskCommand(&root),
		newResumeCommand(&root),
		newStatusCommand(&root),
		newForgetCommand(&root),
		newServeCommand(&root),
		dbCmd,
		versionCmd,
	)

	return rootCmd, nil
}

func runInit(cmd *cobra.Command, root string) error {
	ctx := cmd.Context()
	logger := logging.FromContext(ctx)
	path := config.DefaultPath(root)
	if err := config.Save(path, config.Default()); err != nil {
		return err
	}
	_, cleanup, err := openLocalStore(ctx, root)
	if err != nil {
		return err
	}
	defer cleanup()

	logger.InfoContext(ctx, "initialized repository", "root", root, "config_path", path)
	return output.FromCommand(cmd).Initialized(path)
}

func runDBMigrate(cmd *cobra.Command, root string) error {
	ctx := cmd.Context()
	logger := logging.FromContext(ctx)
	cfg, err := config.Load(config.DefaultPath(root))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	plan := migrations.PlanFor(cfg.Database.Type)
	logger.InfoContext(ctx, "running database migrations", "database", plan.DatabaseType)
	var result migrations.ApplyResult
	switch plan.DatabaseType {
	case "sqlite":
		result, err = migrations.ApplySQLite(ctx, resolveDSN(root, cfg.Database.DSN))
	case "postgres":
		result, err = migrations.ApplyPostgres(ctx, cfg.Database.DSN)
	default:
		err = fmt.Errorf("db migrate is not implemented for %s yet", plan.DatabaseType)
	}
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "database migrations completed", "database", plan.DatabaseType, "applied", len(result.Applied), "skipped", len(result.Skipped))
	return output.FromCommand(cmd).MigrationResult(plan.Description, len(result.Applied), len(result.Skipped))
}

func runDBStatus(cmd *cobra.Command, root string) error {
	ctx := cmd.Context()
	logger := logging.FromContext(ctx)
	cfg, err := config.Load(config.DefaultPath(root))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	plan := migrations.PlanFor(cfg.Database.Type)
	logger.DebugContext(ctx, "checking database status", "database", plan.DatabaseType)
	applied := 0
	pending := 0
	switch plan.DatabaseType {
	case "sqlite":
		status, err := migrations.StatusSQLite(ctx, resolveDSN(root, cfg.Database.DSN))
		if err != nil {
			return err
		}
		applied = len(status.Applied)
		pending = len(status.Pending)
	case "postgres":
		status, err := migrations.StatusPostgres(ctx, cfg.Database.DSN)
		if err != nil {
			return err
		}
		applied = len(status.Applied)
		pending = len(status.Pending)
	}
	return output.FromCommand(cmd).MigrationStatus(cfg.Database.Type, cfg.Database.DSN, plan.Description, applied, pending)
}

func resolveDSN(root string, dsn string) string {
	if filepath.IsAbs(dsn) {
		return dsn
	}
	return filepath.Join(root, dsn)
}
