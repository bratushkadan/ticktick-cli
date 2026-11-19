package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"ticktick-cli/api"
)

// Render implements the global output contract:
// with --json it pretty-prints the raw API response body to stdout;
// otherwise it calls textFn to write compact human-readable text.
func Render(rawBody []byte, textFn func(w io.Writer)) error {
	if flagJSON {
		return printRawJSON(rawBody)
	}
	textFn(os.Stdout)
	return nil
}

// RenderOK is for endpoints with empty response bodies (delete/complete).
// --json prints {"ok":true}; text mode prints the given message.
func RenderOK(text string) error {
	if flagJSON {
		fmt.Fprintln(os.Stdout, `{"ok":true}`)
		return nil
	}
	fmt.Fprintln(os.Stdout, text)
	return nil
}

// PrintJSON marshals v with 2-space indent to stdout (used for derived
// results such as the comments-batch map).
func PrintJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(b))
	return nil
}

func printRawJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		fmt.Fprintln(os.Stdout, `{"ok":true}`)
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		// Not valid JSON — print as-is rather than failing.
		fmt.Fprintln(os.Stdout, string(raw))
		return nil
	}
	fmt.Fprintln(os.Stdout, buf.String())
	return nil
}

// Table writes aligned columns (tab-separated) using tabwriter.
func Table(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

// KV writes "Key: value" lines, skipping empty values.
func KV(w io.Writer, pairs ...[2]string) {
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		fmt.Fprintf(w, "%s: %s\n", p[0], p[1])
	}
}

// PriorityName maps TickTick priority codes to labels.
func PriorityName(p int) string {
	switch p {
	case 0:
		return "none"
	case 1:
		return "low"
	case 3:
		return "medium"
	case 5:
		return "high"
	default:
		return fmt.Sprintf("p%d", p)
	}
}

// TaskStatusName maps TickTick task status codes to labels.
func TaskStatusName(s int) string {
	switch s {
	case -1:
		return "abandoned"
	case 0:
		return "open"
	case 2:
		return "completed"
	default:
		return fmt.Sprintf("s%d", s)
	}
}

// TaskRow builds the standard table row for a task.
func TaskRow(t api.Task) []string {
	return []string{
		Str(t.Id),
		Str(t.Title),
		TaskStatusName(int(I32(t.Status))),
		PriorityName(int(I32(t.Priority))),
		FmtTime(Str(t.DueDate)),
		Str(t.ProjectId),
	}
}

// TaskTableHeaders are the standard headers matching TaskRow.
var TaskTableHeaders = []string{"ID", "TITLE", "STATUS", "PRIORITY", "DUE", "PROJECT"}

// PrintTaskTable renders a list of tasks in the standard compact table.
func PrintTaskTable(w io.Writer, tasks []api.Task) {
	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, TaskRow(t))
	}
	Table(w, TaskTableHeaders, rows)
}