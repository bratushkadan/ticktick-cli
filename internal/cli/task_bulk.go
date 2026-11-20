package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// Bulk task commands: complete-many, move, batch, assign, unassign.

var (
	taskBulkCompleteManyCmd = &cobra.Command{
		Use:   "complete-many --ids id1,id2 [--project P]",
		Short: "Complete up to 50 tasks in one project (inbox when --project is omitted)",
		Long: `Completes multiple tasks in a single request.

The API processes at most 50 IDs per call. When --project is omitted or empty
the current user's inbox is used. The response lists the IDs that were completed.`,
		Args: cobra.NoArgs,
		Example: `  ticktick task complete-many --ids taskId1,taskId2
  ticktick task complete-many --ids taskId1,taskId2 --project 6226ff9877acee87727f6bca --json

Example API response:
["taskId1", "taskId2"]`,
		RunE: taskBulkCompleteManyRun,
	}

	taskBulkMoveCmd = &cobra.Command{
		Use:   "move --from P1 --to P2 --ids id1,id2 | --data JSON",
		Short: "Move one or more tasks between projects",
		Long: `Moves tasks between projects.

Either pass --from, --to and --ids to move several tasks the same way, or pass
--data with a JSON array of {fromProjectId,toProjectId,taskId} objects.`,
		Args: cobra.NoArgs,
		Example: `  ticktick task move --from 69a850ef1c20d2030e148fdd --to 69a850f41c20d2030e148fdf --ids 69a850f8b9061f374d54a046
  ticktick task move --data '[{"fromProjectId":"69a850ef1c20d2030e148fdd","toProjectId":"69a850f41c20d2030e148fdf","taskId":"69a850f8b9061f374d54a046"}]' --json

Example API response:
[
  {"id": "69a850f8b9061f374d54a046", "etag": "43p2zso1"}
]`,
		RunE: taskBulkMoveRun,
	}

	taskBulkBatchCmd = &cobra.Command{
		Use:   "batch --data JSON | --file path.json",
		Short: "Batch create and/or update tasks (≤50 add, ≤50 update)",
		Long: `Creates and updates tasks in a single request.

The body is a JSON object with optional "add" and "update" arrays; each array
supports up to 50 tasks. The response maps task IDs to etags (id2etag) and, for
failed entries, to error codes (id2error).`,
		Args: cobra.NoArgs,
		Example: `  ticktick task batch --data '{"add":[{"title":"New task","projectId":"projectId"}],"update":[{"id":"taskId","projectId":"projectId","title":"Updated task"}]}'
  ticktick task batch --file batch.json --json

Example API response:
{
  "id2etag": {"taskId": "43p2zso1"},
  "id2error": {}
}`,
		RunE: taskBulkBatchRun,
	}

	taskBulkAssignCmd = &cobra.Command{
		Use:   "assign TASK_ID --project P --username U",
		Short: "Assign a task to a member of its project",
		Args:  cobra.ExactArgs(1),
		Example: `  ticktick task assign 63b7bebb91c0a5474805fcd4 --project 6226ff9877acee87727f6bca --username alice
  ticktick task assign 63b7bebb91c0a5474805fcd4 --project 6226ff9877acee87727f6bca --username alice --json

Example API response:
{
  "id": "63b7bebb91c0a5474805fcd4",
  "projectId": "6226ff9877acee87727f6bca",
  "title": "Task Title",
  "content": "Task Content",
  "desc": "Task Description",
  "timeZone": "America/Los_Angeles",
  "repeatFlag": "RRULE:FREQ=DAILY;INTERVAL=1",
  "startDate": "2019-11-13T03:00:00+0000",
  "dueDate": "2019-11-14T03:00:00+0000",
  "reminders": ["TRIGGER:P0DT9H0M0S", "TRIGGER:PT0S"],
  "tags": ["work", "urgent"],
  "priority": 1,
  "status": 0,
  "completedTime": "2019-11-13T03:00:00+0000",
  "sortOrder": 12345,
  "isAllDay": true,
  "assigneeUsername": "alice"
}`,
		RunE: taskBulkAssignRun,
	}

	taskBulkUnassignCmd = &cobra.Command{
		Use:   "unassign TASK_ID --project P",
		Short: "Remove the assignee from a task",
		Args:  cobra.ExactArgs(1),
		Example: `  ticktick task unassign 63b7bebb91c0a5474805fcd4 --project 6226ff9877acee87727f6bca
  ticktick task unassign 63b7bebb91c0a5474805fcd4 --project 6226ff9877acee87727f6bca --json

Example API response:
{
  "id": "63b7bebb91c0a5474805fcd4",
  "projectId": "6226ff9877acee87727f6bca",
  "title": "Task Title",
  "content": "Task Content",
  "desc": "Task Description",
  "timeZone": "America/Los_Angeles",
  "repeatFlag": "RRULE:FREQ=DAILY;INTERVAL=1",
  "startDate": "2019-11-13T03:00:00+0000",
  "dueDate": "2019-11-14T03:00:00+0000",
  "reminders": ["TRIGGER:P0DT9H0M0S", "TRIGGER:PT0S"],
  "tags": ["work", "urgent"],
  "priority": 1,
  "status": 0,
  "completedTime": "2019-11-13T03:00:00+0000",
  "sortOrder": 12345,
  "isAllDay": true
}`,
		RunE: taskBulkUnassignRun,
	}
)

