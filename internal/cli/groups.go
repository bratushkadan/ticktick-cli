package cli

import "github.com/spf13/cobra"

// Group parent commands. Command files register their leaves onto these
// via init() so parallel contributors never edit shared files.
var (
	taskCmd = &cobra.Command{
		Use:   "task",
		Short: "Tasks: CRUD, completion, move, batch, assign, filters, search, comments",
	}
	projectCmd = &cobra.Command{
		Use:   "project",
		Short: "Projects: list, get, data (tasks+columns), members, create, update, delete",
	}
	groupCmd = &cobra.Command{
		Use:   "group",
		Short: "Project groups: list, create, update, delete",
	}
	columnCmd = &cobra.Command{
		Use:   "column",
		Short: "Kanban columns: list, create, update",
	}
	tagCmd = &cobra.Command{
		Use:   "tag",
		Short: "Tags: list, create",
	}
	focusCmd = &cobra.Command{
		Use:   "focus",
		Short: "Focus sessions (pomodoro/timing): get, list by range, create, delete",
	}
	countdownCmd = &cobra.Command{
		Use:   "countdown",
		Short: "Countdowns: list",
	}
	habitCmd = &cobra.Command{
		Use:   "habit",
		Short: "Habits: list, get, sections, create, update, check-in, check-ins history",
	}
	preferenceCmd = &cobra.Command{
		Use:   "preference",
		Short: "User preferences (time zone)",
	}
)

func init() {
	rootCmd.AddCommand(taskCmd, projectCmd, groupCmd, columnCmd, tagCmd,
		focusCmd, countdownCmd, habitCmd, preferenceCmd)
}