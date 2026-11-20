package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// Flags for the project commands. Local flags are declared package-level
// (matching the other command files) and wired in init().
var (
	projectListOffset int
	projectListLimit  int

	projectCreateName      string
	projectCreateColor     string
	projectCreateSortOrder int64
	projectCreateViewMode  string
	projectCreateKind      string

	projectUpdateName      string
	projectUpdateColor     string
	projectUpdateSortOrder int64
	projectUpdateViewMode  string
	projectUpdateKind      string
)

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects",
	Args:  cobra.NoArgs,
	Example: `  ticktick project list --limit 10
  ticktick project list --offset 20 --limit 10 --json

Example API response:
[{
"id": "6226ff9877acee87727f6bca",
"name": "project name",
"color": "#F18181",
"closed": false,
"groupId": "6436176a47fd2e05f26ef56e",
"viewMode": "list",
"permission": "write",
"kind": "TASK"
}]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		params := &api.ListProjectsParams{}
		any := false
		if cmd.Flags().Changed("offset") {
			params.Offset = Ptr(int32(projectListOffset))
			any = true
		}
		if cmd.Flags().Changed("limit") {
			params.Limit = Ptr(int32(projectListLimit))
			any = true
		}
		var p *api.ListProjectsParams
		if any {
			p = params
		}
		resp, err := c.ListProjectsWithResponse(cmd.Context(), p)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			projects := []api.Project{}
			if resp.JSON200 != nil {
				projects = *resp.JSON200
			}
			rows := make([][]string, 0, len(projects))
			for _, pr := range projects {
				rows = append(rows, []string{
					Str(pr.Id),
					Str(pr.Name),
					projectKindStr(pr.Kind),
					projectViewModeStr(pr.ViewMode),
					strconv.FormatBool(BoolVal(pr.Closed)),
					Str(pr.GroupId),
				})
			}
			Table(w, []string{"ID", "NAME", "KIND", "VIEW", "CLOSED", "GROUP"}, rows)
		})
	},
}

var projectGetCmd = &cobra.Command{
	Use:   "get PROJECT_ID",
	Short: "Get one project by ID",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick project get 6226ff9877acee87727f6bca
  ticktick project get 6226ff9877acee87727f6bca --json

Example API response:
{
	"id": "6226ff9877acee87727f6bca",
	"name": "project name",
	"color": "#F18181",
	"closed": false,
	"groupId": "6436176a47fd2e05f26ef56e",
	"viewMode": "list",
	"kind": "TASK"
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.GetProjectWithResponse(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			p := api.Project{}
			if resp.JSON200 != nil {
				p = *resp.JSON200
			}
			printProjectKV(w, p)
		})
	},
}

var projectDataCmd = &cobra.Command{
	Use:   "data PROJECT_ID",
	Short: "Get a project together with its undone tasks and columns",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick project data 6226ff9877acee87727f6bca
  ticktick project data 6226ff9877acee87727f6bca --json

Example API response:
{
"project": {
	"id": "6226ff9877acee87727f6bca",
	"name": "project name",
	"color": "#F18181",
	"closed": false,
	"groupId": "6436176a47fd2e05f26ef56e",
	"viewMode": "list",
	"kind": "TASK"
},
"tasks": [{
	"id": "6247ee29630c800f064fd145",
	"projectId": "6226ff9877acee87727f6bca",
	"title": "Task Title",
	"status": 0,
	"priority": 1,
	"dueDate": "2019-11-14T03:00:00+0000"
}],
"columns": [{
	"id": "6226ff9e76e5fc39f2862d1b",
	"projectId": "6226ff9877acee87727f6bca",
	"name": "Column Name",
	"sortOrder": 0
}]
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.GetProjectDataWithResponse(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			data := api.ProjectData{}
			if resp.JSON200 != nil {
				data = *resp.JSON200
			}
			project := api.Project{}
			if data.Project != nil {
				project = *data.Project
			}
			printProjectKV(w, project)

			tasks := []api.Task{}
			if data.Tasks != nil {
				tasks = *data.Tasks
			}
			fmt.Fprintln(w, "Tasks (undone):")
			PrintTaskTable(w, tasks)

			columns := []api.Column{}
			if data.Columns != nil {
				columns = *data.Columns
			}
			fmt.Fprintln(w, "Columns:")
			rows := make([][]string, 0, len(columns))
			for _, col := range columns {
				rows = append(rows, []string{
					Str(col.Id),
					Str(col.Name),
					strconv.FormatInt(I64(col.SortOrder), 10),
				})
			}
			Table(w, []string{"ID", "NAME", "SORTORDER"}, rows)
		})
	},
}

var projectMembersCmd = &cobra.Command{
	Use:   "members PROJECT_ID",
	Short: "List the members of a project",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick project members 6226ff9877acee87727f6bca
  ticktick project members 6226ff9877acee87727f6bca --json

Example API response:
[
  {
    "username": "member@example.com",
    "displayName": "Project member",
    "self": false
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.GetProjectMembersWithResponse(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			members := []api.ProjectMember{}
			if resp.JSON200 != nil {
				members = *resp.JSON200
			}
			rows := make([][]string, 0, len(members))
			for _, m := range members {
				rows = append(rows, []string{
					Str(m.Username),
					Str(m.DisplayName),
					strconv.FormatBool(BoolVal(m.Self)),
				})
			}
			Table(w, []string{"USERNAME", "DISPLAY NAME", "SELF"}, rows)
		})
	},
}

var projectCreateCmd = &cobra.Command{
	Use:   "create --name NAME",
	Short: "Create a project",
	Args:  cobra.NoArgs,
	Example: `  ticktick project create --name "project name" --color "#F18181" --sort-order 0 --view-mode list --kind TASK
  ticktick project create --name "project name" --json

