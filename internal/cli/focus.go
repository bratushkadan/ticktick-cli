package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// Flags for the focus command group.
var (
	focusGetTypeFlag int

	focusListFromFlag string
	focusListToFlag   string
	focusListTypeFlag int

	focusCreateTypeFlag     int
	focusCreateTaskFlag     string
	focusCreateNoteFlag     string
	focusCreateStartFlag    string
	focusCreateEndFlag      string
	focusCreatePauseFlag    int
	focusCreateDurationFlag int64
	focusCreateRelationFlag string
	focusCreateDataFlag     string

	focusDeleteTypeFlag int
)

// focusTypeName maps the API focus type code to a label.
func focusTypeName(t int32) string {
	switch t {
	case 0:
		return "pomodoro"
	case 1:
		return "timing"
	default:
		return fmt.Sprintf("type%d", t)
	}
}

// focusDecode returns the OpenFocus carried by a response, falling back to
// decoding the raw body when the generated JSON200 field is absent.
func focusDecode(respBody []byte, focus *api.OpenFocus) api.OpenFocus {
	if focus != nil {
		return *focus
	}
	var f api.OpenFocus
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &f)
	}
	return f
}

var focusGetCmd = &cobra.Command{
	Use:   "get FOCUS_ID",
	Short: "Get one focus session by id",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick focus get focus-1 --type 0
  ticktick focus get focus-1 --type 1 --json

Example API response:
{
  "id": "focus-1",
  "type": 0,
  "taskId": "task-1",
  "note": "Deep work",
  "status": 2,
  "startTime": "2026-04-07T09:00:00+0800",
  "endTime": "2026-04-07T09:25:00+0800",
  "pauseDuration": 0,
  "adjustTime": 0,
  "added": true,
  "createdTime": "2026-04-07T09:25:00+0800",
  "modifiedTime": "2026-04-07T09:25:00+0800",
  "etimestamp": 1712453100000,
  "etag": "focus-etag",
  "duration": 1500,
  "relationType": [0]
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.GetFocusWithResponse(cmd.Context(), args[0],
			&api.GetFocusParams{Type: int32(focusGetTypeFlag)})
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			f := focusDecode(resp.Body, resp.JSON200)
			KV(w,
				[2]string{"ID", Str(f.Id)},
				[2]string{"Type", focusTypeName(I32(f.Type))},
				[2]string{"Task", Str(f.TaskId)},
				[2]string{"Status", strconv.FormatInt(int64(I32(f.Status)), 10)},
				[2]string{"Start", FmtTime(Str(f.StartTime))},
				[2]string{"End", FmtTime(Str(f.EndTime))},
				[2]string{"Duration(s)", strconv.FormatInt(I64(f.Duration), 10)},
				[2]string{"Pause(s)", strconv.FormatInt(int64(I32(f.PauseDuration)), 10)},
				[2]string{"Note", Str(f.Note)},
			)
		})
	},
}

var focusListCmd = &cobra.Command{
	Use:   "list",
	Short: "List focus sessions in a time range (server clamps ranges over 30 days)",
	Args:  cobra.NoArgs,
	Example: `  ticktick focus list --from 2026-04-01 --to 2026-04-02 --type 1
  ticktick focus list --from 2026-04-01T00:00:00+0800 --to 2026-04-02T00:00:00+0800 --type 0 --json

Example API response:
[
  {
    "id": "focus-1",
    "type": 1,
    "note": "Timing",
    "startTime": "2026-04-01T09:00:00+0800",
    "endTime": "2026-04-01T10:30:00+0800",
    "duration": 5400
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		from, err := NormalizeTime(focusListFromFlag)
		if err != nil {
			return fmt.Errorf("--from: %w", err)
		}
		to, err := NormalizeTime(focusListToFlag)
		if err != nil {
			return fmt.Errorf("--to: %w", err)
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListFocusesWithResponse(cmd.Context(), &api.ListFocusesParams{
			From: from,
			To:   to,
			Type: int32(focusListTypeFlag),
		})
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			var focuses []api.OpenFocus
			if resp.JSON200 != nil {
				focuses = *resp.JSON200
			}
			rows := make([][]string, 0, len(focuses))
			for _, f := range focuses {
				rows = append(rows, []string{
					Str(f.Id),
					focusTypeName(I32(f.Type)),
					Str(f.TaskId),
					FmtTime(Str(f.StartTime)),
					FmtTime(Str(f.EndTime)),
					strconv.FormatInt(I64(f.Duration), 10),
					Str(f.Note),
				})
			}
			Table(w, []string{"ID", "TYPE", "TASK", "START", "END", "DURATION(s)", "NOTE"}, rows)
		})
	},
}

var focusCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a focus session",
	Args:  cobra.NoArgs,
	Example: `  ticktick focus create --type 0 --task task-1 --start 2026-04-07T09:00:00+0800 --end 2026-04-07T09:25:00+0800 --duration 1500
  ticktick focus create --type 1 --note "Timing" --start "2026-04-01 09:00" --pause 30 --relation 0,1 --json

Example API response:
{
  "id": "focus-1",
  "type": 0,
  "taskId": "task-1",
  "startTime": "2026-04-07T09:00:00+0800",
  "endTime": "2026-04-07T09:25:00+0800",
  "duration": 1500
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var req api.FocusCreateRequest
		if strings.TrimSpace(focusCreateDataFlag) != "" {
			if err := json.Unmarshal([]byte(focusCreateDataFlag), &req); err != nil {
				return fmt.Errorf("invalid --data JSON: %w", err)
			}
		}
		// Explicit flags override values coming from --data.
		if cmd.Flags().Changed("type") {
			req.Type = int32(focusCreateTypeFlag)
		}
		if cmd.Flags().Changed("task") {
			req.TaskId = Ptr(focusCreateTaskFlag)
		}
		if cmd.Flags().Changed("note") {
			req.Note = Ptr(focusCreateNoteFlag)
		}
		if cmd.Flags().Changed("start") {
			start, err := NormalizeTime(focusCreateStartFlag)
			if err != nil {
				return fmt.Errorf("--start: %w", err)
			}
			req.StartTime = Ptr(start)
		}
		if cmd.Flags().Changed("end") {
			end, err := NormalizeTime(focusCreateEndFlag)
			if err != nil {
				return fmt.Errorf("--end: %w", err)
			}
			req.EndTime = Ptr(end)
		}
		if cmd.Flags().Changed("pause") {
			req.PauseDuration = Ptr(int32(focusCreatePauseFlag))
		}
		if cmd.Flags().Changed("duration") {
			req.Duration = Ptr(focusCreateDurationFlag)
		}
		if cmd.Flags().Changed("relation") {
			rels := make([]int32, 0, 4)
			for _, part := range strings.Split(focusCreateRelationFlag, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				n, err := strconv.ParseInt(part, 10, 32)
				if err != nil {
					return fmt.Errorf("invalid --relation %q: use a comma-separated list of integers, e.g. 0,1", part)
				}
				rels = append(rels, int32(n))
			}
			if len(rels) > 0 {
				req.RelationType = &rels
			}
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.CreateFocusWithResponse(cmd.Context(), req)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			f := focusDecode(resp.Body, resp.JSON200)
			fmt.Fprintf(w, "Created focus %s (type %s)\n", Str(f.Id), focusTypeName(I32(f.Type)))
		})
	},
}

