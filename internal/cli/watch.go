package cli

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirrobot01/mnemo/internal/app/ingestsvc"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/logging"
	"github.com/spf13/cobra"
)

// nearestExisting walks up from dir to the first directory that exists, so
// fsnotify can watch it even before a tool has created the leaf directory.
func nearestExisting(dir string) string {
	p := dir
	for {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

func newWatchCommand(root *string) *cobra.Command {
	var debounceMS int
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Continuously ingest sessions and recompile state of play",
		Long: "watch tails every enabled agent's session directory and, on change,\n" +
			"re-ingests, re-threads, and recompiles affected tasks so `mnemo resume`\n" +
			"is always fresh. For a synchronous one-shot before launching an agent,\n" +
			"use `mnemo ingest && mnemo resume` instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			store, cleanup, err := openLocalStoreWithRegistry(ctx, *root)
			if err != nil {
				return err
			}
			defer cleanup()
			logger := logging.FromContext(ctx).With("repository", store.repo.ID)

			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				return err
			}
			defer watcher.Close()

			watched := map[string]bool{}
			addWatches := func() {
				dirs, err := store.registry.WatchTargets(ctx, store.repo.RootPath)
				if err != nil {
					logger.WarnContext(ctx, "watch-dir resolution failed", "error", err)
					return
				}
				for _, d := range dirs {
					target := nearestExisting(d)
					if watched[target] {
						continue
					}
					if err := watcher.Add(target); err != nil {
						logger.WarnContext(ctx, "could not watch directory", "dir", target, "error", err)
						continue
					}
					watched[target] = true
				}
			}
			addWatches()

			pipeline := func() {
				isvc := ingestsvc.New(store.repo, store.adapter, store.registry)
				results, err := isvc.Import(ctx)
				if err != nil {
					// One adapter failing must not abort the watch loop.
					logger.WarnContext(ctx, "ingest error (continuing)", "error", err)
				}
				// Only speak up when a sweep actually changed something —
				// debounced no-op sweeps stay silent.
				for _, r := range results {
					if r.Imported == 0 && r.RedactedSessions == 0 {
						continue
					}
					logger.InfoContext(ctx, "ingested sessions",
						"agent", r.Agent,
						"kind", r.Kind,
						"imported", r.Imported,
						"unchanged", r.Unchanged,
						"skipped", r.Skipped,
						"redacted_sessions", r.RedactedSessions,
						"redacted_events", r.RedactedEvents,
					)
				}
				tsvc := newTaskSvc(store)
				if _, err := tsvc.Thread(ctx); err != nil {
					logger.WarnContext(ctx, "thread error (continuing)", "error", err)
					return
				}
				if _, err := tsvc.Decay(ctx); err != nil {
					logger.WarnContext(ctx, "decay error (continuing)", "error", err)
				}
				tasks, err := tsvc.List(ctx)
				if err != nil {
					logger.WarnContext(ctx, "task list error (continuing)", "error", err)
					return
				}
				ssvc, err := newStateSvc(store)
				if err != nil {
					logger.WarnContext(ctx, "state compiler setup error (continuing)", "error", err)
					return
				}
				for _, t := range tasks {
					if t.Status == domain.TaskStatusDone {
						continue
					}
					if _, err := ssvc.Compile(ctx, t.ID); err != nil {
						logger.WarnContext(ctx, "compile error (continuing)", "task", t.ID, "error", err)
					}
				}
				// New leaf dirs may now exist (tool just created them).
				addWatches()
			}

			if err := output.FromCommand(cmd).Line("Watching for session changes. Ctrl-C to stop."); err != nil {
				return err
			}
			pipeline() // initial sweep

			debounce := time.Duration(debounceMS) * time.Millisecond
			timer := time.NewTimer(debounce)
			timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case _, ok := <-watcher.Events:
					if !ok {
						return nil
					}
					timer.Reset(debounce)
				case err, ok := <-watcher.Errors:
					if !ok {
						return nil
					}
					logger.WarnContext(ctx, "fsnotify error", "error", err)
				case <-timer.C:
					pipeline()
				}
			}
		},
	}
	cmd.Flags().IntVar(&debounceMS, "debounce-ms", 750, "quiet period after a change before recompiling")
	return cmd
}
