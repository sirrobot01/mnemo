package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirrobot01/mnemo/internal/api"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/logging"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/ui"
	"github.com/spf13/cobra"
)

func newServeCommand(root *string) *cobra.Command {
	var addr string
	var apiOnly bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local Mnemo server (task timeline UI + API)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			store, cleanup, err := openLocalStore(ctx, *root)
			if err != nil {
				return err
			}
			defer cleanup()
			logger := logging.FromContext(ctx).With("addr", addr, "repository", store.repo.ID)

			dbStatus := func() (string, int, int, error) {
				cfg, err := config.Load(config.DefaultPath(store.repo.RootPath))
				if err != nil {
					return "", 0, 0, err
				}
				if cfg.Database.Type != "sqlite" {
					return cfg.Database.Type, 0, 0, nil
				}
				st, err := migrations.StatusSQLite(ctx, resolveDSN(store.repo.RootPath, cfg.Database.DSN))
				if err != nil {
					return cfg.Database.Type, 0, 0, err
				}
				return cfg.Database.Type, len(st.Applied), len(st.Pending), nil
			}

			apiHandler := api.New(store.repo, store.adapter, enabledAdapters(), dbStatus)
			mux := http.NewServeMux()
			mux.Handle("/v1/", logging.Middleware(logger.With("surface", "api"), apiHandler))
			if !apiOnly {
				if uiHandler, err := ui.Handler(); err == nil {
					mux.Handle("/", logging.Middleware(logger.With("surface", "ui"), uiHandler))
				} else {
					logger.WarnContext(ctx, "UI bundle unavailable; serving API only", "error", err)
				}
			}

			server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			logger.InfoContext(ctx, "starting mnemo server", "ui_enabled", !apiOnly)
			if err := output.FromCommand(cmd).Line(fmt.Sprintf("Serving Mnemo on http://%s (API under /v1)", addr)); err != nil {
				return err
			}

			errCh := make(chan error, 1)
			go func() {
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errCh <- err
				}
			}()
			select {
			case <-ctx.Done():
				shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				return server.Shutdown(shutCtx)
			case err := <-errCh:
				return err
			}
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:47321", "HTTP listen address")
	cmd.Flags().BoolVar(&apiOnly, "api-only", false, "serve only the /v1 API")
	return cmd
}
