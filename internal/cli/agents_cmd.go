package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirrobot01/mnemo/internal/cli/output"
	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/domain"
	reporoot "github.com/sirrobot01/mnemo/internal/repo"
	"github.com/spf13/cobra"
)

func newAgentsCommand(root *string) *cobra.Command {
	agentsCmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage the agents Mnemo ingests from",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return output.FromCommand(cmd).Line("mnemo agents requires a subcommand: list, add, remove, or detect")
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured agents",
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
			agents := cfg.Agents
			if len(agents) == 0 {
				agents = defaultAgentConfigs(nil)
			}
			out := output.FromCommand(cmd)
			for _, a := range agents {
				src := "built-in discovery"
				if len(a.Sources) > 0 {
					src = strings.Join(a.Sources, ", ")
				}
				caps := joinCapabilities(a.Capabilities)
				if err := out.Line(fmt.Sprintf("%s  kind=%s  caps=[%s]  sources=%s", a.Name, a.Kind, caps, src)); err != nil {
					return err
				}
			}
			return nil
		},
	}

	detectCmd := &cobra.Command{
		Use:   "detect",
		Short: "Report which known agents are present on this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, err := reporoot.Resolve(*root)
			if err != nil {
				return err
			}
			home, _ := os.UserHomeDir()
			probes := map[string]string{
				"claude":   filepath.Join(home, ".claude"),
				"codex":    filepath.Join(home, ".codex"),
				"continue": filepath.Join(home, ".continue"),
				"aider":    filepath.Join(info.Root, ".aider.chat.history.md"),
				"copilot":  filepath.Join(home, ".copilot", "session-state"),
				"cursor":   filepath.Join(home, ".cursor", "agent"),
				"windsurf": filepath.Join(home, ".devin"),
			}
			out := output.FromCommand(cmd)
			for _, name := range knownAgentNames() {
				status := "not found"
				if st, err := os.Stat(probes[name]); err == nil {
					status = "found"
					_ = st
				}
				if err := out.Line(fmt.Sprintf("%s\t%s\t(%s)", name, status, probes[name])); err != nil {
					return err
				}
			}
			return nil
		},
	}

	var addKind, addParser string
	var addSources, addCaps []string
	addCmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add an agent to the project config",
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
			for _, a := range cfg.Agents {
				if a.Name == name {
					return fmt.Errorf("agent %q already exists", name)
				}
			}
			entry := config.AgentConfig{
				Name:         name,
				Kind:         domain.SessionKind(strings.TrimSpace(addKind)),
				Parser:       domain.SessionKind(strings.TrimSpace(addParser)),
				Sources:      addSources,
				Capabilities: parseAgentCapabilities(addCaps),
			}
			// A known agent name with no explicit kind inherits its defaults.
			if entry.Kind == "" {
				if known, ok := knownAgents[name]; ok {
					entry.Kind = known.kind
					if len(entry.Capabilities) == 0 {
						entry.Capabilities = known.capabilities
					}
				}
			}
			if entry.Kind == "" {
				return fmt.Errorf("agent %q is not a known agent; specify --kind (and --parser for a custom agent)", name)
			}
			cfg.Agents = append(cfg.Agents, entry)
			if err := config.RewriteProject(config.DefaultPath(info.Root), cfg); err != nil {
				return err
			}
			return output.FromCommand(cmd).Line(fmt.Sprintf("added agent %q (kind=%s)", name, entry.Kind))
		},
	}
	addCmd.Flags().StringVar(&addKind, "kind", "", "agent kind (claude|codex|continue|aider|copilot|cursor|windsurf, or a custom kind)")
	addCmd.Flags().StringVar(&addParser, "parser", "", "parser for a custom agent (jsonl, jsonl-openai, jsonl-anthropic)")
	addCmd.Flags().StringSliceVar(&addSources, "source", nil, "transcript glob (repeatable; supports ~ and {repo})")
	addCmd.Flags().StringSliceVar(&addCaps, "cap", nil, "capability tag (repeatable; e.g. resume.cli)")

	removeCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an agent from the project config",
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
			kept := make([]config.AgentConfig, 0, len(cfg.Agents))
			found := false
			for _, a := range cfg.Agents {
				if a.Name == name {
					found = true
					continue
				}
				kept = append(kept, a)
			}
			if !found {
				return fmt.Errorf("agent %q not found", name)
			}
			cfg.Agents = kept
			if err := config.RewriteProject(config.DefaultPath(info.Root), cfg); err != nil {
				return err
			}
			return output.FromCommand(cmd).Line(fmt.Sprintf("removed agent %q", name))
		},
	}

	agentsCmd.AddCommand(listCmd, detectCmd, addCmd, removeCmd)
	return agentsCmd
}

func parseAgentCapabilities(values []string) []config.AgentCapability {
	out := make([]config.AgentCapability, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, config.AgentCapability(value))
		}
	}
	return out
}

func joinCapabilities(values []config.AgentCapability) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return strings.Join(out, ",")
}
