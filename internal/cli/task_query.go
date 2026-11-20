package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// Query command state. Every list-valued flag is a plain string so that a
// repeated run cannot accumulate values in the persistent cobra FlagSet;
// the comma-separated form is parsed by taskQuerySplitCSV.
var (
	// task completed
	taskQueryCompletedProjects string
	taskQueryCompletedStart    string
	taskQueryCompletedEnd      string

	// task filter
	taskQueryFilterProjects string
	taskQueryFilterStart    string
	taskQueryFilterEnd      string
	taskQueryFilterPriority string
	taskQueryFilterTags     string
	taskQueryFilterKind     string
	taskQueryFilterStatus   string

	// task undone
	taskQueryUndoneProjects string
	taskQueryUndoneIDs      string
	taskQueryUndoneStart    string
	taskQueryUndoneEnd      string

	// task search
	taskQuerySearchProjects string
	taskQuerySearchTags     string
	taskQuerySearchStatus   string
	taskQuerySearchDueFrom  string
	taskQuerySearchDueTo    string
)

// taskQuerySplitCSV splits "a, b,c" into ["a","b","c"]; empty input
// returns nil so callers can leave optional body fields unset.
func taskQuerySplitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// taskQueryInt32List parses a comma-separated integer list
// (e.g. "0,3" or "0" for priority/status filters).
func taskQueryInt32List(name, s string) ([]int32, error) {
	items := taskQuerySplitCSV(s)
	if items == nil {
		return nil, nil
	}
	out := make([]int32, 0, len(items))
	for _, it := range items {
		n, err := strconv.ParseInt(it, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid value %q for %s: use comma-separated integers", it, name)
		}
		out = append(out, int32(n))
	}
	return out, nil
}

// taskQueryNormalizedPtr normalizes a time flag and returns a pointer, or
// nil when the flag is empty.
func taskQueryNormalizedPtr(name, value string) (*string, error) {
	v, err := NormalizeTime(value)
	if err != nil {
		return nil, fmt.Errorf("--%s: %w", name, err)
	}
	if v == "" {
		return nil, nil
	}
	return Ptr(v), nil
}

var taskQueryCompletedCmd = &cobra.Command{
	Use:   "completed",
	Short: "List completed tasks in a completedTime range (max 200)",
	Long: `Retrieves at most 200 tasks marked as completed within the given projects
and completedTime range (startDate <= completedTime <= endDate).`,
	Args: cobra.NoArgs,
	Example: `  ticktick task completed --projects 69a850f41c20d2030e148fdf --start 2026-03-01 --end 2026-03-05
  ticktick task completed --projects p1,p2 --start "2026-03-01 00:00" --end 2026-03-05 --json

Example API response:
[
  {
    "id": "69a850f8b9061f374d54a046",
    "projectId": "69a850f41c20d2030e148fdf",
    "sortOrder": -1099511627776,
    "title": "update",
    "content": "",
    "timeZone": "America/Los_Angeles",
    "isAllDay": false,
    "priority": 0,
    "completedTime": "2026-03-04T23:58:20.000+0000",
    "status": 2,
    "etag": "t3kc5m5f",
    "kind": "TEXT"
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body := api.CompletedTasksFilter{}
		if v := taskQuerySplitCSV(taskQueryCompletedProjects); v != nil {
			body.ProjectIds = Ptr(v)
		}
		start, err := taskQueryNormalizedPtr("start", taskQueryCompletedStart)
		if err != nil {
			return err
		}
		body.StartDate = start
		end, err := taskQueryNormalizedPtr("end", taskQueryCompletedEnd)
		if err != nil {
			return err
		}
		body.EndDate = end

		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListCompletedTasksWithResponse(cmd.Context(), body)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			if resp.JSON200 != nil {
				PrintTaskTable(w, *resp.JSON200)
			}
		})
	},
}

var taskQueryFilterCmd = &cobra.Command{
	Use:   "filter",
	Short: "Filter tasks by project, startDate range, priority, tags, kind, status (max 200)",
	Long: `Retrieves at most 200 tasks matching advanced criteria. --start/--end filter
the task startDate; --priority accepts 0 (none), 1 (low), 3 (medium), 5 (high);
--status accepts -1 (abandoned), 0 (open), 2 (completed).

All list flags are comma-separated, e.g. --priority 0,3,5 --tags work,urgent.`,
	Args: cobra.NoArgs,
	Example: `  ticktick task filter --projects 69a850f41c20d2030e148fdf --start 2026-03-01 --end 2026-03-06 --priority 0 --tags urgent --status 0
  ticktick task filter --priority 0,3 --kind TEXT,CHECKLIST --tags work,urgent --json

Example API response:
[
  {
    "id": "69a85785b9061f3c217e9de6",
    "projectId": "69a850f41c20d2030e148fdf",
    "sortOrder": -2199023255552,
    "title": "task1",
    "content": "",
    "desc": "",
    "startDate": "2026-03-05T00:00:00.000+0000",
    "dueDate": "2026-03-05T00:00:00.000+0000",
    "timeZone": "America/Los_Angeles",
    "isAllDay": false,
    "priority": 0,
    "status": 0,
    "tags": [
      "tag"
    ],
    "etag": "cic6e3cg",
    "kind": "TEXT"
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body := api.TaskFilter{}
		if v := taskQuerySplitCSV(taskQueryFilterProjects); v != nil {
			body.ProjectIds = Ptr(v)
		}
		start, err := taskQueryNormalizedPtr("start", taskQueryFilterStart)
		if err != nil {
			return err
		}
		body.StartDate = start
		end, err := taskQueryNormalizedPtr("end", taskQueryFilterEnd)
		if err != nil {
			return err
		}
		body.EndDate = end

		priority, err := taskQueryInt32List("--priority", taskQueryFilterPriority)
		if err != nil {
			return err
		}
		if priority != nil {
			body.Priority = Ptr(priority)
		}
		status, err := taskQueryInt32List("--status", taskQueryFilterStatus)
		if err != nil {
			return err
		}
		if status != nil {
			body.Status = Ptr(status)
		}
		if v := taskQuerySplitCSV(taskQueryFilterTags); v != nil {
			body.Tag = Ptr(v)
		}
		if v := taskQuerySplitCSV(taskQueryFilterKind); v != nil {
			body.Kind = Ptr(v)
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.FilterTasksWithResponse(cmd.Context(), body)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			if resp.JSON200 != nil {
				PrintTaskTable(w, *resp.JSON200)
			}
		})
	},
}