// Flag variables for the bulk task commands.
var (
	taskBulkCompleteManyIDs     string
	taskBulkCompleteManyProject string

	taskBulkMoveFrom string
	taskBulkMoveTo   string
	taskBulkMoveIDs  string
	taskBulkMoveData string

	taskBulkBatchData string
	taskBulkBatchFile string

	taskBulkAssignProject  string
	taskBulkAssignUsername string

	taskBulkUnassignProject string
)

func init() {
	taskCmd.AddCommand(taskBulkCompleteManyCmd, taskBulkMoveCmd, taskBulkBatchCmd,
		taskBulkAssignCmd, taskBulkUnassignCmd)

	taskBulkCompleteManyCmd.Flags().StringVar(&taskBulkCompleteManyIDs, "ids", "",
		"comma-separated task IDs (max 50)")
	taskBulkCompleteManyCmd.Flags().StringVar(&taskBulkCompleteManyProject, "project", "",
		"project ID (optional; defaults to the inbox)")
	_ = taskBulkCompleteManyCmd.MarkFlagRequired("ids")

	taskBulkMoveCmd.Flags().StringVar(&taskBulkMoveFrom, "from", "",
		"source project ID")
	taskBulkMoveCmd.Flags().StringVar(&taskBulkMoveTo, "to", "",
		"destination project ID")
	taskBulkMoveCmd.Flags().StringVar(&taskBulkMoveIDs, "ids", "",
		"comma-separated task IDs to move")
	taskBulkMoveCmd.Flags().StringVar(&taskBulkMoveData, "data", "",
		"JSON array of {fromProjectId,toProjectId,taskId} objects (alternative to --from/--to/--ids)")

	taskBulkBatchCmd.Flags().StringVar(&taskBulkBatchData, "data", "",
		"inline batch JSON: {\"add\":[...],\"update\":[...]} (≤50 add, ≤50 update)")
	taskBulkBatchCmd.Flags().StringVar(&taskBulkBatchFile, "file", "",
		"path to a JSON file with the batch body")

	taskBulkAssignCmd.Flags().StringVar(&taskBulkAssignProject, "project", "",
		"project containing the task")
	taskBulkAssignCmd.Flags().StringVar(&taskBulkAssignUsername, "username", "",
		"username of a project member")
	_ = taskBulkAssignCmd.MarkFlagRequired("project")
	_ = taskBulkAssignCmd.MarkFlagRequired("username")

	taskBulkUnassignCmd.Flags().StringVar(&taskBulkUnassignProject, "project", "",
		"project containing the task")
	_ = taskBulkUnassignCmd.MarkFlagRequired("project")
}

// taskBulkParseIDs splits a comma-separated ID list, trimming whitespace.
func taskBulkParseIDs(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			ids = append(ids, p)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("--ids is required: pass a comma-separated list such as id1,id2")
	}
	return ids, nil
}

// taskBulkSortedKeys returns map keys in deterministic order.
func taskBulkSortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func taskBulkCompleteManyRun(cmd *cobra.Command, _ []string) error {
	ids, err := taskBulkParseIDs(taskBulkCompleteManyIDs)
	if err != nil {
		return err
	}
	req := api.CompleteTasksRequest{TaskIds: ids}
	if p := strings.TrimSpace(taskBulkCompleteManyProject); p != "" {
		req.ProjectId = Ptr(p)
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	resp, err := c.CompleteTasksWithResponse(cmd.Context(), req)
	if err != nil {
		return err
	}
	if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
		return err
	}
	return Render(resp.Body, func(w io.Writer) {
		completed := Strs(resp.JSON200)
		fmt.Fprintf(w, "Completed %d task(s):\n", len(completed))
		for _, id := range completed {
			fmt.Fprintln(w, id)
		}
	})
}

