package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/app/context"
	"github.com/sirrobot01/mnemo/internal/app/resumesvc"
	"github.com/sirrobot01/mnemo/internal/app/tasksvc"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/logging"
	"github.com/sirrobot01/mnemo/internal/storage"
	"github.com/spf13/cobra"
)

type resumeAgentSpec struct {
	Command string
	Args    func(prompt string) []string
}

var resumeAgentSpecs = map[string]resumeAgentSpec{
	"aider": {
		Command: "aider",
		Args:    func(prompt string) []string { return []string{"--message", prompt} },
	},
	"claude": {
		Command: "claude",
		Args:    func(prompt string) []string { return []string{prompt} },
	},
	"codex": {
		Command: "codex",
		Args:    func(prompt string) []string { return []string{prompt} },
	},
	"continue": {
		Command: "cn",
		Args:    func(prompt string) []string { return []string{"-p", prompt} },
	},
	"copilot": {
		Command: "copilot",
		Args:    func(prompt string) []string { return []string{"-i", prompt} },
	},
	"cursor": {
		Command: "cursor-agent",
		Args:    func(prompt string) []string { return []string{prompt} },
	},
	"windsurf": {
		Command: "devin",
		Args:    func(prompt string) []string { return []string{"--", prompt} },
	},
}

var launchResumeAgent = runResumeAgent

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

func sourceKinds(ctx context.Context, store localStore, task domain.Task, tsvc *tasksvc.Service) []domain.SessionKind {
	ids, _ := tsvc.Sessions(ctx, task.ID)
	seen := map[domain.SessionKind]bool{}
	kinds := []domain.SessionKind{}
	for _, id := range ids {
		s, err := store.adapter.GetSession(ctx, id)
		if err != nil {
			continue
		}
		if !seen[s.Kind] {
			seen[s.Kind] = true
			kinds = append(kinds, s.Kind)
		}
	}
	return kinds
}

func newResumeCommand(root *string) *cobra.Command {
	var tool, taskID string
	var write, printOnly, allowCrossVendor bool

	cmd := &cobra.Command{
		Use:   "resume [agent]",
		Short: "Compile state of play, or launch an agent with it",
		Long: "Compile the active task's state of play. With no agent, print it.\n" +
			"With an agent, launch that CLI with the resume handoff.\n\n" +
			"Examples:\n" +
			"  mnemo resume\n" +
			"  mnemo resume claude\n" +
			"  mnemo resume codex\n" +
			"  mnemo resume aider\n" +
			"  mnemo resume continue\n" +
			"  mnemo resume copilot\n" +
			"  mnemo resume cursor\n" +
			"  mnemo resume windsurf",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			legacyToolOnly := len(args) == 0 && strings.TrimSpace(tool) != ""
			agent, explicitAgent, err := resumeAgentFromArgs(args, tool)
			if err != nil {
				return err
			}

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
			ssvc, err := newStateSvc(store)
			if err != nil {
				return err
			}
			ws, err := ssvc.Compile(ctx, task.ID)
			if err != nil {
				return err
			}
			logging.FromContext(ctx).InfoContext(ctx, "resume compiled",
				"task", task.ID, "version", ws.Version,
				"compile_ms", time.Since(compileStart).Milliseconds())

			allowed := allowCrossVendor || store.cfg.Privacy.AllowCrossVendorEgress
			// A positional agent is an explicit handoff request: the user is
			// asking Mnemo to inject this state into that agent now.
			allowed = allowed || explicitAgent

			contextText := ""
			if len(store.cfg.Contexts) > 0 {
				csvc := contextsvc.New(store.repo.RootPath, store.cfg.Contexts, store.cfg.Privacy.AllowContextURLEgress)
				contextText, err = csvc.Render(ctx, store.cfg.Contexts)
				if err != nil {
					return err
				}
			}

			rendered, err := resumesvc.Render(ws, sourceKinds(ctx, store, task, tsvc), resumesvc.Options{
				Tool:               agent,
				CrossVendorAllowed: allowed,
				Context:            contextText,
			})
			if err != nil {
				return err
			}

			if write {
				name := "resume.md"
				if agent != "" {
					name = "resume-" + agent + ".md"
				}
				dest := filepath.Join(store.repo.RootPath, ".mnemo", name)
				if err := os.WriteFile(dest, []byte(rendered.Content), 0o644); err != nil {
					return err
				}
				return output.FromCommand(cmd).Line(fmt.Sprintf("Wrote %s", dest))
			}
			if agent != "" && !printOnly && !legacyToolOnly {
				return launchResumeAgent(ctx, cmd, store.repo.RootPath, agent, rendered.Content)
			}
			return output.FromCommand(cmd).Text(rendered.Content)
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "", "target agent; deprecated, use `mnemo resume <agent>`")
	cmd.Flags().StringVar(&taskID, "task", "", "task id (default: most-recently-active task)")
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the rendered handoff instead of launching the agent")
	cmd.Flags().BoolVar(&write, "write", false, "write a managed block to .mnemo/ instead of stdout")
	cmd.Flags().BoolVar(&allowCrossVendor, "allow-cross-vendor", false, "permit injecting into a different vendor's agent")
	_ = cmd.Flags().MarkHidden("tool")
	_ = cmd.Flags().MarkHidden("allow-cross-vendor")
	return cmd
}

func resumeAgentFromArgs(args []string, legacyTool string) (agent string, explicit bool, err error) {
	if len(args) > 0 && strings.TrimSpace(legacyTool) != "" {
		return "", false, fmt.Errorf("use either `mnemo resume <agent>` or --tool, not both")
	}
	if len(args) > 0 {
		agent = args[0]
		explicit = true
	} else {
		agent = legacyTool
	}
	agent = strings.ToLower(strings.TrimSpace(agent))
	switch agent {
	case "", "stdout", "generic":
		return "", explicit, nil
	case "claude-code":
		return "claude", explicit, nil
	case "openai-codex":
		return "codex", explicit, nil
	case "aider-chat":
		return "aider", explicit, nil
	case "cn", "continue-cli":
		return "continue", explicit, nil
	case "github-copilot":
		return "copilot", explicit, nil
	case "cursor-agent":
		return "cursor", explicit, nil
	case "devin", "windsurf-devin":
		return "windsurf", explicit, nil
	}
	if _, ok := resumeAgentSpecs[agent]; !ok {
		return "", false, fmt.Errorf("unsupported resume agent %q (supported: aider, claude, codex, continue, copilot, cursor, windsurf)", agent)
	}
	return agent, explicit, nil
}

func runResumeAgent(ctx context.Context, cobraCmd *cobra.Command, root string, agent string, prompt string) error {
	spec, ok := resumeAgentSpecs[agent]
	if !ok {
		return fmt.Errorf("unsupported resume agent %q", agent)
	}
	if _, err := exec.LookPath(spec.Command); err != nil {
		return fmt.Errorf("%s CLI not found in PATH", spec.Command)
	}
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args(prompt)...)
	cmd.Dir = root
	cmd.Stdin = cobraCmd.InOrStdin()
	cmd.Stdout = cobraCmd.OutOrStdout()
	cmd.Stderr = cobraCmd.ErrOrStderr()
	return cmd.Run()
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