var taskQueryUndoneCmd = &cobra.Command{
	Use:   "undone",
	Short: "List undone tasks in a required date range of at most 14 days",
	Long: `Retrieves undone tasks within a date range of at most 14 days.
--start and --end are required; an invalid range returns an empty array.
--projects accepts project IDs or "inbox" for the inbox project.`,
	Args: cobra.NoArgs,
	Example: `  ticktick task undone --start 2026-07-01 --end 2026-07-14
  ticktick task undone --start "2026-07-01 00:00" --end "2026-07-14 23:59:59" --projects inbox --json

Example API response:
[
  {
    "id": "69a85785b9061f3c217e9de6",
    "projectId": "69a850f41c20d2030e148fdf",
    "sortOrder": -2199023255552,
    "title": "task1",
    "content": "",
    "desc": "",
    "startDate": "2026-03-05T00:00:00.000+0000",
    "dueDate": "2026-03-05T00:00:00.000+0000",
    "timeZone": "America/Los_Angeles",
    "isAllDay": false,
    "priority": 0,
    "status": 0,
    "tags": [
      "tag"
    ],
    "etag": "cic6e3cg",
    "kind": "TEXT"
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(taskQueryUndoneStart) == "" || strings.TrimSpace(taskQueryUndoneEnd) == "" {
			return fmt.Errorf("--start and --end are required (date range of at most 14 days)")
		}
		start, err := taskQueryNormalizedPtr("start", taskQueryUndoneStart)
		if err != nil {
			return err
		}
		end, err := taskQueryNormalizedPtr("end", taskQueryUndoneEnd)
		if err != nil {
			return err
		}
		body := api.UndoneTasksFilter{StartDate: *start, EndDate: *end}
		if v := taskQuerySplitCSV(taskQueryUndoneProjects); v != nil {
			body.ProjectIds = Ptr(v)
		}
		if v := taskQuerySplitCSV(taskQueryUndoneIDs); v != nil {
			body.TaskIds = Ptr(v)
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListUndoneTasksWithResponse(cmd.Context(), body)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			if resp.JSON200 != nil {
				PrintTaskTable(w, *resp.JSON200)
			}
		})
	},
}

var taskQuerySearchCmd = &cobra.Command{
	Use:   "search KEYWORDS",
	Short: "Search tasks by keywords with optional project, tag, status and due-date filters",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick task search "quarterly report"
  ticktick task search "quarterly report" --projects p1 --tags work --status 0 --due-from 2026-07-01 --due-to 2026-07-31 --json

Example API response:
[
  {
    "id": "69a85785b9061f3c217e9de6",
    "projectId": "69a850f41c20d2030e148fdf",
    "sortOrder": -2199023255552,
    "title": "task1",
    "content": "",
    "desc": "",
    "startDate": "2026-03-05T00:00:00.000+0000",
    "dueDate": "2026-03-05T00:00:00.000+0000",
    "timeZone": "America/Los_Angeles",
    "isAllDay": false,
    "priority": 0,
    "status": 0,
    "tags": [
      "tag"
    ],
    "etag": "cic6e3cg",
    "kind": "TEXT"
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body := api.TaskSearchFilter{Keywords: Ptr(args[0])}
		if v := taskQuerySplitCSV(taskQuerySearchProjects); v != nil {
			body.ProjectIds = Ptr(v)
		}
		if v := taskQuerySplitCSV(taskQuerySearchTags); v != nil {
			body.Tags = Ptr(v)
		}
		status, err := taskQueryInt32List("--status", taskQuerySearchStatus)
		if err != nil {
			return err
		}
		if status != nil {
			body.Status = Ptr(status)
		}
		dueFrom, err := taskQueryNormalizedPtr("due-from", taskQuerySearchDueFrom)
		if err != nil {
			return err
		}
		body.DueFrom = dueFrom
		dueTo, err := taskQueryNormalizedPtr("due-to", taskQuerySearchDueTo)
		if err != nil {
			return err
		}
		body.DueTo = dueTo

		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.SearchTasksWithResponse(cmd.Context(), body)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			if resp.JSON200 != nil {
				PrintTaskTable(w, *resp.JSON200)
			}
		})
	},
}

func init() {
	taskQueryCompletedCmd.Flags().StringVar(&taskQueryCompletedProjects, "projects", "", "project IDs, comma-separated")
	taskQueryCompletedCmd.Flags().StringVar(&taskQueryCompletedStart, "start", "", "range start; completedTime >= start (YYYY-MM-DD, \"YYYY-MM-DD HH:MM\", RFC3339)")
	taskQueryCompletedCmd.Flags().StringVar(&taskQueryCompletedEnd, "end", "", "range end; completedTime <= end (YYYY-MM-DD, \"YYYY-MM-DD HH:MM\", RFC3339)")

	taskQueryFilterCmd.Flags().StringVar(&taskQueryFilterProjects, "projects", "", "project IDs, comma-separated")
	taskQueryFilterCmd.Flags().StringVar(&taskQueryFilterStart, "start", "", "range start; task startDate >= start")
	taskQueryFilterCmd.Flags().StringVar(&taskQueryFilterEnd, "end", "", "range end; task startDate <= end")
	taskQueryFilterCmd.Flags().StringVar(&taskQueryFilterPriority, "priority", "", "priority levels, comma-separated: 0,1,3,5")
	taskQueryFilterCmd.Flags().StringVar(&taskQueryFilterTags, "tags", "", "tags the task must contain, comma-separated")
	taskQueryFilterCmd.Flags().StringVar(&taskQueryFilterKind, "kind", "", "task kinds, comma-separated: TEXT,NOTE,CHECKLIST")
	taskQueryFilterCmd.Flags().StringVar(&taskQueryFilterStatus, "status", "", "status codes, comma-separated: -1,0,2")

	taskQueryUndoneCmd.Flags().StringVar(&taskQueryUndoneStart, "start", "", "range start (required; range at most 14 days)")
	taskQueryUndoneCmd.Flags().StringVar(&taskQueryUndoneEnd, "end", "", "range end (required; range at most 14 days)")
	taskQueryUndoneCmd.Flags().StringVar(&taskQueryUndoneProjects, "projects", "", "project IDs (or \"inbox\"), comma-separated")
	taskQueryUndoneCmd.Flags().StringVar(&taskQueryUndoneIDs, "ids", "", "task IDs, comma-separated")

	taskQuerySearchCmd.Flags().StringVar(&taskQuerySearchProjects, "projects", "", "project IDs, comma-separated")
	taskQuerySearchCmd.Flags().StringVar(&taskQuerySearchTags, "tags", "", "tags to filter by, comma-separated")
	taskQuerySearchCmd.Flags().StringVar(&taskQuerySearchStatus, "status", "", "status codes, comma-separated: -1,0,2")
	taskQuerySearchCmd.Flags().StringVar(&taskQuerySearchDueFrom, "due-from", "", "include tasks due on or after this time")
	taskQuerySearchCmd.Flags().StringVar(&taskQuerySearchDueTo, "due-to", "", "include tasks due on or before this time")

	taskCmd.AddCommand(taskQueryCompletedCmd, taskQueryFilterCmd,
		taskQueryUndoneCmd, taskQuerySearchCmd)
}
