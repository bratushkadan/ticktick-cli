package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// This file implements `ticktick group` — project groups:
//
//	group list
//	group create --name NAME
//	group update GROUP_ID --name NAME
//	group delete GROUP_ID
//
// All optional request fields are pointers in the generated api package;
// the helpers in ptr.go (Ptr, Str, I64, BoolVal, ...) are used throughout.

const groupNameMaxLen = 64

// Flag values for the group commands. They are package-level because cobra
// only binds them once, at init().
var (
	groupCreateName string
	groupUpdateName string
)

// groupValidateName trims and validates a project group name.
// It returns the trimmed name.
func groupValidateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("--name must not be empty")
	}
	if utf8.RuneCountInString(name) > groupNameMaxLen {
		return "", fmt.Errorf("--name must be at most %d characters", groupNameMaxLen)
	}
	return name, nil
}

// groupDecodeGroup decodes one project group. An empty body (HTTP 201 with
// no content) yields the zero value rather than an error.
func groupDecodeGroup(body []byte) (api.OpenProjectGroup, error) {
	var g api.OpenProjectGroup
	if len(strings.TrimSpace(string(body))) == 0 {
		return g, nil
	}
	if err := json.Unmarshal(body, &g); err != nil {
		return g, fmt.Errorf("decode project group: %w", err)
	}
	return g, nil
}

// groupDecodeList decodes the project group array.
func groupDecodeList(body []byte) ([]api.OpenProjectGroup, error) {
	var groups []api.OpenProjectGroup
	if len(strings.TrimSpace(string(body))) == 0 {
		return groups, nil
	}
	if err := json.Unmarshal(body, &groups); err != nil {
		return nil, fmt.Errorf("decode project groups: %w", err)
	}
	return groups, nil
}

// groupViewMode dereferences the optional view mode.
func groupViewMode(g api.OpenProjectGroup) string {
	if g.ViewMode == nil {
		return ""
	}
	return string(*g.ViewMode)
}

// groupPrintCreated prints "Created group <id>: <name>", falling back to the
// response name and then to the --name flag.
func groupPrintCreated(w io.Writer, g api.OpenProjectGroup, fallbackName string) {
	name := Str(g.Name)
	if name == "" {
		name = fallbackName
	}
	fmt.Fprintf(w, "Created group %s: %s\n", Str(g.Id), name)
}

// groupPrintUpdated prints "Updated group <id>: <name>".
func groupPrintUpdated(w io.Writer, g api.OpenProjectGroup, fallbackName string) {
	name := Str(g.Name)
	if name == "" {
		name = fallbackName
	}
	fmt.Fprintf(w, "Updated group %s: %s\n", Str(g.Id), name)
}

var groupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all project groups",
	Args:  cobra.NoArgs,
	Example: `  ticktick group list
  ticktick group list --json

Example API response:
[
  {
    "id": "group-1",
    "name": "Group Name",
    "sortOrder": 0,
    "showAll": true,
    "viewMode": "list"
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListProjectGroupsWithResponse(cmd.Context())
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		groups, err := groupDecodeList(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			rows := make([][]string, 0, len(groups))
			for _, g := range groups {
				rows = append(rows, []string{
					Str(g.Id),
					Str(g.Name),
					fmt.Sprintf("%d", I64(g.SortOrder)),
					fmt.Sprintf("%t", BoolVal(g.ShowAll)),
					groupViewMode(g),
				})
			}
			Table(w, []string{"ID", "NAME", "SORTORDER", "SHOWALL", "VIEWMODE"}, rows)
		})
	},
}

var groupCreateCmd = &cobra.Command{
	Use:   "create --name NAME",
	Short: "Create a project group (name max 64 chars)",
	Args:  cobra.NoArgs,
	Example: `  ticktick group create --name "Group Name"
  ticktick group create --name "Group Name" --json

Example API response:
{
  "id": "group-1",
  "name": "Group Name",
  "sortOrder": 0
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := groupValidateName(groupCreateName)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.CreateProjectGroupWithResponse(cmd.Context(),
			api.ProjectGroupCreateRequest{Name: name})
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		g, err := groupDecodeGroup(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			groupPrintCreated(w, g, name)
		})
	},
}

var groupUpdateCmd = &cobra.Command{
	Use:   "update GROUP_ID --name NAME",
	Short: "Update a project group name (max 64 chars)",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick group update group-1 --name "New Group Name"
  ticktick group update group-1 --name "New Group Name" --json

Example API response:
{
  "id": "group-1",
  "name": "New Group Name",
  "sortOrder": 0
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := groupValidateName(groupUpdateName)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.UpdateProjectGroupWithResponse(cmd.Context(), args[0],
			api.ProjectGroupUpdateRequest{Name: Ptr(name)})
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		g, err := groupDecodeGroup(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			groupPrintUpdated(w, g, name)
		})
	},
}

var groupDeleteCmd = &cobra.Command{
	Use:   "delete GROUP_ID",
	Short: "Delete a project group",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick group delete group-1
  ticktick group delete group-1 --json

Example API response:
(empty body, HTTP 200)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.DeleteProjectGroupWithResponse(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return RenderOK("Project group deleted.")
	},
}

func init() {
	groupCreateCmd.Flags().StringVar(&groupCreateName, "name", "",
		"project group name (required, max 64 chars)")
	_ = groupCreateCmd.MarkFlagRequired("name")

	groupUpdateCmd.Flags().StringVar(&groupUpdateName, "name", "",
		"new project group name (required, max 64 chars)")
	_ = groupUpdateCmd.MarkFlagRequired("name")

	groupCmd.AddCommand(groupListCmd, groupCreateCmd, groupUpdateCmd, groupDeleteCmd)
}
