package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// This file implements the core task CRUD commands:
//
//	task get      TASK_ID --project P
//	task create   --title T --project P [flags]
//	task update   TASK_ID --project P [flags]
//	task complete TASK_ID --project P
//	task delete   TASK_ID --project P
//
// All optional request fields are pointers in the generated api package;
// the helpers in ptr.go (Ptr, Str, I32, ...) are used throughout.

// taskCrudFlags holds the optional flags shared by `task create` and
// `task update`. Explicit flags always override --data fields.
type taskCrudFlags struct {
	title      string
	project    string
	content    string
	desc       string
	allDay     bool
	start      string
	due        string
	tz         string
	reminders  []string
	tags       []string
	repeat     string
	repeatFrom string
	priority   int
	sortOrder  int64
	parent     string
	kind       string
	items      []string
	data       string
}

// taskCrudRegisterFlags registers the shared optional flags on cmd.
func taskCrudRegisterFlags(cmd *cobra.Command, f *taskCrudFlags) {
	fl := cmd.Flags()
	fl.StringVar(&f.title, "title", "", "task title")
	fl.StringVar(&f.project, "project", "", "project id (required)")
	fl.StringVar(&f.content, "content", "", "task content")
	fl.StringVar(&f.desc, "desc", "", "description of the checklist")
	fl.BoolVar(&f.allDay, "all-day", false, "mark the task as all day")
	fl.StringVar(&f.start, "start", "",
		`start time: YYYY-MM-DD, "YYYY-MM-DD HH:MM", or RFC3339`)
	fl.StringVar(&f.due, "due", "",
		`due time: YYYY-MM-DD, "YYYY-MM-DD HH:MM", or RFC3339`)
	fl.StringVar(&f.tz, "tz", "", "time zone the times are specified in, e.g. America/Los_Angeles")
	fl.StringArrayVar(&f.reminders, "reminder", nil,
		"reminder trigger, e.g. TRIGGER:PT0S (repeatable)")
	fl.StringArrayVar(&f.tags, "tag", nil, "task tag (repeatable)")
	fl.StringVar(&f.repeat, "repeat", "", "recurring rule, e.g. RRULE:FREQ=DAILY;INTERVAL=1")
	fl.StringVar(&f.repeatFrom, "repeat-from", "",
		"recurrence mode: 0 original date, 1 completion date, 2 calendar")
	fl.IntVar(&f.priority, "priority", 0, "priority: 0 none, 1 low, 3 medium, 5 high")
	fl.Int64Var(&f.sortOrder, "sort-order", 0, "task sort order")
	fl.StringVar(&f.parent, "parent", "", `parent task id (pass "" to detach)`)
	fl.StringVar(&f.kind, "kind", "", "task kind: TEXT, NOTE or CHECKLIST")
	fl.StringArrayVar(&f.items, "item", nil, "checklist item title (repeatable)")
	fl.StringVar(&f.data, "data", "",
		"raw JSON request body; fields are merged first and explicit flags win")
}

// applyCreate merges --data (already unmarshalled into req) and every flag
// that was explicitly set into the create request.
func (f *taskCrudFlags) applyCreate(cmd *cobra.Command, req *api.TaskCreateRequest) error {
	fl := cmd.Flags()
	if fl.Changed("title") {
		req.Title = f.title
	}
	if fl.Changed("project") {
		req.ProjectId = f.project
	}
	if fl.Changed("content") {
		req.Content = Ptr(f.content)
	}
	if fl.Changed("desc") {
		req.Desc = Ptr(f.desc)
	}
	if fl.Changed("all-day") {
		req.IsAllDay = Ptr(f.allDay)
	}
	if fl.Changed("start") {
		v, err := NormalizeTime(f.start)
		if err != nil {
			return err
		}
		req.StartDate = Ptr(v)
	}
	if fl.Changed("due") {
		v, err := NormalizeTime(f.due)
		if err != nil {
			return err
		}
		req.DueDate = Ptr(v)
	}
	if fl.Changed("tz") {
		req.TimeZone = Ptr(f.tz)
	}
	if fl.Changed("reminder") {
		req.Reminders = Ptr(f.reminders)
	}
	if fl.Changed("tag") {
		req.Tags = Ptr(f.tags)
	}
	if fl.Changed("repeat") {
		req.RepeatFlag = Ptr(f.repeat)
	}
	if fl.Changed("repeat-from") {
		req.RepeatFrom = Ptr(api.TaskCreateRequestRepeatFrom(f.repeatFrom))
	}
	if fl.Changed("priority") {
		req.Priority = Ptr(int32(f.priority))
	}
	if fl.Changed("sort-order") {
		req.SortOrder = Ptr(f.sortOrder)
	}
	if fl.Changed("parent") {
		req.ParentId = Ptr(f.parent)
	}
	if fl.Changed("kind") {
		req.Kind = Ptr(api.TaskCreateRequestKind(f.kind))
	}
	if fl.Changed("item") {
		req.Items = taskCrudItems(f.items)
	}
	return nil
}