Example API response:
{
"id": "6226ff9877acee87727f6bca",
"name": "project name",
"color": "#F18181",
"sortOrder": 0,
"viewMode": "list",
"kind": "TASK"
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		req := api.ProjectCreateRequest{Name: projectCreateName}
		if cmd.Flags().Changed("color") {
			req.Color = Ptr(projectCreateColor)
		}
		if cmd.Flags().Changed("sort-order") {
			req.SortOrder = Ptr(projectCreateSortOrder)
		}
		if cmd.Flags().Changed("view-mode") {
			req.ViewMode = Ptr(api.ProjectCreateRequestViewMode(projectCreateViewMode))
		}
		if cmd.Flags().Changed("kind") {
			req.Kind = Ptr(api.ProjectCreateRequestKind(projectCreateKind))
		}
		resp, err := c.CreateProjectWithResponse(cmd.Context(), req)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			id, name := "", projectCreateName
			if resp.JSON200 != nil {
				id = Str(resp.JSON200.Id)
				if n := Str(resp.JSON200.Name); n != "" {
					name = n
				}
			}
			fmt.Fprintf(w, "Created project %s: %s\n", id, name)
		})
	},
}

var projectUpdateCmd = &cobra.Command{
	Use:   "update PROJECT_ID",
	Short: "Update a project (only passed flags are sent)",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick project update 6226ff9877acee87727f6bca --name "Project Name" --color "#F18181" --view-mode list
  ticktick project update 6226ff9877acee87727f6bca --name "Project Name" --json

Example API response:
{
"id": "6226ff9877acee87727f6bca",
"name": "Project Name",
"color": "#F18181",
"sortOrder": 0,
"viewMode": "list",
"kind": "TASK"
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		req := api.ProjectUpdateRequest{}
		any := false
		if cmd.Flags().Changed("name") {
			req.Name = Ptr(projectUpdateName)
			any = true
		}
		if cmd.Flags().Changed("color") {
			req.Color = Ptr(projectUpdateColor)
			any = true
		}
		if cmd.Flags().Changed("sort-order") {
			req.SortOrder = Ptr(projectUpdateSortOrder)
			any = true
		}
		if cmd.Flags().Changed("view-mode") {
			req.ViewMode = Ptr(api.ProjectUpdateRequestViewMode(projectUpdateViewMode))
			any = true
		}
		if cmd.Flags().Changed("kind") {
			req.Kind = Ptr(api.ProjectUpdateRequestKind(projectUpdateKind))
			any = true
		}
		if !any {
			return errors.New("nothing to update: pass at least one of --name, --color, --sort-order, --view-mode, --kind")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.UpdateProjectWithResponse(cmd.Context(), args[0], req)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			id, name := args[0], ""
			if resp.JSON200 != nil {
				if v := Str(resp.JSON200.Id); v != "" {
					id = v
				}
				name = Str(resp.JSON200.Name)
			}
			fmt.Fprintf(w, "Updated project %s: %s\n", id, name)
		})
	},
}

