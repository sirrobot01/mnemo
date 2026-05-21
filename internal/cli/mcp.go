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

	"github.com/sirrobot01/mnemo/internal/app/authsvc"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/logging"
	mnemomcp "github.com/sirrobot01/mnemo/internal/mcp"
	"github.com/spf13/cobra"
)

// mcpTokenTTL is passed to authsvc only for symmetry; the MCP HTTP surface
// exposes no login endpoint, so this affects nothing (Authenticate honours
// each token's own stored expiry). Tokens are minted via `mnemo serve`.
const mcpTokenTTL = 720 * time.Hour

func newMCPCommand(root *string) *cobra.Command {
	var httpAddr string
	var requireAuth bool

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run Mnemo as an MCP server (stdio by default, or Streamable HTTP)",
		Long: "Expose Mnemo's continuity surface to MCP-aware agents.\n" +
			"Tools: mnemo_resume, mnemo_list_tasks, mnemo_task_state, mnemo_ingest.\n" +
			"Task mutation is intentionally not exposed.\n\n" +
			"By default it speaks MCP over stdio — register `mnemo mcp` as a\n" +
			"stdio server in your agent client. Pass --http <addr> to serve the\n" +
			"Streamable HTTP transport instead (the current MCP remote transport;\n" +
			"HTTP+SSE is deprecated). HTTP requests require a bearer token unless\n" +
			"--auth=false; mint a token via `mnemo serve` (shared auth store).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			store, cleanup, err := openLocalStoreWithRegistry(ctx, *root)
			if err != nil {
				return err
			}
			defer cleanup()

			logger := logging.FromContext(ctx).With("repository", store.repo.ID, "surface", "mcp")
			srv, err := mnemomcp.New(store.repo, store.adapter, store.registry, version)
			if err != nil {
				return err
			}

			if httpAddr == "" {
				// stdout is the JSON-RPC channel; logs already go to stderr
				// via the root command's slog wiring, so nothing here may
				// print to stdout.
				logger.InfoContext(ctx, "starting mnemo MCP server (stdio)")
				return srv.Run(ctx)
			}

			var auth *authsvc.Service
			if requireAuth {
				auth = authsvc.New(store.adapter, mcpTokenTTL)
				logger.InfoContext(ctx, "MCP HTTP auth enabled; requests require a bearer token (mint one via `mnemo serve`)")
			} else {
				logger.WarnContext(ctx, "MCP HTTP auth disabled; anyone who can reach this address can read state and trigger ingest", "addr", httpAddr)
			}

			handler := logging.Middleware(logger.With("surface", "mcp-http"), srv.HTTPHandler(auth))
			httpServer := &http.Server{Addr: httpAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}

			logger.InfoContext(ctx, "starting mnemo MCP server (streamable http)", "addr", httpAddr, "auth", requireAuth)
			if err := output.FromCommand(cmd).Line(fmt.Sprintf("Serving Mnemo MCP (streamable http) on http://%s", httpAddr)); err != nil {
				return err
			}

			errCh := make(chan error, 1)
			go func() {
				if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errCh <- err
				}
			}()
			select {
			case <-ctx.Done():
				shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				return httpServer.Shutdown(shutCtx)
			case err := <-errCh:
				return err
			}
		},
	}
	cmd.Flags().StringVar(&httpAddr, "http", "", "serve Streamable HTTP on this address instead of stdio (e.g. 127.0.0.1:47422); empty = stdio")
	cmd.Flags().BoolVar(&requireAuth, "auth", true, "require a bearer token when serving over HTTP")
	return cmd
}
