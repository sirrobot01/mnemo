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
	"github.com/sirrobot01/mnemo/internal/app/authsvc"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/logging"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/ui"
	"github.com/spf13/cobra"
)

func newServeCommand(root *string) *cobra.Command {
	var addr string
	var apiOnly bool
	var requireAuth bool
	var allowSignup bool
	var tokenTTL string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local Mnemo server (task timeline UI + API)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			store, cleanup, err := openLocalStoreWithRegistry(ctx, *root)
			if err != nil {
				return err
			}
			defer cleanup()
			logger := logging.FromContext(ctx).With("addr", addr, "repository", store.repo.ID)

			dbStatus := func() (string, int, int, error) {
				cfg := store.cfg
				if cfg.Database.Type != "sqlite" {
					return string(cfg.Database.Type), 0, 0, nil
				}
				st, err := migrations.StatusSQLite(ctx, resolveDSN(cfg.Database.DSN))
				if err != nil {
					return string(cfg.Database.Type), 0, 0, err
				}
				return string(cfg.Database.Type), len(st.Applied), len(st.Pending), nil
			}

			var auth *authsvc.Service
			if requireAuth {
				ttl, _ := time.ParseDuration(tokenTTL)
				auth = authsvc.New(store.adapter, ttl)
				logger.InfoContext(ctx, "server auth enabled; /v1 requires a bearer token")
			}

			apiHandler := api.New(store.repo, store.adapter, store.registry, dbStatus, auth, allowSignup)
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
			if err := output.FromCommand(cmd).Line(fmt.Sprintf("Serving Mnemo on http://%s", addr)); err != nil {
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
	cmd.Flags().BoolVar(&requireAuth, "auth", true, "require browser/API login")
	cmd.Flags().BoolVar(&allowSignup, "allow-signup", true, "allow browser signup when auth is enabled")
	cmd.Flags().StringVar(&tokenTTL, "token-ttl", "720h", "browser/API auth token lifetime")
	return cmd
}