var projectDeleteCmd = &cobra.Command{
	Use:   "delete PROJECT_ID",
	Short: "Delete a project",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick project delete 6226ff9877acee87727f6bca
  ticktick project delete 6226ff9877acee87727f6bca --json

Example API response:
(empty body, HTTP 200)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.DeleteProjectWithResponse(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return RenderOK("Project deleted.")
	},
}

func init() {
	projectListCmd.Flags().IntVar(&projectListOffset, "offset", 0,
		"zero-based result offset")
	projectListCmd.Flags().IntVar(&projectListLimit, "limit", 0,
		"maximum number of projects to return (default 200)")

	projectCreateCmd.Flags().StringVar(&projectCreateName, "name", "",
		"name of the project (required)")
	projectCreateCmd.Flags().StringVar(&projectCreateColor, "color", "",
		"project color, e.g. #F18181")
	projectCreateCmd.Flags().Int64Var(&projectCreateSortOrder, "sort-order", 0,
		"sort order value of the project")
	projectCreateCmd.Flags().StringVar(&projectCreateViewMode, "view-mode", "",
		"view mode: list|kanban|timeline")
	projectCreateCmd.Flags().StringVar(&projectCreateKind, "kind", "",
		"project kind: TASK|NOTE")
	_ = projectCreateCmd.MarkFlagRequired("name")

	projectUpdateCmd.Flags().StringVar(&projectUpdateName, "name", "",
		"new name of the project")
	projectUpdateCmd.Flags().StringVar(&projectUpdateColor, "color", "",
		"new project color")
	projectUpdateCmd.Flags().Int64Var(&projectUpdateSortOrder, "sort-order", 0,
		"new sort order value")
	projectUpdateCmd.Flags().StringVar(&projectUpdateViewMode, "view-mode", "",
		"new view mode: list|kanban|timeline")
	projectUpdateCmd.Flags().StringVar(&projectUpdateKind, "kind", "",
		"new project kind: TASK|NOTE")

	projectCmd.AddCommand(projectListCmd, projectGetCmd, projectDataCmd,
		projectMembersCmd, projectCreateCmd, projectUpdateCmd, projectDeleteCmd)
}

// printProjectKV renders the standard project key/value block.
func printProjectKV(w io.Writer, p api.Project) {
	KV(w,
		[2]string{"ID", Str(p.Id)},
		[2]string{"Name", Str(p.Name)},
		[2]string{"Color", Str(p.Color)},
		[2]string{"Kind", projectKindStr(p.Kind)},
		[2]string{"View mode", projectViewModeStr(p.ViewMode)},
		[2]string{"Closed", projectClosedStr(p.Closed)},
		[2]string{"Group", Str(p.GroupId)},
		[2]string{"Sort order", projectSortOrderStr(p.SortOrder)},
		[2]string{"Permission", projectPermissionStr(p.Permission)},
	)
}

// projectKindStr dereferences Project.Kind ("" when nil).
func projectKindStr(p *api.ProjectKind) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// projectViewModeStr dereferences Project.ViewMode ("" when nil).
func projectViewModeStr(p *api.ProjectViewMode) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// projectPermissionStr dereferences Project.Permission ("" when nil).
func projectPermissionStr(p *api.ProjectPermission) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// projectClosedStr renders Project.Closed ("" when nil).
func projectClosedStr(p *bool) string {
	if p == nil {
		return ""
	}
	return strconv.FormatBool(*p)
}

// projectSortOrderStr renders Project.SortOrder ("" when nil).
func projectSortOrderStr(p *int64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(*p, 10)
}
