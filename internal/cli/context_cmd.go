package cli

import (
	"fmt"
	"strings"

	"github.com/sirrobot01/mnemo/internal/app/context"
	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/config"
	reporoot "github.com/sirrobot01/mnemo/internal/repo"
	"github.com/spf13/cobra"
)

func newContextCommand(root *string) *cobra.Command {
	contextCmd := &cobra.Command{
		Use:   "context",
		Short: "Manage read-only knowledge contexts for the handoff",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return output.FromCommand(cmd).Line("mnemo context requires a subcommand: list, add, remove, or show")
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured contexts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, err := reporoot.Resolve(*root)
			if err != nil {
				return err
			}
			cfg, err := config.LoadLayered(info.Root)
			if err != nil {
				return err
			}
			out := output.FromCommand(cmd)
			for _, c := range cfg.Contexts {
				target := c.Path
				if c.Type == "url" {
					target = c.URL
				} else if c.Type == "context" {
					target = "→ " + c.Ref
				}
				if err := out.Line(fmt.Sprintf("%s\t%s\t%s", c.Name, c.Type, target)); err != nil {
					return err
				}
			}
			return nil
		},
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Resolve and print the scrubbed context block",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, err := reporoot.Resolve(*root)
			if err != nil {
				return err
			}
			cfg, err := config.LoadLayered(info.Root)
			if err != nil {
				return err
			}
			csvc := contextsvc.New(info.Root, cfg.Contexts, cfg.Privacy.AllowContextURLEgress)
			text, err := csvc.Render(cmd.Context(), cfg.Contexts)
			if err != nil {
				return err
			}
			return output.FromCommand(cmd).Line(text)
		},
	}

	var addType, addPath, addRef, addURL string
	addCmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a context to the project config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			info, err := reporoot.Resolve(*root)
			if err != nil {
				return err
			}
			cfg, err := config.LoadProject(info.Root)
			if err != nil {
				return err
			}
			for _, c := range cfg.Contexts {
				if c.Name == name {
					return fmt.Errorf("context %q already exists", name)
				}
			}
			contextType := config.ContextType(strings.TrimSpace(addType))
			switch contextType {
			case config.ContextFile, config.ContextDir:
				if addPath == "" {
					return fmt.Errorf("--path is required for a %s context", addType)
				}
			case config.ContextURL:
				if addURL == "" {
					return fmt.Errorf("--url is required for a url context")
				}
			case config.ContextReference:
				if addRef == "" {
					return fmt.Errorf("--ref is required for a context reference")
				}
			default:
				return fmt.Errorf("--type must be file, dir, url, or context")
			}
			cfg.Contexts = append(cfg.Contexts, config.ContextConfig{
				Name: name, Type: contextType, Path: addPath, Ref: addRef, URL: addURL,
			})
			if err := config.RewriteProject(config.DefaultPath(info.Root), cfg); err != nil {
				return err
			}
			return output.FromCommand(cmd).Line(fmt.Sprintf("added context %q (%s)", name, addType))
		},
	}
	addCmd.Flags().StringVar(&addType, "type", "", "file | dir | url | context")
	addCmd.Flags().StringVar(&addPath, "path", "", "path for file/dir contexts (repo-relative or absolute)")
	addCmd.Flags().StringVar(&addRef, "ref", "", "referenced context name for type=context")
	addCmd.Flags().StringVar(&addURL, "url", "", "URL for type=url (fetched only if privacy.allow_context_url_egress)")

	removeCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a context from the project config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			info, err := reporoot.Resolve(*root)
			if err != nil {
				return err
			}
			cfg, err := config.LoadProject(info.Root)
			if err != nil {
				return err
			}
			kept := make([]config.ContextConfig, 0, len(cfg.Contexts))
			found := false
			for _, c := range cfg.Contexts {
				if c.Name == name {
					found = true
					continue
				}
				kept = append(kept, c)
			}
			if !found {
				return fmt.Errorf("context %q not found", name)
			}
			cfg.Contexts = kept
			if err := config.RewriteProject(config.DefaultPath(info.Root), cfg); err != nil {
				return err
			}
			return output.FromCommand(cmd).Line(fmt.Sprintf("removed context %q", name))
		},
	}

	contextCmd.AddCommand(listCmd, showCmd, addCmd, removeCmd)
	return contextCmd
}