// applyUpdate merges --data (already unmarshalled into req) and every flag
// that was explicitly set into the update request.
func (f *taskCrudFlags) applyUpdate(cmd *cobra.Command, req *api.TaskUpdateRequest) error {
	fl := cmd.Flags()
	if fl.Changed("title") {
		req.Title = Ptr(f.title)
	}
	if fl.Changed("project") {
		req.ProjectId = f.project
	}
	if fl.Changed("content") {
		req.Content = Ptr(f.content)
	}
	if fl.Changed("desc") {
		req.Desc = Ptr(f.desc)
	}
	if fl.Changed("all-day") {
		req.IsAllDay = Ptr(f.allDay)
	}
	if fl.Changed("start") {
		v, err := NormalizeTime(f.start)
		if err != nil {
			return err
		}
		req.StartDate = Ptr(v)
	}
	if fl.Changed("due") {
		v, err := NormalizeTime(f.due)
		if err != nil {
			return err
		}
		req.DueDate = Ptr(v)
	}
	if fl.Changed("tz") {
		req.TimeZone = Ptr(f.tz)
	}
	if fl.Changed("reminder") {
		req.Reminders = Ptr(f.reminders)
	}
	if fl.Changed("tag") {
		req.Tags = Ptr(f.tags)
	}
	if fl.Changed("repeat") {
		req.RepeatFlag = Ptr(f.repeat)
	}
	if fl.Changed("repeat-from") {
		req.RepeatFrom = Ptr(api.TaskUpdateRequestRepeatFrom(f.repeatFrom))
	}
	if fl.Changed("priority") {
		req.Priority = Ptr(int32(f.priority))
	}
	if fl.Changed("sort-order") {
		req.SortOrder = Ptr(f.sortOrder)
	}
	if fl.Changed("parent") {
		req.ParentId = Ptr(f.parent)
	}
	if fl.Changed("kind") {
		req.Kind = Ptr(api.TaskUpdateRequestKind(f.kind))
	}
	if fl.Changed("item") {
		req.Items = taskCrudItems(f.items)
	}
	return nil
}

// taskCrudItems turns repeated --item values into checklist items.
func taskCrudItems(titles []string) *[]api.ChecklistItem {
	items := make([]api.ChecklistItem, 0, len(titles))
	for _, s := range titles {
		items = append(items, api.ChecklistItem{Title: Ptr(s)})
	}
	return &items
}

// taskCrudDecodeTask parses a Task response body. Empty bodies (some 201
// responses) yield a zero Task rather than an error.
func taskCrudDecodeTask(body []byte) (api.Task, error) {
	var t api.Task
	if len(bytes.TrimSpace(body)) == 0 {
		return t, nil
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return t, fmt.Errorf("decode task: %w", err)
	}
	return t, nil
}

// taskCrudKind dereferences an optional TaskKind.
func taskCrudKind(p *api.TaskKind) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// taskCrudPrintTask renders the full task detail block (ID first).
func taskCrudPrintTask(w io.Writer, t api.Task) {
	KV(w, [2]string{"ID", Str(t.Id)})
	taskCrudPrintTaskDetails(w, t)
}

