package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
	"ticktick-cli/internal/comments"
)

// Flags for the task comment commands. Each command registers its own
// flags; tests must pass every flag explicitly because pflag values
// persist between rootCmd.Execute calls.
var (
	taskCmtProject string
	taskCmtText    string
	taskCmtIDs     string
	taskCmtTasks   string
	taskCmtWorkers int
)

var taskCommentsCmd = &cobra.Command{
	Use:   "comments TASK_ID",
	Short: "List comments on a task",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick task comments 641a2b3c4d5e6f7890abcdef --project 63f1a2b3c4d5e6f7890abcde
  ticktick task comments 641a2b3c4d5e6f7890abcdef --project 63f1a2b3c4d5e6f7890abcde --json

Example API response (live shape; created/modifiedTime are epoch milliseconds,
although the official docs show strings):
[
  {
    "id": "comment-1",
    "userId": 12345,
    "title": "Comment text",
    "createdTime": 1577934245000,
    "modifiedTime": 1577934245000
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.GetTaskCommentsWithResponse(cmd.Context(), taskCmtProject, args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		list := commentsFromResponse(resp.JSON200, resp.Body)
		return Render(resp.Body, func(w io.Writer) {
			printCommentLines(w, list)
		})
	},
}

var taskCommentAddCmd = &cobra.Command{
	Use:   "comment-add TASK_ID",
	Short: "Add a comment to a task",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick task comment-add 641a2b3c4d5e6f7890abcdef --project 63f1a2b3c4d5e6f7890abcde --text "Looks good to me"
  ticktick task comment-add 641a2b3c4d5e6f7890abcdef --project 63f1a2b3c4d5e6f7890abcde --text "Looks good to me" --json

Example API response (live shape; created/modifiedTime are epoch milliseconds):
{
  "id": "comment-1",
  "userId": 12345,
  "title": "Comment text",
  "createdTime": 1577934245000,
  "modifiedTime": 1577934245000
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.AddTaskCommentWithResponse(cmd.Context(), taskCmtProject, args[0],
			api.CommentCreateRequest{Title: taskCmtText})
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		var added *api.OpenComment
		if resp.JSON200 != nil {
			added = resp.JSON200
		} else if b := bytes.TrimSpace(resp.Body); len(b) > 0 && !bytes.Equal(b, []byte("null")) {
			var oc api.OpenComment
			if err := json.Unmarshal(b, &oc); err == nil {
				added = &oc
			}
		}
		return Render(resp.Body, func(w io.Writer) {
			id := ""
			if added != nil {
				id = Str(added.Id)
			}
			if id == "" {
				fmt.Fprintln(w, "Comment added.")
				return
			}
			fmt.Fprintf(w, "Comment added: %s\n", id)
		})
	},
}

var taskCommentDeleteCmd = &cobra.Command{
	Use:   "comment-delete TASK_ID COMMENT_ID",
	Short: "Delete a comment from a task",
	Args:  cobra.ExactArgs(2),
	Example: `  ticktick task comment-delete 641a2b3c4d5e6f7890abcdef 5f2a1b3c4d5e6f7890abcd12 --project 63f1a2b3c4d5e6f7890abcde
  ticktick task comment-delete 641a2b3c4d5e6f7890abcdef 5f2a1b3c4d5e6f7890abcd12 --project 63f1a2b3c4d5e6f7890abcde --json

Example API response:
(empty body, HTTP 200)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.DeleteTaskCommentWithResponse(cmd.Context(), taskCmtProject, args[0], args[1])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return RenderOK("Comment deleted.")
	},
}

var taskCommentsBatchCmd = &cobra.Command{
	Use:   "comments-batch",
	Short: "Fetch comments for many tasks concurrently (1-8 workers)",
	Long: `Fetch comments for many tasks in one go.

Two input forms (mutually exclusive):
  --project <projectId> --ids <taskId1,taskId2,...>
  --tasks <projectIdA:taskId1,projectIdB:taskId2,...>

The per-task API endpoint returns a plain array of comments; this command
assembles a taskID → comments map client-side, fetching up to 8 tasks
concurrently (--workers, default 4, clamped to 1..8). Per-task failures
are reported per task without aborting the batch; partial results are
still printed and the exit code is 1.`,
	Args: cobra.NoArgs,
	Example: `  # form 1: one project, many task IDs
  ticktick task comments-batch --project 63f1a2b3c4d5e6f7890abcde --ids 641a2b3c4d5e6f7890ab01,641a2b3c4d5e6f7890ab02 --workers 4
  # form 2: explicit project:task pairs (may span projects)
  ticktick task comments-batch --tasks 63f1a2b3c4d5e6f7890abcde:641a2b3c4d5e6f7890ab01,63f1a2b3c4d5e6f7890abcdf:641a2b3c4d5e6f7890ab02 --workers 8 --json

Example API response (assembled map; per-task comment times are epoch milliseconds):
{
  "<taskId>": [
    {
      "id": "comment-1",
      "userId": 12345,
      "title": "Comment text",
      "createdTime": 1577934245000,
      "modifiedTime": 1577934245000
    }
  ]
}

Note: GET /open/v1/project/{projectId}/task/{taskId}/comments returns an
array per task; the batch command assembles the taskID → comments map
client-side with 1-8 workers. Metadata about failures goes to stderr.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		refs, err := taskCmtBuildRefs(taskCmtProject, taskCmtIDs, taskCmtTasks)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}

		res := comments.FetchAll(cmd.Context(), c, refs, taskCmtWorkers)

		if flagJSON {
			// Pure map on stdout: taskID → []comments, nothing else.
			if err := PrintJSON(res.Comments); err != nil {
				return err
			}
		} else {
			for _, ref := range refs {
				if e := res.Errors[ref.TaskID]; e != nil {
					fmt.Fprintf(os.Stdout, "task %s — ERROR:\n", ref.TaskID)
					fmt.Fprintf(os.Stdout, "  ERROR: %v\n", e)
					continue
				}
				list := res.Comments[ref.TaskID]
				fmt.Fprintf(os.Stdout, "task %s — %d comment(s):\n", ref.TaskID, len(list))
				printCommentLines(os.Stdout, list)
			}
		}

		if len(res.Errors) > 0 {
			// Per-task diagnostics always go to stderr; the partial result
			// (map or text) has already been printed above.
			for _, ref := range refs {
				if e := res.Errors[ref.TaskID]; e != nil {
					fmt.Fprintf(os.Stderr, "task %s: %v\n", ref.TaskID, e)
				}
			}
			return fmt.Errorf("%d of %d tasks failed", len(res.Errors), len(refs))
		}
		return nil
	},
}