func taskBulkMoveRun(cmd *cobra.Command, _ []string) error {
	from := strings.TrimSpace(taskBulkMoveFrom)
	to := strings.TrimSpace(taskBulkMoveTo)
	rawIDs := strings.TrimSpace(taskBulkMoveIDs)
	rawData := strings.TrimSpace(taskBulkMoveData)

	var body []api.TaskMove
	switch {
	case rawData != "" && (from != "" || to != "" || rawIDs != ""):
		return fmt.Errorf("use either --data or --from/--to/--ids, not both")
	case rawData != "":
		if err := json.Unmarshal([]byte(rawData), &body); err != nil {
			return fmt.Errorf("invalid --data JSON: %w", err)
		}
		if len(body) == 0 {
			return fmt.Errorf("--data must be a non-empty JSON array of {fromProjectId,toProjectId,taskId} objects")
		}
		for i, m := range body {
			if m.FromProjectId == "" || m.ToProjectId == "" || m.TaskId == "" {
				return fmt.Errorf("--data[%d]: fromProjectId, toProjectId and taskId are all required", i)
			}
		}
	default:
		if from == "" || to == "" || rawIDs == "" {
			return fmt.Errorf("--from, --to and --ids are required (or pass --data)")
		}
		ids, err := taskBulkParseIDs(rawIDs)
		if err != nil {
			return err
		}
		body = make([]api.TaskMove, 0, len(ids))
		for _, id := range ids {
			body = append(body, api.TaskMove{
				FromProjectId: from,
				ToProjectId:   to,
				TaskId:        id,
			})
		}
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	resp, err := c.MoveTasksWithResponse(cmd.Context(), body)
	if err != nil {
		return err
	}
	if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
		return err
	}
	return Render(resp.Body, func(w io.Writer) {
		rows := make([][]string, 0)
		if resp.JSON200 != nil {
			for _, r := range *resp.JSON200 {
				rows = append(rows, []string{Str(r.Id), Str(r.Etag)})
			}
		}
		Table(w, []string{"ID", "ETAG"}, rows)
	})
}

func taskBulkBatchRun(cmd *cobra.Command, _ []string) error {
	rawData := strings.TrimSpace(taskBulkBatchData)
	path := strings.TrimSpace(taskBulkBatchFile)

	switch {
	case rawData != "" && path != "":
		return fmt.Errorf("use either --data or --file, not both")
	case rawData == "" && path == "":
		return fmt.Errorf("one of --data or --file is required")
	case path != "":
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read --file %s: %w", path, err)
		}
		rawData = string(b)
	}

	var req api.TaskBatchRequest
	if err := json.Unmarshal([]byte(rawData), &req); err != nil {
		return fmt.Errorf("invalid batch JSON: %w", err)
	}
	if req.Add == nil && req.Update == nil {
		return fmt.Errorf(`batch body must contain "add" and/or "update" arrays`)
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	resp, err := c.BatchTasksWithResponse(cmd.Context(), req)
	if err != nil {
		return err
	}
	if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
		return err
	}
	return Render(resp.Body, func(w io.Writer) {
		var etags, errs map[string]string
		if resp.JSON200 != nil {
			if resp.JSON200.Id2etag != nil {
				etags = *resp.JSON200.Id2etag
			}
			if resp.JSON200.Id2error != nil {
				errs = *resp.JSON200.Id2error
			}
		}
		fmt.Fprintf(w, "OK: %d task(s)\n", len(etags))
		for _, id := range taskBulkSortedKeys(etags) {
			fmt.Fprintf(w, "%s -> %s\n", id, etags[id])
		}
		if len(errs) > 0 {
			fmt.Fprintln(w, "Errors:")
			for _, id := range taskBulkSortedKeys(errs) {
				fmt.Fprintf(w, "%s -> %s\n", id, errs[id])
			}
		}
	})
}

func taskBulkAssignRun(cmd *cobra.Command, args []string) error {
	taskID := strings.TrimSpace(args[0])
	if taskID == "" {
		return fmt.Errorf("TASK_ID must not be empty")
	}
	project := strings.TrimSpace(taskBulkAssignProject)
	username := strings.TrimSpace(taskBulkAssignUsername)
	if project == "" {
		return fmt.Errorf("--project is required")
	}
	if username == "" {
		return fmt.Errorf("--username is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	resp, err := c.AssignTaskWithResponse(cmd.Context(), api.TaskAssignRequest{
		ProjectId:        project,
		TaskId:           taskID,
		AssigneeUsername: username,
	})
	if err != nil {
		return err
	}
	if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
		return err
	}
	return Render(resp.Body, func(w io.Writer) {
		fmt.Fprintf(w, "Assigned %s to %s\n", taskID, username)

		var t api.Task
		if resp.JSON200 != nil {
			t = *resp.JSON200
		}
		id := Str(t.Id)
		if id == "" {
			id = taskID
		}
		assignee := Str(t.AssigneeUsername)
		if assignee == "" {
			assignee = username
		}
		KV(w,
			[2]string{"ID", id},
			[2]string{"Title", Str(t.Title)},
			[2]string{"Assignee", assignee},
		)
	})
}

func taskBulkUnassignRun(cmd *cobra.Command, args []string) error {
	taskID := strings.TrimSpace(args[0])
	if taskID == "" {
		return fmt.Errorf("TASK_ID must not be empty")
	}
	project := strings.TrimSpace(taskBulkUnassignProject)
	if project == "" {
		return fmt.Errorf("--project is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	resp, err := c.UnassignTaskWithResponse(cmd.Context(), api.TaskUnassignRequest{
		ProjectId: project,
		TaskId:    taskID,
	})
	if err != nil {
		return err
	}
	if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
		return err
	}
	return Render(resp.Body, func(w io.Writer) {
		fmt.Fprintf(w, "Unassigned %s\n", taskID)
	})
}