// taskCrudPrintTaskDetails renders every key task field plus the checklist.
func taskCrudPrintTaskDetails(w io.Writer, t api.Task) {
	KV(w,
		[2]string{"Title", Str(t.Title)},
		[2]string{"Status", TaskStatusName(int(I32(t.Status)))},
		[2]string{"Priority", PriorityName(int(I32(t.Priority)))},
		[2]string{"Project", Str(t.ProjectId)},
		[2]string{"Start", FmtTime(Str(t.StartDate))},
		[2]string{"Due", FmtTime(Str(t.DueDate))},
		[2]string{"Time zone", Str(t.TimeZone)},
		[2]string{"Kind", taskCrudKind(t.Kind)},
		[2]string{"Parent", Str(t.ParentId)},
		[2]string{"Content", Str(t.Content)},
		[2]string{"Desc", Str(t.Desc)},
		[2]string{"Tags", strings.Join(Strs(t.Tags), ", ")},
		[2]string{"Repeat", Str(t.RepeatFlag)},
	)
	taskCrudPrintChecklist(w, t)
}

// taskCrudPrintChecklist prints subtask lines; status 1 means completed.
func taskCrudPrintChecklist(w io.Writer, t api.Task) {
	if t.Items == nil || len(*t.Items) == 0 {
		return
	}
	items := *t.Items
	fmt.Fprintln(w, "Checklist:")
	for _, it := range items {
		mark := " "
		if I32(it.Status) == 1 {
			mark = "x"
		}
		fmt.Fprintf(w, "  [%s] %s\n", mark, Str(it.Title))
	}
}

var taskCrudGetCmd = &cobra.Command{
	Use:   "get TASK_ID --project PROJECT_ID",
	Short: "Get a task by project and task id",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick task get 63b7bebb91c0a5474805fcd4 --project 6226ff9877acee87727f6bca

Example API response:
{
  "id": "63b7bebb91c0a5474805fcd4",
  "projectId": "6226ff9877acee87727f6bca",
  "title": "Task Title",
  "content": "Task Content",
  "desc": "Task Description",
  "isAllDay": true,
  "startDate": "2019-11-13T03:00:00+0000",
  "dueDate": "2019-11-14T03:00:00+0000",
  "timeZone": "America/Los_Angeles",
  "repeatFlag": "RRULE:FREQ=DAILY;INTERVAL=1",
  "reminders": ["TRIGGER:P0DT9H0M0S", "TRIGGER:PT0S"],
  "tags": ["work", "urgent"],
  "priority": 1,
  "status": 0,
  "items": [{"id": "6435074647fd2e6387145f20", "status": 0, "title": "Item Title"}]
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, err := cmd.Flags().GetString("project")
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.GetTaskWithResponse(cmd.Context(), project, args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		task, err := taskCrudDecodeTask(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			taskCrudPrintTask(w, task)
		})
	},
}

