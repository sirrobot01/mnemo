package cli

import (
	"context"

	"github.com/sirrobot01/mnemo/internal/app/tasksvc"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/spf13/cobra"
)

func newTaskService(ctx context.Context, root string) (*tasksvc.Service, domain.Repository, func(), error) {
	store, cleanup, err := openLocalStore(ctx, root)
	if err != nil {
		return nil, domain.Repository{}, nil, err
	}
	return newTaskSvc(store), store.repo, cleanup, nil
}

func taskView(ctx context.Context, svc *tasksvc.Service, t domain.Task) output.TaskView {
	sessions, _ := svc.Sessions(ctx, t.ID)
	return output.TaskView{
		ID:           string(t.ID),
		Title:        t.Title,
		Goal:         t.Goal,
		Status:       string(t.Status),
		Branch:       t.Branch,
		Sessions:     len(sessions),
		LastActiveAt: t.LastActiveAt.Format("2006-01-02 15:04"),
	}
}

func newTaskCommand(root *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks (the unit of cross-agent continuity)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	var goal, branch string
	startCmd := &cobra.Command{
		Use:   "start [title]",
		Short: "Start a new task and make it active",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, cleanup, err := newTaskService(cmd.Context(), *root)
			if err != nil {
				return err
			}
			defer cleanup()
			t, err := svc.Start(cmd.Context(), args[0], goal, branch)
			if err != nil {
				return err
			}
			return output.FromCommand(cmd).Task(taskView(cmd.Context(), svc, t))
		},
	}
	startCmd.Flags().StringVar(&goal, "goal", "", "task goal")
	startCmd.Flags().StringVar(&branch, "branch", "", "git branch this task is scoped to")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, _, cleanup, err := newTaskService(cmd.Context(), *root)
			if err != nil {
				return err
			}
			defer cleanup()
			// Thread first so freshly-ingested sessions show up as tasks,
			// consistent with `status` and `resume`.
			if _, err := svc.Thread(cmd.Context()); err != nil {
				return err
			}
			tasks, err := svc.List(cmd.Context())
			if err != nil {
				return err
			}
			views := make([]output.TaskView, 0, len(tasks))
			for _, t := range tasks {
				views = append(views, taskView(cmd.Context(), svc, t))
			}
			return output.FromCommand(cmd).TaskList(views)
		},
	}

	showCmd := &cobra.Command{
		Use:   "show [task-id]",
		Short: "Show one task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, cleanup, err := newTaskService(cmd.Context(), *root)
			if err != nil {
				return err
			}
			defer cleanup()
			t, err := svc.Get(cmd.Context(), domain.ID(args[0]))
			if err != nil {
				return err
			}
			return output.FromCommand(cmd).Task(taskView(cmd.Context(), svc, t))
		},
	}

	transition := func(use, short string, fn func(*tasksvc.Service, context.Context, domain.ID) (domain.Task, error)) *cobra.Command {
		return &cobra.Command{
			Use:   use + " [task-id]",
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				svc, _, cleanup, err := newTaskService(cmd.Context(), *root)
				if err != nil {
					return err
				}
				defer cleanup()
				t, err := fn(svc, cmd.Context(), domain.ID(args[0]))
				if err != nil {
					return err
				}
				return output.FromCommand(cmd).Task(taskView(cmd.Context(), svc, t))
			},
		}
	}

	cmd.AddCommand(
		startCmd,
		listCmd,
		showCmd,
		transition("switch", "Make a task active", (*tasksvc.Service).Switch),
		transition("pause", "Pause a task", (*tasksvc.Service).Pause),
		transition("done", "Mark a task done", (*tasksvc.Service).Done),
	)
	return cmd
}
