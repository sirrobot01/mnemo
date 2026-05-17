package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sirrobot01/mnemo/internal/app/resumesvc"
	"github.com/sirrobot01/mnemo/internal/app/statesvc"
	"github.com/sirrobot01/mnemo/internal/app/tasksvc"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/logging"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/storage"
	"github.com/spf13/cobra"
)

// pickTask resolves the task to act on: an explicit --task id, otherwise the
// most-recently-active task after threading any freshly ingested sessions.
func pickTask(ctx context.Context, svc *tasksvc.Service, explicit string) (domain.Task, error) {
	if _, err := svc.Thread(ctx); err != nil {
		return domain.Task{}, err
	}
	if explicit != "" {
		return svc.Get(ctx, domain.ID(explicit))
	}
	task, ok, err := svc.Active(ctx)
	if err != nil {
		return domain.Task{}, err
	}
	if !ok {
		return domain.Task{}, tasksvc.ErrNoActiveTask
	}
	return task, nil
}

func sourceTools(ctx context.Context, store localStore, task domain.Task, tsvc *tasksvc.Service) []domain.SessionTool {
	ids, _ := tsvc.Sessions(ctx, task.ID)
	seen := map[domain.SessionTool]bool{}
	tools := []domain.SessionTool{}
	for _, id := range ids {
		s, err := store.adapter.GetSession(ctx, id)
		if err != nil {
			continue
		}
		if !seen[s.Tool] {
			seen[s.Tool] = true
			tools = append(tools, s.Tool)
		}
	}
	return tools
}

func newResumeCommand(root *string) *cobra.Command {
	var tool, taskID string
	var write, allowCrossVendor bool

	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Compile and emit the state of play for the next agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			store, cleanup, err := openLocalStore(ctx, *root)
			if err != nil {
				return err
			}
			defer cleanup()

			tsvc := newTaskSvc(store)
			task, err := pickTask(ctx, tsvc, taskID)
			if err != nil {
				return err
			}

			compileStart := time.Now()
			ssvc := statesvc.New(store.adapter, store.adapter, store.adapter)
			ws, err := ssvc.Compile(ctx, task.ID)
			if err != nil {
				return err
			}
			logging.FromContext(ctx).InfoContext(ctx, "resume compiled",
				"task", task.ID, "version", ws.Version,
				"compile_ms", time.Since(compileStart).Milliseconds())

			allowed := allowCrossVendor
			if cfg, err := config.Load(config.DefaultPath(store.repo.RootPath)); err == nil {
				allowed = allowed || cfg.Privacy.AllowCrossVendorEgress
			}

			rendered, err := resumesvc.Render(ws, sourceTools(ctx, store, task, tsvc), resumesvc.Options{
				Tool:               tool,
				CrossVendorAllowed: allowed,
			})
			if err != nil {
				return err
			}

			if write {
				name := "resume.md"
				if tool != "" {
					name = "resume-" + tool + ".md"
				}
				dest := filepath.Join(store.repo.RootPath, ".mnemo", name)
				if err := os.WriteFile(dest, []byte(rendered.Content), 0o644); err != nil {
					return err
				}
				return output.FromCommand(cmd).Line(fmt.Sprintf("Wrote %s", dest))
			}
			return output.FromCommand(cmd).Text(rendered.Content)
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "", "target agent (claude, codex, …); empty prints locally")
	cmd.Flags().StringVar(&taskID, "task", "", "task id (default: most-recently-active task)")
	cmd.Flags().BoolVar(&write, "write", false, "write a managed block to .mnemo/ instead of stdout")
	cmd.Flags().BoolVar(&allowCrossVendor, "allow-cross-vendor", false, "permit injecting into a different vendor's agent")
	return cmd
}

func newStatusCommand(root *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the active task and its current state of play",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			store, cleanup, err := openLocalStore(ctx, *root)
			if err != nil {
				return err
			}
			defer cleanup()

			tsvc := newTaskSvc(store)
			if _, err := tsvc.Thread(ctx); err != nil {
				return err
			}
			task, ok, err := tsvc.Active(ctx)
			if err != nil {
				return err
			}
			if !ok {
				return output.FromCommand(cmd).Status(output.StatusView{})
			}
			ids, _ := tsvc.Sessions(ctx, task.ID)
			view := output.StatusView{
				ActiveTask: &output.TaskView{
					ID:       string(task.ID),
					Title:    task.Title,
					Status:   string(task.Status),
					Sessions: len(ids),
				},
				Goal: task.Goal,
			}
			if ws, err := store.adapter.GetLatestWorkingState(ctx, task.ID); err == nil {
				view.WorkingVersion = ws.Version
				if view.Goal == "" {
					view.Goal = ws.Goal
				}
			}
			return output.FromCommand(cmd).Status(view)
		},
	}
}

func newForgetCommand(root *string) *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "forget [session-id]",
		Short: "Completely delete a session (and its events) or a task",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			store, cleanup, err := openLocalStore(ctx, *root)
			if err != nil {
				return err
			}
			defer cleanup()

			if taskID != "" {
				tsvc := newTaskSvc(store)
				if err := tsvc.ForgetTask(ctx, domain.ID(taskID)); err != nil {
					return err
				}
				return output.FromCommand(cmd).Line("Forgot task " + taskID)
			}
			if len(args) != 1 {
				return fmt.Errorf("provide a session-id argument or --task <id>")
			}
			sid := domain.ID(args[0])

			// Detach the session from any task before deleting it.
			tasks, err := store.adapter.ListTasks(ctx, storage.TaskFilter{RepoID: store.repo.ID})
			if err != nil {
				return err
			}
			for _, t := range tasks {
				ids, err := store.adapter.ListTaskSessions(ctx, t.ID)
				if err != nil {
					return err
				}
				for _, id := range ids {
					if id == sid {
						if err := store.adapter.DetachSession(ctx, t.ID, sid); err != nil {
							return err
						}
					}
				}
			}
			if err := store.adapter.DeleteSession(ctx, sid); err != nil {
				return err
			}
			return output.FromCommand(cmd).Line("Forgot session " + string(sid))
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "forget a task (its working states + session links) instead of a session")
	return cmd
}
