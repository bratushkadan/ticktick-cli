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

// This file implements `ticktick column` — kanban columns:
//
//	column list   --project PROJECT_ID
//	column create --project PROJECT_ID --name NAME
//	column update COLUMN_ID --project PROJECT_ID --name NAME
//
// All optional request fields are pointers in the generated api package;
// the helpers in ptr.go (Ptr, Str, I64, ...) are used throughout.

const columnNameMaxLen = 1000

// Flag values for the column commands.
var (
	columnListProject   string
	columnCreateProject string
	columnCreateName    string
	columnUpdateProject string
	columnUpdateName    string
)

// columnValidateName trims and validates a column name; returns the trimmed name.
func columnValidateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("--name must not be empty")
	}
	if utf8.RuneCountInString(name) > columnNameMaxLen {
		return "", fmt.Errorf("--name must be at most %d characters", columnNameMaxLen)
	}
	return name, nil
}

// columnDecodeColumn decodes one column; empty bodies yield the zero value.
func columnDecodeColumn(body []byte) (api.Column, error) {
	var col api.Column
	if len(strings.TrimSpace(string(body))) == 0 {
		return col, nil
	}
	if err := json.Unmarshal(body, &col); err != nil {
		return col, fmt.Errorf("decode column: %w", err)
	}
	return col, nil
}

// columnDecodeList decodes the column array.
func columnDecodeList(body []byte) ([]api.Column, error) {
	var cols []api.Column
	if len(strings.TrimSpace(string(body))) == 0 {
		return cols, nil
	}
	if err := json.Unmarshal(body, &cols); err != nil {
		return nil, fmt.Errorf("decode columns: %w", err)
	}
	return cols, nil
}

var columnListCmd = &cobra.Command{
	Use:   "list --project PROJECT_ID",
	Short: "List the kanban columns of a project",
	Args:  cobra.NoArgs,
	Example: `  ticktick column list --project project-1
  ticktick column list --project project-1 --json

Example API response:
[
  {
    "id": "column-1",
    "projectId": "project-1",
    "name": "Column Name",
    "sortOrder": 0
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListColumnsWithResponse(cmd.Context(), columnListProject)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		cols, err := columnDecodeList(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			rows := make([][]string, 0, len(cols))
			for _, col := range cols {
				rows = append(rows, []string{
					Str(col.Id),
					Str(col.Name),
					fmt.Sprintf("%d", I64(col.SortOrder)),
				})
			}
			Table(w, []string{"ID", "NAME", "SORTORDER"}, rows)
		})
	},
}

var columnCreateCmd = &cobra.Command{
	Use:   "create --project PROJECT_ID --name NAME",
	Short: "Create a kanban column (name max 1000 chars)",
	Args:  cobra.NoArgs,
	Example: `  ticktick column create --project project-1 --name "Column Name"
  ticktick column create --project project-1 --name "Column Name" --json

Example API response:
{
  "id": "column-1",
  "projectId": "project-1",
  "name": "Column Name",
  "sortOrder": 0
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := columnValidateName(columnCreateName)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.CreateColumnWithResponse(cmd.Context(), columnCreateProject,
			api.ColumnCreateRequest{Name: name})
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		col, err := columnDecodeColumn(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			name2 := Str(col.Name)
			if name2 == "" {
				name2 = name
			}
			fmt.Fprintf(w, "Created column %s: %s\n", Str(col.Id), name2)
		})
	},
}

var columnUpdateCmd = &cobra.Command{
	Use:   "update COLUMN_ID --project PROJECT_ID --name NAME",
	Short: "Update a kanban column name (max 1000 chars)",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick column update column-1 --project project-1 --name "New Column Name"
  ticktick column update column-1 --project project-1 --name "New Column Name" --json

Example API response:
{
  "id": "column-1",
  "projectId": "project-1",
  "name": "New Column Name",
  "sortOrder": 0
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := columnValidateName(columnUpdateName)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.UpdateColumnWithResponse(cmd.Context(), columnUpdateProject, args[0],
			api.ColumnUpdateRequest{Name: Ptr(name)})
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		col, err := columnDecodeColumn(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			name2 := Str(col.Name)
			if name2 == "" {
				name2 = name
			}
			fmt.Fprintf(w, "Updated column %s: %s\n", Str(col.Id), name2)
		})
	},
}

func init() {
	columnListCmd.Flags().StringVar(&columnListProject, "project", "",
		"project id (required)")
	_ = columnListCmd.MarkFlagRequired("project")

	columnCreateCmd.Flags().StringVar(&columnCreateProject, "project", "",
		"project id (required)")
	columnCreateCmd.Flags().StringVar(&columnCreateName, "name", "",
		"column name (required, max 1000 chars)")
	_ = columnCreateCmd.MarkFlagRequired("project")
	_ = columnCreateCmd.MarkFlagRequired("name")

	columnUpdateCmd.Flags().StringVar(&columnUpdateProject, "project", "",
		"project id (required)")
	columnUpdateCmd.Flags().StringVar(&columnUpdateName, "name", "",
		"new column name (required, max 1000 chars)")
	_ = columnUpdateCmd.MarkFlagRequired("project")
	_ = columnUpdateCmd.MarkFlagRequired("name")

	columnCmd.AddCommand(columnListCmd, columnCreateCmd, columnUpdateCmd)
}