var focusDeleteCmd = &cobra.Command{
	Use:   "delete FOCUS_ID",
	Short: "Delete a focus session",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick focus delete focus-1 --type 0
  ticktick focus delete focus-1 --type 1 --json

Example API response:
{
  "id": "focus-1",
  "type": 0
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.DeleteFocusWithResponse(cmd.Context(), args[0],
			&api.DeleteFocusParams{Type: int32(focusDeleteTypeFlag)})
		if err != nil {
			return err
		}
		// Delete Focus returns an OpenFocus body, so Render (not RenderOK).
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			fmt.Fprintf(w, "Deleted focus %s\n", args[0])
		})
	},
}

func init() {
	focusCmd.AddCommand(focusGetCmd, focusListCmd, focusCreateCmd, focusDeleteCmd)

	focusGetCmd.Flags().IntVar(&focusGetTypeFlag, "type", 0,
		"focus type: 0 = Pomodoro, 1 = Timing")
	_ = focusGetCmd.MarkFlagRequired("type")

	focusListCmd.Flags().StringVar(&focusListFromFlag, "from", "",
		"range start (YYYY-MM-DD, \"YYYY-MM-DD HH:MM\" or RFC3339)")
	focusListCmd.Flags().StringVar(&focusListToFlag, "to", "",
		"range end (YYYY-MM-DD, \"YYYY-MM-DD HH:MM\" or RFC3339)")
	focusListCmd.Flags().IntVar(&focusListTypeFlag, "type", 0,
		"focus type: 0 = Pomodoro, 1 = Timing")
	_ = focusListCmd.MarkFlagRequired("from")
	_ = focusListCmd.MarkFlagRequired("to")
	_ = focusListCmd.MarkFlagRequired("type")

	focusCreateCmd.Flags().IntVar(&focusCreateTypeFlag, "type", 0,
		"focus type: 0 = Pomodoro, 1 = Timing")
	focusCreateCmd.Flags().StringVar(&focusCreateTaskFlag, "task", "",
		"related task id")
	focusCreateCmd.Flags().StringVar(&focusCreateNoteFlag, "note", "",
		"focus note (max 5000 chars)")
	focusCreateCmd.Flags().StringVar(&focusCreateStartFlag, "start", "",
		"start time (YYYY-MM-DD, \"YYYY-MM-DD HH:MM\" or RFC3339)")
	focusCreateCmd.Flags().StringVar(&focusCreateEndFlag, "end", "",
		"end time (YYYY-MM-DD, \"YYYY-MM-DD HH:MM\" or RFC3339)")
	focusCreateCmd.Flags().IntVar(&focusCreatePauseFlag, "pause", 0,
		"pause duration in seconds")
	focusCreateCmd.Flags().Int64Var(&focusCreateDurationFlag, "duration", 0,
		"focus duration in seconds")
	focusCreateCmd.Flags().StringVar(&focusCreateRelationFlag, "relation", "",
		"relation types, comma-separated, e.g. 0,1")
	focusCreateCmd.Flags().StringVar(&focusCreateDataFlag, "data", "",
		"raw JSON request body; explicit flags override its fields")
	_ = focusCreateCmd.MarkFlagRequired("type")

	focusDeleteCmd.Flags().IntVar(&focusDeleteTypeFlag, "type", 0,
		"focus type: 0 = Pomodoro, 1 = Timing")
	_ = focusDeleteCmd.MarkFlagRequired("type")
}
