package cli

import (
	"sort"

	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/sessions"
	"github.com/sirrobot01/mnemo/internal/sessions/aider"
	"github.com/sirrobot01/mnemo/internal/sessions/claude"
	"github.com/sirrobot01/mnemo/internal/sessions/codex"
	"github.com/sirrobot01/mnemo/internal/sessions/continueide"
	"github.com/sirrobot01/mnemo/internal/sessions/copilot"
	"github.com/sirrobot01/mnemo/internal/sessions/cursor"
	"github.com/sirrobot01/mnemo/internal/sessions/windsurf"
)

// builtinProviders maps each known kind to its built-in transcript provider.
// It lives in the cli package (not sessions) so the sessions package never
// imports the adapter packages — that would create an import cycle.
func builtinProviders() map[domain.SessionKind]sessions.Provider {
	return map[domain.SessionKind]sessions.Provider{
		domain.SessionKindClaude:   claude.New(""),
		domain.SessionKindCodex:    codex.New(""),
		domain.SessionKindAider:    aider.New(),
		domain.SessionKindContinue: continueide.New(""),
		domain.SessionKindCopilot:  copilot.New(""),
		domain.SessionKindCursor:   cursor.New(""),
		domain.SessionKindWindsurf: windsurf.New(""),
	}
}

// buildRegistry assembles the effective agent registry from config. An empty
// agents list (e.g. a repo initialized before agents existed) falls back to
// every known agent so ingestion still works out of the box.
func buildRegistry(cfg config.Config) (*sessions.Registry, error) {
	agents := cfg.Agents
	if len(agents) == 0 {
		agents = defaultAgentConfigs(nil)
	}
	return sessions.NewRegistry(agents, builtinProviders())
}

// knownAgent is the built-in default for a recognized coding tool. Known
// agents carry no explicit sources: discovery delegates to the built-in
// provider, which knows the tool's on-disk layout and scopes results to this
// repository. A user can still add `sources` in config to override that.
type knownAgent struct {
	kind         domain.SessionKind
	capabilities []config.AgentCapability
}

// knownAgents is the registry of agents Mnemo recognizes out of the box. A
// custom agent is anything not in this table; it must declare its own parser
// and sources in .mnemo/config.yaml.
var knownAgents = map[string]knownAgent{
	"claude": {
		kind: domain.SessionKindClaude,
		capabilities: []config.AgentCapability{
			config.CapabilityResumeCLI,
			config.CapabilityResumeStdin,
		},
	},
	"codex": {
		kind: domain.SessionKindCodex,
		capabilities: []config.AgentCapability{
			config.CapabilityResumeCLI,
			config.CapabilityResumeStdin,
		},
	},
	"continue": {
		kind: domain.SessionKindContinue,
		capabilities: []config.AgentCapability{
			config.CapabilityResumeCLI,
			config.CapabilityResumeFile,
		},
	},
	"aider": {
		kind: domain.SessionKindAider,
		capabilities: []config.AgentCapability{
			config.CapabilityResumeCLI,
			config.CapabilityResumeFile,
		},
	},
	"copilot": {
		kind: domain.SessionKindCopilot,
		capabilities: []config.AgentCapability{
			config.CapabilityResumeCLI,
			config.CapabilityResumeFile,
		},
	},
	"cursor": {
		kind: domain.SessionKindCursor,
		capabilities: []config.AgentCapability{
			config.CapabilityResumeCLI,
			config.CapabilityResumeFile,
		},
	},
	"windsurf": {
		kind: domain.SessionKindWindsurf,
		capabilities: []config.AgentCapability{
			config.CapabilityResumeCLI,
			config.CapabilityResumeFile,
		},
	},
}

// defaultAgentConfigs builds the agents block written to a new project's
// config. With no names it registers every known agent; otherwise only the
// named known agents (unknown names are skipped — custom agents are added
// later via `mnemo agents add`).
func defaultAgentConfigs(names []string) []config.AgentConfig {
	if len(names) == 0 {
		names = knownAgentNames()
	}
	out := make([]config.AgentConfig, 0, len(names))
	for _, name := range names {
		known, ok := knownAgents[name]
		if !ok {
			continue
		}
		out = append(out, config.AgentConfig{
			Name:         name,
			Kind:         known.kind,
			Capabilities: known.capabilities,
		})
	}
	return out
}

func knownAgentNames() []string {
	names := make([]string, 0, len(knownAgents))
	for name := range knownAgents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