var taskCrudCreateCmd = &cobra.Command{
	Use:   "create --title TITLE --project PROJECT_ID",
	Short: "Create a task",
	Args:  cobra.NoArgs,
	Example: `  ticktick task create --title "Buy milk" --project 6226ff9877acee87727f6bca --due 2026-01-03 --tag shopping

Example API response:
{
  "id": "63b7bebb91c0a5474805fcd4",
  "projectId": "6226ff9877acee87727f6bca",
  "title": "Task Title",
  "content": "Task Content",
  "desc": "Task Description",
  "isAllDay": true,
  "startDate": "2019-11-13T03:00:00+0000",
  "dueDate": "2019-11-14T03:00:00+0000",
  "timeZone": "America/Los_Angeles",
  "reminders": ["TRIGGER:PT0S"],
  "tags": ["work", "urgent"],
  "repeatFlag": "RRULE:FREQ=DAILY;INTERVAL=1",
  "priority": 1,
  "status": 0,
  "items": [{"id": "6435074647fd2e6387145f20", "status": 1, "title": "Subtask Title"}]
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		f := taskCrudCreateFlags
		req := api.TaskCreateRequest{Title: f.title, ProjectId: f.project}
		if f.data != "" {
			if err := json.Unmarshal([]byte(f.data), &req); err != nil {
				return fmt.Errorf("invalid --data JSON: %w", err)
			}
		}
		if err := f.applyCreate(cmd, &req); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.CreateTaskWithResponse(cmd.Context(), req)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		task, err := taskCrudDecodeTask(resp.Body)
		if err != nil {
			return err
		}
		if task.Title == nil {
			task.Title = Ptr(req.Title)
		}
		if task.ProjectId == nil {
			task.ProjectId = Ptr(req.ProjectId)
		}
		return Render(resp.Body, func(w io.Writer) {
			fmt.Fprintf(w, "Created task %s: %s\n", taskCrudID(Str(task.Id)), Str(task.Title))
			taskCrudPrintTaskDetails(w, task)
		})
	},
}

var taskCrudUpdateCmd = &cobra.Command{
	Use:   "update TASK_ID --project PROJECT_ID",
	Short: "Update a task (only the flags you pass are changed)",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick task update 63b7bebb91c0a5474805fcd4 --project 6226ff9877acee87727f6bca --title "New title" --priority 5

Example API response:
{
  "id": "63b7bebb91c0a5474805fcd4",
  "projectId": "6226ff9877acee87727f6bca",
  "title": "Task Title",
  "content": "Task Content",
  "desc": "Task Description",
  "isAllDay": true,
  "startDate": "2019-11-13T03:00:00+0000",
  "dueDate": "2019-11-14T03:00:00+0000",
  "timeZone": "America/Los_Angeles",
  "tags": ["work", "urgent"],
  "priority": 1,
  "status": 0,
  "kind": "CHECKLIST"
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		f := taskCrudUpdateFlags
		req := api.TaskUpdateRequest{Id: args[0], ProjectId: f.project}
		if f.data != "" {
			if err := json.Unmarshal([]byte(f.data), &req); err != nil {
				return fmt.Errorf("invalid --data JSON: %w", err)
			}
		}
		// The path id always wins so the body and URL cannot diverge.
		req.Id = args[0]
		if err := f.applyUpdate(cmd, &req); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.UpdateTaskWithResponse(cmd.Context(), args[0], req)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		task, err := taskCrudDecodeTask(resp.Body)
		if err != nil {
			return err
		}
		if task.Id == nil {
			task.Id = Ptr(args[0])
		}
		if task.Title == nil {
			task.Title = req.Title
		}
		return Render(resp.Body, func(w io.Writer) {
			fmt.Fprintf(w, "Updated task %s: %s\n", taskCrudID(Str(task.Id)), Str(task.Title))
			taskCrudPrintTaskDetails(w, task)
		})
	},
}

var taskCrudCompleteCmd = &cobra.Command{
	Use:   "complete TASK_ID --project PROJECT_ID",
	Short: "Mark a task as completed",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick task complete 63b7bebb91c0a5474805fcd4 --project 6226ff9877acee87727f6bca

Example API response: (empty body, HTTP 200)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, err := cmd.Flags().GetString("project")
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.CompleteTaskWithResponse(cmd.Context(), project, args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return RenderOK("Task completed.")
	},
}

var taskCrudDeleteCmd = &cobra.Command{
	Use:   "delete TASK_ID --project PROJECT_ID",
	Short: "Delete a task",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick task delete 63b7bebb91c0a5474805fcd4 --project 6226ff9877acee87727f6bca

Example API response: (empty body, HTTP 200)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, err := cmd.Flags().GetString("project")
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.DeleteTaskWithResponse(cmd.Context(), project, args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return RenderOK("Task deleted.")
	},
}

// taskCrudID falls back to a placeholder when the API returns no id.
func taskCrudID(id string) string {
	if id == "" {
		return "(unknown)"
	}
	return id
}

// Shared flag value holders (registered in init).
var (
	taskCrudCreateFlags = &taskCrudFlags{}
	taskCrudUpdateFlags = &taskCrudFlags{}
)

func init() {
	taskCmd.AddCommand(taskCrudGetCmd, taskCrudCreateCmd, taskCrudUpdateCmd,
		taskCrudCompleteCmd, taskCrudDeleteCmd)

	taskCrudGetCmd.Flags().String("project", "", "project id (required)")
	_ = taskCrudGetCmd.MarkFlagRequired("project")

	taskCrudRegisterFlags(taskCrudCreateCmd, taskCrudCreateFlags)
	_ = taskCrudCreateCmd.MarkFlagRequired("title")
	_ = taskCrudCreateCmd.MarkFlagRequired("project")

	taskCrudRegisterFlags(taskCrudUpdateCmd, taskCrudUpdateFlags)
	_ = taskCrudUpdateCmd.MarkFlagRequired("project")

	taskCrudCompleteCmd.Flags().String("project", "", "project id (required)")
	_ = taskCrudCompleteCmd.MarkFlagRequired("project")

	taskCrudDeleteCmd.Flags().String("project", "", "project id (required)")
	_ = taskCrudDeleteCmd.MarkFlagRequired("project")
}
