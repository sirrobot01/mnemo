package cli

import "testing"

func TestResumeAgentFromArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		legacyTool   string
		wantAgent    string
		wantExplicit bool
		wantErr      bool
	}{
		{name: "default prints locally"},
		{name: "claude positional", args: []string{"claude"}, wantAgent: "claude", wantExplicit: true},
		{name: "codex positional", args: []string{"codex"}, wantAgent: "codex", wantExplicit: true},
		{name: "aider positional", args: []string{"aider"}, wantAgent: "aider", wantExplicit: true},
		{name: "continue positional", args: []string{"continue"}, wantAgent: "continue", wantExplicit: true},
		{name: "copilot positional", args: []string{"copilot"}, wantAgent: "copilot", wantExplicit: true},
		{name: "cursor positional", args: []string{"cursor"}, wantAgent: "cursor", wantExplicit: true},
		{name: "windsurf positional", args: []string{"windsurf"}, wantAgent: "windsurf", wantExplicit: true},
		{name: "claude alias", args: []string{"claude-code"}, wantAgent: "claude", wantExplicit: true},
		{name: "codex alias", args: []string{"openai-codex"}, wantAgent: "codex", wantExplicit: true},
		{name: "aider alias", args: []string{"aider-chat"}, wantAgent: "aider", wantExplicit: true},
		{name: "continue alias", args: []string{"cn"}, wantAgent: "continue", wantExplicit: true},
		{name: "copilot alias", args: []string{"github-copilot"}, wantAgent: "copilot", wantExplicit: true},
		{name: "cursor alias", args: []string{"cursor-agent"}, wantAgent: "cursor", wantExplicit: true},
		{name: "windsurf alias", args: []string{"devin"}, wantAgent: "windsurf", wantExplicit: true},
		{name: "legacy tool", legacyTool: "codex", wantAgent: "codex"},
		{name: "local target", args: []string{"stdout"}, wantExplicit: true},
		{name: "unsupported", args: []string{"unknown"}, wantErr: true},
		{name: "ambiguous", args: []string{"codex"}, legacyTool: "claude", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAgent, gotExplicit, err := resumeAgentFromArgs(tt.args, tt.legacyTool)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotAgent != tt.wantAgent || gotExplicit != tt.wantExplicit {
				t.Fatalf("got agent=%q explicit=%v, want agent=%q explicit=%v", gotAgent, gotExplicit, tt.wantAgent, tt.wantExplicit)
			}
		})
	}
}

func TestResumeAgentSpecs(t *testing.T) {
	tests := []struct {
		agent       string
		wantCommand string
		wantArgs    []string
	}{
		{agent: "aider", wantCommand: "aider", wantArgs: []string{"--message", "handoff"}},
		{agent: "claude", wantCommand: "claude", wantArgs: []string{"handoff"}},
		{agent: "codex", wantCommand: "codex", wantArgs: []string{"handoff"}},
		{agent: "continue", wantCommand: "cn", wantArgs: []string{"-p", "handoff"}},
		{agent: "copilot", wantCommand: "copilot", wantArgs: []string{"-i", "handoff"}},
		{agent: "cursor", wantCommand: "cursor-agent", wantArgs: []string{"handoff"}},
		{agent: "windsurf", wantCommand: "devin", wantArgs: []string{"--", "handoff"}},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			spec, ok := resumeAgentSpecs[tt.agent]
			if !ok {
				t.Fatalf("missing spec for %s", tt.agent)
			}
			if spec.Command != tt.wantCommand {
				t.Fatalf("command = %q, want %q", spec.Command, tt.wantCommand)
			}
			got := spec.Args("handoff")
			if len(got) != len(tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", got, tt.wantArgs)
			}
			for i := range got {
				if got[i] != tt.wantArgs[i] {
					t.Fatalf("args = %#v, want %#v", got, tt.wantArgs)
				}
			}
		})
	}
}
