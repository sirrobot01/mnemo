package output

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type Format string

const (
	FormatHuman Format = "human"
	FormatJSON  Format = "json"
)

type formatKey struct{}

type Renderer struct {
	out    io.Writer
	format Format
}

type Message struct {
	Message string `json:"message"`
}

type InitResult struct {
	Message string `json:"message"`
	Path    string `json:"path"`
}

type MigrationResult struct {
	Strategy string `json:"strategy"`
	Applied  int    `json:"applied"`
	Skipped  int    `json:"skipped"`
}

type MigrationStatus struct {
	Database string `json:"database"`
	DSN      string `json:"dsn"`
	Strategy string `json:"strategy"`
	Applied  int    `json:"applied"`
	Pending  int    `json:"pending"`
}

func ParseFormat(value string) (Format, error) {
	switch value {
	case "", string(FormatHuman), "text":
		return FormatHuman, nil
	case string(FormatJSON):
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported output format %q", value)
	}
}

func WithFormat(ctx context.Context, format Format) context.Context {
	return context.WithValue(ctx, formatKey{}, format)
}

func FormatFromContext(ctx context.Context) Format {
	format, ok := ctx.Value(formatKey{}).(Format)
	if !ok || format == "" {
		return FormatHuman
	}
	return format
}

func FromCommand(cmd *cobra.Command) Renderer {
	return Renderer{
		out:    cmd.OutOrStdout(),
		format: FormatFromContext(cmd.Context()),
	}
}

type TaskView struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Goal         string `json:"goal,omitempty"`
	Status       string `json:"status"`
	Branch       string `json:"branch,omitempty"`
	Sessions     int    `json:"sessions"`
	LastActiveAt string `json:"last_active_at,omitempty"`
}

type StatusView struct {
	ActiveTask     *TaskView `json:"active_task,omitempty"`
	WorkingVersion int       `json:"working_state_version,omitempty"`
	Goal           string    `json:"goal,omitempty"`
	Message        string    `json:"message,omitempty"`
}

func (r Renderer) Text(content string) error {
	if r.format == FormatJSON {
		return r.JSON(map[string]string{"content": content})
	}
	_, err := io.WriteString(r.out, content)
	return err
}

func (r Renderer) TaskList(tasks []TaskView) error {
	if r.format == FormatJSON {
		if tasks == nil {
			tasks = []TaskView{}
		}
		return r.JSON(tasks)
	}
	if len(tasks) == 0 {
		return r.Line("No tasks.")
	}
	for _, t := range tasks {
		if _, err := fmt.Fprintf(r.out, "%s  [%s]  %s  (%d sessions)\n", t.ID, t.Status, t.Title, t.Sessions); err != nil {
			return err
		}
	}
	return nil
}

func (r Renderer) Task(t TaskView) error {
	if r.format == FormatJSON {
		return r.JSON(t)
	}
	_, err := fmt.Fprintf(
		r.out,
		"ID: %s\nTitle: %s\nGoal: %s\nStatus: %s\nBranch: %s\nSessions: %d\nLast active: %s\n",
		t.ID, t.Title, t.Goal, t.Status, t.Branch, t.Sessions, t.LastActiveAt,
	)
	return err
}

func (r Renderer) Status(v StatusView) error {
	if r.format == FormatJSON {
		return r.JSON(v)
	}
	if v.ActiveTask == nil {
		return r.Line(orDefault(v.Message, "No active task. Run `mnemo ingest` then `mnemo resume`."))
	}
	_, err := fmt.Fprintf(
		r.out,
		"Active task: %s  [%s]\nTitle: %s\nGoal: %s\nSessions: %d\nWorking state: v%d\n",
		v.ActiveTask.ID, v.ActiveTask.Status, v.ActiveTask.Title, v.Goal, v.ActiveTask.Sessions, v.WorkingVersion,
	)
	return err
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func (r Renderer) Line(line string) error {
	if r.format == FormatJSON {
		return r.JSON(Message{Message: line})
	}
	_, err := fmt.Fprintln(r.out, line)
	return err
}

func (r Renderer) JSON(value any) error {
	encoder := json.NewEncoder(r.out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (r Renderer) Initialized(path string) error {
	if r.format == FormatJSON {
		return r.JSON(InitResult{Message: "initialized", Path: path})
	}
	_, err := fmt.Fprintf(r.out, "Initialized Mnemo at %s\n", path)
	return err
}

func (r Renderer) MigrationResult(strategy string, applied int, skipped int) error {
	if r.format == FormatJSON {
		return r.JSON(MigrationResult{Strategy: strategy, Applied: applied, Skipped: skipped})
	}
	_, err := fmt.Fprintf(r.out, "Migration strategy: %s\nApplied migrations: %d\nSkipped migrations: %d\n", strategy, applied, skipped)
	return err
}

type IngestResult struct {
	Tool             string `json:"tool"`
	Discovered       int    `json:"discovered"`
	Imported         int    `json:"imported"`
	Unchanged        int    `json:"unchanged"`
	Skipped          int    `json:"skipped"`
	RedactedEvents   int    `json:"redacted_events"`
	RedactedSessions int    `json:"redacted_sessions"`
}

func (r Renderer) IngestResults(results []IngestResult) error {
	if r.format == FormatJSON {
		if results == nil {
			results = []IngestResult{}
		}
		return r.JSON(results)
	}
	if len(results) == 0 {
		return r.Line("No session sources found.")
	}
	for _, res := range results {
		if _, err := fmt.Fprintf(
			r.out,
			"%s: discovered %d, imported %d, unchanged %d, skipped %d, redacted events %d, redacted sessions %d\n",
			res.Tool, res.Discovered, res.Imported, res.Unchanged, res.Skipped, res.RedactedEvents, res.RedactedSessions,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r Renderer) MigrationStatus(database string, dsn string, strategy string, applied int, pending int) error {
	if r.format == FormatJSON {
		return r.JSON(MigrationStatus{Database: database, DSN: dsn, Strategy: strategy, Applied: applied, Pending: pending})
	}
	_, err := fmt.Fprintf(r.out, "Database: %s\nDSN: %s\nMigration strategy: %s\nApplied migrations: %d\nPending migrations: %d\n", database, dsn, strategy, applied, pending)
	return err
}