// commentsFromResponse returns the decoded comment list, preferring the
// generated JSON200 decode and falling back to decoding the raw body
// (the generated parser only fills JSON200 for JSON content types).
// The result is never nil.
func commentsFromResponse(decoded *[]api.OpenComment, body []byte) []api.OpenComment {
	if decoded != nil {
		if *decoded == nil {
			return []api.OpenComment{}
		}
		return *decoded
	}
	var out []api.OpenComment
	if b := bytes.TrimSpace(body); len(b) > 0 && !bytes.Equal(b, []byte("null")) {
		if err := json.Unmarshal(b, &out); err != nil {
			return []api.OpenComment{}
		}
	}
	if out == nil {
		out = []api.OpenComment{}
	}
	return out
}

// printCommentLines writes one line per comment, or "(no comments)".
func printCommentLines(w io.Writer, list []api.OpenComment) {
	if len(list) == 0 {
		fmt.Fprintln(w, "(no comments)")
		return
	}
	for _, cm := range list {
		fmt.Fprintf(w, "  [%s] %s  (id: %s)\n", FmtEpochMs(I64(cm.CreatedTime)), Str(cm.Title), Str(cm.Id))
	}
}

// taskCmtBuildRefs validates the mutually exclusive input forms of
// comments-batch and builds refs in input order.
func taskCmtBuildRefs(project, ids, tasks string) ([]comments.TaskRef, error) {
	project = strings.TrimSpace(project)
	ids = strings.TrimSpace(ids)
	tasks = strings.TrimSpace(tasks)

	hasProject, hasIDs, hasTasks := project != "", ids != "", tasks != ""

	if hasTasks && (hasProject || hasIDs) {
		return nil, fmt.Errorf("use either --project and --ids or --tasks, not both")
	}
	if !hasTasks && (!hasProject || !hasIDs) {
		return nil, fmt.Errorf("specify either --project and --ids, or --tasks")
	}

	if hasTasks {
		parts := strings.Split(tasks, ",")
		refs := make([]comments.TaskRef, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			kv := strings.SplitN(p, ":", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid --tasks entry %q: expected PROJECT_ID:TASK_ID", p)
			}
			projID := strings.TrimSpace(kv[0])
			taskID := strings.TrimSpace(kv[1])
			if projID == "" || taskID == "" {
				return nil, fmt.Errorf("invalid --tasks entry %q: expected PROJECT_ID:TASK_ID", p)
			}
			refs = append(refs, comments.TaskRef{ProjectID: projID, TaskID: taskID})
		}
		if len(refs) == 0 {
			return nil, fmt.Errorf("--tasks is empty: expected PROJECT_ID:TASK_ID[,PROJECT_ID:TASK_ID...]")
		}
		return refs, nil
	}

	parts := strings.Split(ids, ",")
	refs := make([]comments.TaskRef, 0, len(parts))
	for _, id := range parts {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		refs = append(refs, comments.TaskRef{ProjectID: project, TaskID: id})
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("--ids is empty: expected a comma-separated list of task IDs")
	}
	return refs, nil
}

func init() {
	taskCmd.AddCommand(taskCommentsCmd, taskCommentAddCmd, taskCommentDeleteCmd, taskCommentsBatchCmd)

	taskCommentsCmd.Flags().StringVar(&taskCmtProject, "project", "", "project ID (required)")
	_ = taskCommentsCmd.MarkFlagRequired("project")

	taskCommentAddCmd.Flags().StringVar(&taskCmtProject, "project", "", "project ID (required)")
	taskCommentAddCmd.Flags().StringVar(&taskCmtText, "text", "", "comment text (required)")
	_ = taskCommentAddCmd.MarkFlagRequired("project")
	_ = taskCommentAddCmd.MarkFlagRequired("text")

	taskCommentDeleteCmd.Flags().StringVar(&taskCmtProject, "project", "", "project ID (required)")
	_ = taskCommentDeleteCmd.MarkFlagRequired("project")

	taskCommentsBatchCmd.Flags().StringVar(&taskCmtProject, "project", "", "project ID (form 1, with --ids)")
	taskCommentsBatchCmd.Flags().StringVar(&taskCmtIDs, "ids", "", "comma-separated task IDs (form 1, requires --project)")
	taskCommentsBatchCmd.Flags().StringVar(&taskCmtTasks, "tasks", "", "comma-separated PROJECT_ID:TASK_ID pairs (form 2)")
	taskCommentsBatchCmd.Flags().IntVar(&taskCmtWorkers, "workers", comments.DefaultWorkers, "concurrent workers, clamped to 1-8")
}
