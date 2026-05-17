package cli

import (
	"time"

	"github.com/sirrobot01/mnemo/internal/app/ingestsvc"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/logging"
	"github.com/sirrobot01/mnemo/internal/sessions"
	"github.com/sirrobot01/mnemo/internal/sessions/aider"
	"github.com/sirrobot01/mnemo/internal/sessions/claude"
	"github.com/sirrobot01/mnemo/internal/sessions/codex"
	"github.com/sirrobot01/mnemo/internal/sessions/continueide"
	"github.com/spf13/cobra"
)

// enabledAdapters builds the session adapters Mnemo ingests from. Claude and
// Codex support `mnemo watch` (DirWatcher); Aider and Continue are ingested
// one-shot via `mnemo ingest`.
func enabledAdapters() []sessions.Adapter {
	return []sessions.Adapter{claude.New(""), codex.New(""), aider.New(), continueide.New("")}
}

func newIngestCommand(root *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ingest",
		Short: "Ingest AI coding tool sessions for this repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			store, cleanup, err := openLocalStore(ctx, *root)
			if err != nil {
				return err
			}
			defer cleanup()

			start := time.Now()
			svc := ingestsvc.New(store.repo, store.adapter, enabledAdapters()...)
			results, importErr := svc.Import(ctx)
			logging.FromContext(ctx).InfoContext(ctx, "ingest complete",
				"duration_ms", time.Since(start).Milliseconds(), "adapters", len(results))

			rendered := make([]output.IngestResult, 0, len(results))
			for _, res := range results {
				rendered = append(rendered, output.IngestResult{
					Tool:             res.Tool,
					Discovered:       res.Discovered,
					Imported:         res.Imported,
					Unchanged:        res.Unchanged,
					Skipped:          res.Skipped,
					RedactedEvents:   res.RedactedEvents,
					RedactedSessions: res.RedactedSessions,
				})
			}
			if err := output.FromCommand(cmd).IngestResults(rendered); err != nil {
				return err
			}
			if importErr != nil {
				logging.FromContext(ctx).WarnContext(ctx, "an adapter failed during ingest", "error", importErr)
				return importErr
			}
			return nil
		},
	}
}
