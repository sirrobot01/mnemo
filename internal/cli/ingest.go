package cli

import (
	"time"

	"github.com/sirrobot01/mnemo/internal/app/ingestsvc"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/logging"
	"github.com/spf13/cobra"
)

func newIngestCommand(root *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ingest",
		Short: "Ingest AI coding tool sessions for this repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			store, cleanup, err := openLocalStoreWithRegistry(ctx, *root)
			if err != nil {
				return err
			}
			defer cleanup()

			start := time.Now()
			svc := ingestsvc.New(store.repo, store.adapter, store.registry)
			results, importErr := svc.Import(ctx)
			logging.FromContext(ctx).InfoContext(ctx, "ingest complete",
				"duration_ms", time.Since(start).Milliseconds(), "agents", len(results))

			rendered := make([]output.IngestResult, 0, len(results))
			for _, res := range results {
				rendered = append(rendered, output.IngestResult{
					Agent:            res.Agent,
					Kind:             res.Kind,
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
