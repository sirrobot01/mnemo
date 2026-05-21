// Package resumesvc renders a compiled WorkingState into the state-of-play
// text the next agent inherits. It enforces two safety boundaries:
// cross-vendor injection is refused unless explicitly allowed, and the
// rendered output is secret-scanned a second time (belt-and-suspenders over
// ingest-time scanning).
package resumesvc

import (
	"fmt"
	"strings"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/safety"
)

// Options controls a render.
type Options struct {
	// Tool is the agent the state of play is being injected into. Empty,
	// "stdout", or "generic" means "just print locally" and is never gated.
	Tool string
	// CrossVendorAllowed must be true to render for an agent whose vendor is
	// not among the sources the state was derived from.
	CrossVendorAllowed bool
	// Context is already-resolved, already-scrubbed read-only knowledge
	// (house rules, AGENTS.md, docs) appended to the handoff so the next
	// agent sees it. Mnemo never writes back into these sources.
	Context string
}

type Rendered struct {
	Tool    string `json:"tool"`
	Content string `json:"content"`
}

// ErrCrossVendorEgress is returned when the target agent differs from every
// source tool and the caller has not opted in.
var ErrCrossVendorEgress = fmt.Errorf("refusing cross-vendor injection: enable it explicitly (privacy.allow_cross_vendor_egress)")

func localTarget(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "", "stdout", "generic":
		return true
	default:
		return false
	}
}

// Render produces the state-of-play text. sourceKinds is the set of vendor
// kinds the task's sessions came from; egress to a different vendor is gated.
func Render(ws domain.WorkingState, sourceKinds []domain.SessionKind, opts Options) (Rendered, error) {
	if !localTarget(opts.Tool) && !opts.CrossVendorAllowed {
		fromSource := false
		for _, st := range sourceKinds {
			if strings.EqualFold(string(st), strings.TrimSpace(opts.Tool)) {
				fromSource = true
				break
			}
		}
		if !fromSource {
			return Rendered{}, ErrCrossVendorEgress
		}
	}

	var b strings.Builder
	b.WriteString("<!-- mnemo:resume:start -->\n")
	goal := ws.Goal
	if goal == "" {
		goal = "(no goal captured)"
	}
	fmt.Fprintf(&b, "# Resume — %s\n", goal)

	section(&b, "Done", ws.Done)
	if ws.InProgress != "" {
		b.WriteString("\n## In progress\n" + ws.InProgress + "\n")
	}
	section(&b, "Next steps", ws.NextSteps)

	if len(ws.Decisions) > 0 {
		b.WriteString("\n## Decisions\n")
		for _, d := range ws.Decisions {
			if d.Rationale != "" {
				fmt.Fprintf(&b, "- %s — %s\n", d.Decision, d.Rationale)
			} else {
				fmt.Fprintf(&b, "- %s\n", d.Decision)
			}
		}
	}
	if len(ws.Rejected) > 0 {
		b.WriteString("\n## Rejected — do not retry\n")
		for _, r := range ws.Rejected {
			fmt.Fprintf(&b, "- %s — %s\n", r.Approach, r.Reason)
		}
	}
	section(&b, "Open questions", ws.OpenQuestions)
	if len(ws.FilesTouched) > 0 {
		b.WriteString("\n## Files touched\n")
		for _, f := range ws.FilesTouched {
			if f.Summary != "" {
				fmt.Fprintf(&b, "- %s — %s\n", f.Path, f.Summary)
			} else {
				fmt.Fprintf(&b, "- %s\n", f.Path)
			}
		}
	}
	if len(ws.Hypotheses) > 0 {
		b.WriteString("\n## Working hypotheses — UNCONFIRMED, do not treat as fact\n")
		for _, h := range ws.Hypotheses {
			fmt.Fprintf(&b, "- %s\n", h.Claim)
		}
	}
	if c := strings.TrimSpace(opts.Context); c != "" {
		b.WriteString("\n## Context — read-only house rules, do not modify these sources\n")
		b.WriteString(c)
		if !strings.HasSuffix(c, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("<!-- mnemo:resume:end -->\n")

	return Rendered{Tool: opts.Tool, Content: redact(b.String())}, nil
}

func section(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", title)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
}

// redact replaces any line that trips the secret scanner. The whole line is
// dropped rather than masked in place — never emit a partial secret.
func redact(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if safety.RejectIfSecret(line) != nil {
			lines[i] = "[REDACTED]"
		}
	}
	return strings.Join(lines, "\n")
}
