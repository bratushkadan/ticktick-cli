package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// This file implements `ticktick habit` — habits and their check-ins:
//
//	habit list
//	habit get      HABIT_ID
//	habit sections
//	habit create   --name NAME [flags]
//	habit update   HABIT_ID [flags]
//	habit checkin  HABIT_ID --stamp YYYYMMDD [flags]
//	habit checkins --ids h1,h2 --from YYYYMMDD --to YYYYMMDD
//
// All optional request fields are pointers in the generated api package;
// the helpers in ptr.go (Ptr, Str, I32, I64, F64, ...) are used throughout.

// habitFlags holds the optional flags shared by `habit create` and
// `habit update`. Explicit flags always override --data fields.
type habitFlags struct {
	name            string
	icon            string
	color           string
	sortOrder       int64
	status          int
	encouragement   string
	typ             string
	goal            float64
	step            float64
	unit            string
	repeat          string
	reminders       []string
	record          bool
	section         string
	targetDays      int
	targetStart     string
	completedCycles int
	exDates         []string
	style           int
	data            string
}

// Shared flag value holders (registered in init).
var (
	habitCreateFlags habitFlags
	habitUpdateFlags habitFlags
)

// habitRegisterFlags registers the shared optional flags on cmd.
func habitRegisterFlags(cmd *cobra.Command, f *habitFlags) {
	fl := cmd.Flags()
	fl.StringVar(&f.name, "name", "", "habit name (max 1000 chars)")
	fl.StringVar(&f.icon, "icon", "", "habit icon resource, e.g. habit_reading")
	fl.StringVar(&f.color, "color", "", "habit color, e.g. #4D8CF5")
	fl.Int64Var(&f.sortOrder, "sort-order", 0, "habit sort order")
	fl.IntVar(&f.status, "status", 0, "habit status")
	fl.StringVar(&f.encouragement, "encouragement", "", "habit encouragement message")
	fl.StringVar(&f.typ, "type", "", "habit type, e.g. Boolean")
	fl.Float64Var(&f.goal, "goal", 0, "habit goal")
	fl.Float64Var(&f.step, "step", 0, "habit step")
	fl.StringVar(&f.unit, "unit", "", "habit unit, e.g. Count")
	fl.StringVar(&f.repeat, "repeat", "", "habit repeat rule, e.g. RRULE:FREQ=DAILY;INTERVAL=1")
	fl.StringArrayVar(&f.reminders, "reminder", nil, "habit reminder (repeatable)")
	fl.BoolVar(&f.record, "record", false, "enable habit record")
	fl.StringVar(&f.section, "section", "", "habit section identifier")
	fl.IntVar(&f.targetDays, "target-days", 0, "target days")
	fl.StringVar(&f.targetStart, "target-start", "",
		"target start date: YYYYMMDD or YYYY-MM-DD")
	fl.IntVar(&f.completedCycles, "completed-cycles", 0, "completed cycles")
	fl.StringArrayVar(&f.exDates, "ex-date", nil, "excluded date (repeatable)")
	fl.IntVar(&f.style, "style", 0, "habit style")
	fl.StringVar(&f.data, "data", "",
		"raw JSON request body; fields are merged first and explicit flags win")
}

// applyCreate merges --data (already unmarshalled into req) and every flag
// that was explicitly set into the create request.
func (f *habitFlags) applyCreate(cmd *cobra.Command, req *api.HabitCreateRequest) error {
	fl := cmd.Flags()
	if fl.Changed("name") {
		req.Name = f.name
	}
	if fl.Changed("icon") {
		req.IconRes = Ptr(f.icon)
	}
	if fl.Changed("color") {
		req.Color = Ptr(f.color)
	}
	if fl.Changed("sort-order") {
		req.SortOrder = Ptr(f.sortOrder)
	}
	if fl.Changed("status") {
		req.Status = Ptr(int32(f.status))
	}
	if fl.Changed("encouragement") {
		req.Encouragement = Ptr(f.encouragement)
	}
	if fl.Changed("type") {
		req.Type = Ptr(f.typ)
	}
	if fl.Changed("goal") {
		req.Goal = Ptr(f.goal)
	}
	if fl.Changed("step") {
		req.Step = Ptr(f.step)
	}
	if fl.Changed("unit") {
		req.Unit = Ptr(f.unit)
	}
	if fl.Changed("repeat") {
		req.RepeatRule = Ptr(f.repeat)
	}
	if fl.Changed("reminder") {
		req.Reminders = Ptr(f.reminders)
	}
	if fl.Changed("record") {
		req.RecordEnable = Ptr(f.record)
	}
	if fl.Changed("section") {
		req.SectionId = Ptr(f.section)
	}
	if fl.Changed("target-days") {
		req.TargetDays = Ptr(int32(f.targetDays))
	}
	if fl.Changed("target-start") {
		stamp, err := ParseStamp(f.targetStart)
		if err != nil {
			return err
		}
		req.TargetStartDate = Ptr(stamp)
	}
	if fl.Changed("completed-cycles") {
		req.CompletedCycles = Ptr(int32(f.completedCycles))
	}
	if fl.Changed("ex-date") {
		req.ExDates = Ptr(f.exDates)
	}
	if fl.Changed("style") {
		req.Style = Ptr(int32(f.style))
	}
	return nil
}

// applyUpdate merges --data (already unmarshalled into req) and every flag
// that was explicitly set into the update request.
func (f *habitFlags) applyUpdate(cmd *cobra.Command, req *api.HabitUpdateRequest) error {
	fl := cmd.Flags()
	if fl.Changed("name") {
		req.Name = Ptr(f.name)
	}
	if fl.Changed("icon") {
		req.IconRes = Ptr(f.icon)
	}
	if fl.Changed("color") {
		req.Color = Ptr(f.color)
	}
	if fl.Changed("sort-order") {
		req.SortOrder = Ptr(f.sortOrder)
	}
	if fl.Changed("status") {
		req.Status = Ptr(int32(f.status))
	}
	if fl.Changed("encouragement") {
		req.Encouragement = Ptr(f.encouragement)
	}
	if fl.Changed("type") {
		req.Type = Ptr(f.typ)
	}
	if fl.Changed("goal") {
		req.Goal = Ptr(f.goal)
	}
	if fl.Changed("step") {
		req.Step = Ptr(f.step)
	}
	if fl.Changed("unit") {
		req.Unit = Ptr(f.unit)
	}
	if fl.Changed("repeat") {
		req.RepeatRule = Ptr(f.repeat)
	}
	if fl.Changed("reminder") {
		req.Reminders = Ptr(f.reminders)
	}
	if fl.Changed("record") {
		req.RecordEnable = Ptr(f.record)
	}
	if fl.Changed("section") {
		req.SectionId = Ptr(f.section)
	}
	if fl.Changed("target-days") {
		req.TargetDays = Ptr(int32(f.targetDays))
	}
	if fl.Changed("target-start") {
		stamp, err := ParseStamp(f.targetStart)
		if err != nil {
			return err
		}
		req.TargetStartDate = Ptr(stamp)
	}
	if fl.Changed("completed-cycles") {
		req.CompletedCycles = Ptr(int32(f.completedCycles))
	}
	if fl.Changed("ex-date") {
		req.ExDates = Ptr(f.exDates)
	}
	if fl.Changed("style") {
		req.Style = Ptr(int32(f.style))
	}
	return nil
}

// habitDecodeHabit parses a habit response body. Empty bodies (some 201
// responses) yield a zero habit rather than an error.
func habitDecodeHabit(body []byte) (api.OpenHabit, error) {
	var h api.OpenHabit
	if len(bytes.TrimSpace(body)) == 0 {
		return h, nil
	}
	if err := json.Unmarshal(body, &h); err != nil {
		return h, fmt.Errorf("decode habit: %w", err)
	}
	return h, nil
}

// habitDecodeHabitList parses a habit array response body.
func habitDecodeHabitList(body []byte) ([]api.OpenHabit, error) {
	var habits []api.OpenHabit
	if len(bytes.TrimSpace(body)) == 0 {
		return habits, nil
	}
	if err := json.Unmarshal(body, &habits); err != nil {
		return nil, fmt.Errorf("decode habits: %w", err)
	}
	return habits, nil
}

// habitDecodeSectionList parses a habit section array response body.
func habitDecodeSectionList(body []byte) ([]api.OpenHabitSection, error) {
	var sections []api.OpenHabitSection
	if len(bytes.TrimSpace(body)) == 0 {
		return sections, nil
	}
	if err := json.Unmarshal(body, &sections); err != nil {
		return nil, fmt.Errorf("decode habit sections: %w", err)
	}
	return sections, nil
}

// habitDecodeCheckin parses a single habit check-in response body.
func habitDecodeCheckin(body []byte) (api.OpenHabitCheckin, error) {
	var ch api.OpenHabitCheckin
	if len(bytes.TrimSpace(body)) == 0 {
		return ch, nil
	}
	if err := json.Unmarshal(body, &ch); err != nil {
		return ch, fmt.Errorf("decode habit check-in: %w", err)
	}
	return ch, nil
}

// habitDecodeCheckinList parses a habit check-in array response body.
func habitDecodeCheckinList(body []byte) ([]api.OpenHabitCheckin, error) {
	var checkins []api.OpenHabitCheckin
	if len(bytes.TrimSpace(body)) == 0 {
		return checkins, nil
	}
	if err := json.Unmarshal(body, &checkins); err != nil {
		return nil, fmt.Errorf("decode habit check-ins: %w", err)
	}
	return checkins, nil
}

// habitID renders a habit id for human output.
func habitID(id string) string {
	if id == "" {
		return "(unknown)"
	}
	return id
}

// habitFmtFloat renders an optional float without trailing zeros ("" when nil).
func habitFmtFloat(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

// habitFmtInt32 renders an optional int32 as text ("" when nil).
func habitFmtInt32(p *int32) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(int64(*p), 10)
}

// habitFmtInt64 renders an optional int64 as text ("" when nil).
func habitFmtInt64(p *int64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(*p, 10)
}

// habitFmtStamp renders an optional YYYYMMDD stamp as YYYY-MM-DD ("" when nil).
func habitFmtStamp(p *int32) string {
	if p == nil {
		return ""
	}
	return StampToDate(*p)
}

// habitPrintHabitDetails renders every key habit field.
func habitPrintHabitDetails(w io.Writer, h api.OpenHabit) {
	KV(w,
		[2]string{"Name", Str(h.Name)},
		[2]string{"Type", Str(h.Type)},
		[2]string{"Goal", habitFmtFloat(h.Goal)},
		[2]string{"Step", habitFmtFloat(h.Step)},
		[2]string{"Unit", Str(h.Unit)},
		[2]string{"Color", Str(h.Color)},
		[2]string{"Icon", Str(h.IconRes)},
		[2]string{"Status", habitFmtInt32(h.Status)},
		[2]string{"Total check-ins", habitFmtInt32(h.TotalCheckIns)},
		[2]string{"Repeat rule", Str(h.RepeatRule)},
		[2]string{"Reminders", strings.Join(Strs(h.Reminders), ", ")},
		[2]string{"Section", Str(h.SectionId)},
		[2]string{"Target days", habitFmtInt32(h.TargetDays)},
		[2]string{"Target start", habitFmtStamp(h.TargetStartDate)},
		[2]string{"Completed cycles", habitFmtInt32(h.CompletedCycles)},
		[2]string{"Encouragement", Str(h.Encouragement)},
		[2]string{"Created", FmtTime(Str(h.CreatedTime))},
		[2]string{"Modified", FmtTime(Str(h.ModifiedTime))},
	)
}

// habitCheckinEntries dereferences an optional check-in entry slice.
func habitCheckinEntries(p *[]api.OpenHabitCheckinData) []api.OpenHabitCheckinData {
	if p == nil {
		return nil
	}
	return *p
}

// habitPrintCheckinTable renders the check-in entries of one habit.
func habitPrintCheckinTable(w io.Writer, checkins []api.OpenHabitCheckinData) {
	rows := make([][]string, 0, len(checkins))
	for _, ch := range checkins {
		rows = append(rows, []string{
			habitFmtStamp(ch.Stamp),
			habitFmtFloat(ch.Value),
			habitFmtFloat(ch.Goal),
			habitFmtInt32(ch.Status),
		})
	}
	Table(w, []string{"STAMP", "VALUE", "GOAL", "STATUS"}, rows)
}

var habitListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all habits",
	Args:  cobra.NoArgs,
	Example: `  ticktick habit list
  ticktick habit list --json

Example API response:
[
  {
    "id": "habit-1",
    "name": "Read",
    "repeatRule": "RRULE:FREQ=DAILY;INTERVAL=1",
    "status": 0
  },
  {
    "id": "habit-2",
    "name": "Exercise",
    "repeatRule": "RRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR",
    "status": 0
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListHabitsWithResponse(cmd.Context())
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		habits, err := habitDecodeHabitList(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			rows := make([][]string, 0, len(habits))
			for _, h := range habits {
				rows = append(rows, []string{
					Str(h.Id),
					Str(h.Name),
					Str(h.Type),
					habitFmtFloat(h.Goal),
					Str(h.Unit),
					Str(h.RepeatRule),
					habitFmtInt32(h.Status),
				})
			}
			Table(w, []string{"ID", "NAME", "TYPE", "GOAL", "UNIT", "REPEAT", "STATUS"}, rows)
		})
	},
}

var habitGetCmd = &cobra.Command{
	Use:   "get HABIT_ID",
	Short: "Get a habit by id",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick habit get habit-1
  ticktick habit get habit-1 --json

Example API response:
{
  "id": "habit-1",
  "name": "Read",
  "iconRes": "habit_reading",
  "color": "#4D8CF5",
  "sortOrder": 12345,
  "status": 0,
  "encouragement": "Keep going",
  "totalCheckIns": 12,
  "createdTime": "2024-01-01T00:00:00+0000",
  "modifiedTime": "2024-01-08T00:00:00+0000",
  "type": "Boolean",
  "goal": 1.0,
  "step": 1.0,
  "unit": "Count"
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.GetHabitWithResponse(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		h, err := habitDecodeHabit(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			KV(w, [2]string{"ID", Str(h.Id)})
			habitPrintHabitDetails(w, h)
		})
	},
}

var habitSectionsCmd = &cobra.Command{
	Use:   "sections",
	Short: "List the habit sections of the current user",
	Args:  cobra.NoArgs,
	Example: `  ticktick habit sections
  ticktick habit sections --json

Example API response:
[
  {
    "id": "section-1",
    "name": "Health",
    "sortOrder": 12345,
    "createdTime": "2026-07-01T00:00:00+0000",
    "modifiedTime": "2026-07-01T00:00:00+0000",
    "etag": "section-etag"
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListHabitSectionsWithResponse(cmd.Context())
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		sections, err := habitDecodeSectionList(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			rows := make([][]string, 0, len(sections))
			for _, s := range sections {
				rows = append(rows, []string{
					Str(s.Id),
					Str(s.Name),
					habitFmtInt64(s.SortOrder),
				})
			}
			Table(w, []string{"ID", "NAME", "SORTORDER"}, rows)
		})
	},
}

var habitCreateCmd = &cobra.Command{
	Use:   "create --name NAME",
	Short: "Create a habit (name required, max 1000 chars)",
	Args:  cobra.NoArgs,
	Example: `  ticktick habit create --name Read --icon habit_reading --color "#4D8CF5" --type Boolean --goal 1 --step 1 --unit Count --repeat "RRULE:FREQ=DAILY;INTERVAL=1"
  ticktick habit create --name Read --data '{"name":"Read","goal":2}' --json

Example API response:
{
  "id": "habit-1",
  "name": "Read",
  "iconRes": "habit_reading",
  "color": "#4D8CF5",
  "type": "Boolean",
  "goal": 1.0,
  "step": 1.0,
  "unit": "Count",
  "repeatRule": "RRULE:FREQ=DAILY;INTERVAL=1",
  "recordEnable": false
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		f := habitCreateFlags
		req := api.HabitCreateRequest{Name: f.name}
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
		resp, err := c.CreateHabitWithResponse(cmd.Context(), req)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		h, err := habitDecodeHabit(resp.Body)
		if err != nil {
			return err
		}
		if h.Name == nil {
			h.Name = Ptr(req.Name)
		}
		return Render(resp.Body, func(w io.Writer) {
			fmt.Fprintf(w, "Created habit %s: %s\n", habitID(Str(h.Id)), Str(h.Name))
		})
	},
}

var habitUpdateCmd = &cobra.Command{
	Use:   "update HABIT_ID",
	Short: "Update a habit (only the flags you pass are changed)",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick habit update habit-1 --goal 2 --repeat "RRULE:FREQ=DAILY;INTERVAL=1"
  ticktick habit update habit-1 --name "Read more" --json

Example API response:
{
  "id": "habit-1",
  "name": "Read more",
  "goal": 2.0,
  "repeatRule": "RRULE:FREQ=DAILY;INTERVAL=1"
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		f := habitUpdateFlags
		var req api.HabitUpdateRequest
		if f.data != "" {
			if err := json.Unmarshal([]byte(f.data), &req); err != nil {
				return fmt.Errorf("invalid --data JSON: %w", err)
			}
		}
		if err := f.applyUpdate(cmd, &req); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.UpdateHabitWithResponse(cmd.Context(), args[0], req)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		h, err := habitDecodeHabit(resp.Body)
		if err != nil {
			return err
		}
		if h.Name == nil && req.Name != nil {
			h.Name = req.Name
		}
		return Render(resp.Body, func(w io.Writer) {
			fmt.Fprintf(w, "Updated habit %s: %s\n", habitID(Str(h.Id)), Str(h.Name))
		})
	},
}

var habitCheckinCmd = &cobra.Command{
	Use:   "checkin HABIT_ID --stamp YYYYMMDD",
	Short: "Create or update a habit check-in for one date stamp",
	Args:  cobra.ExactArgs(1),
	Example: `  ticktick habit checkin habit-1 --stamp 20260407
  ticktick habit checkin habit-1 --stamp 2026-04-07 --value 2 --goal 1 --status 0 --json

Example API response:
{
  "habitId": "habit-1",
  "year": 2026,
  "checkins": [
    {
      "stamp": 20260407,
      "value": 1.0,
      "goal": 1.0
    }
  ]
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fl := cmd.Flags()
		stampRaw, err := fl.GetString("stamp")
		if err != nil {
			return err
		}
		stamp, err := ParseStamp(stampRaw)
		if err != nil {
			return err
		}
		req := api.HabitCheckinRequest{Stamp: stamp}
		if fl.Changed("time") {
			v, err := fl.GetString("time")
			if err != nil {
				return err
			}
			normalized, err := NormalizeTime(v)
			if err != nil {
				return err
			}
			req.Time = Ptr(normalized)
		}
		if fl.Changed("op-time") {
			v, err := fl.GetString("op-time")
			if err != nil {
				return err
			}
			normalized, err := NormalizeTime(v)
			if err != nil {
				return err
			}
			req.OpTime = Ptr(normalized)
		}
		if fl.Changed("value") {
			v, err := fl.GetFloat64("value")
			if err != nil {
				return err
			}
			req.Value = Ptr(v)
		}
		if fl.Changed("goal") {
			v, err := fl.GetFloat64("goal")
			if err != nil {
				return err
			}
			req.Goal = Ptr(v)
		}
		if fl.Changed("status") {
			v, err := fl.GetInt("status")
			if err != nil {
				return err
			}
			req.Status = Ptr(int32(v))
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.CheckinHabitWithResponse(cmd.Context(), args[0], req)
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		checkin, err := habitDecodeCheckin(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			id := Str(checkin.HabitId)
			if id == "" {
				id = args[0]
			}
			year := I32(checkin.Year)
			if checkin.Year == nil {
				year = stamp / 10000
			}
			fmt.Fprintf(w, "Check-in recorded for %s (%d):\n", id, year)
			habitPrintCheckinTable(w, habitCheckinEntries(checkin.Checkins))
		})
	},
}

var habitCheckinsCmd = &cobra.Command{
	Use:   "checkins --ids HABIT_IDS --from YYYYMMDD --to YYYYMMDD",
	Short: "List habit check-ins for comma-separated habit ids in a date range",
	Args:  cobra.NoArgs,
	Example: `  ticktick habit checkins --ids habit-1,habit-2 --from 20260401 --to 20260407
  ticktick habit checkins --ids habit-1 --from 2026-04-01 --to 2026-04-07 --json

Example API response:
[
  {
    "habitId": "habit-1",
    "year": 2026,
    "checkins": [
      {
        "stamp": 20260407,
        "value": 1.0,
        "goal": 1.0
      }
    ]
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fl := cmd.Flags()
		ids, err := fl.GetString("ids")
		if err != nil {
			return err
		}
		fromRaw, err := fl.GetString("from")
		if err != nil {
			return err
		}
		toRaw, err := fl.GetString("to")
		if err != nil {
			return err
		}
		from, err := ParseStamp(fromRaw)
		if err != nil {
			return err
		}
		to, err := ParseStamp(toRaw)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListHabitCheckinsWithResponse(cmd.Context(), &api.ListHabitCheckinsParams{
			HabitIds: ids,
			From:     from,
			To:       to,
		})
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		checkins, err := habitDecodeCheckinList(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			for i, ch := range checkins {
				if i > 0 {
					fmt.Fprintln(w)
				}
				fmt.Fprintf(w, "habit %s (%d):\n", Str(ch.HabitId), I32(ch.Year))
				habitPrintCheckinTable(w, habitCheckinEntries(ch.Checkins))
			}
		})
	},
}

func init() {
	habitRegisterFlags(habitCreateCmd, &habitCreateFlags)
	_ = habitCreateCmd.MarkFlagRequired("name")

	habitRegisterFlags(habitUpdateCmd, &habitUpdateFlags)

	habitCheckinCmd.Flags().String("stamp", "",
		"date stamp to check in: YYYYMMDD or YYYY-MM-DD (required)")
	habitCheckinCmd.Flags().String("time", "",
		`check-in time: YYYY-MM-DD, "YYYY-MM-DD HH:MM", or RFC3339`)
	habitCheckinCmd.Flags().String("op-time", "",
		`operation time: YYYY-MM-DD, "YYYY-MM-DD HH:MM", or RFC3339`)
	habitCheckinCmd.Flags().Float64("value", 0, "check-in value (default 1.0)")
	habitCheckinCmd.Flags().Float64("goal", 0, "check-in goal (default 1.0)")
	habitCheckinCmd.Flags().Int("status", 0, "check-in status")
	_ = habitCheckinCmd.MarkFlagRequired("stamp")

	habitCheckinsCmd.Flags().String("ids", "",
		"habit ids separated by commas, e.g. habit-1,habit-2 (required)")
	habitCheckinsCmd.Flags().String("from", "",
		"start date stamp: YYYYMMDD or YYYY-MM-DD (required)")
	habitCheckinsCmd.Flags().String("to", "",
		"end date stamp: YYYYMMDD or YYYY-MM-DD (required)")
	_ = habitCheckinsCmd.MarkFlagRequired("ids")
	_ = habitCheckinsCmd.MarkFlagRequired("from")
	_ = habitCheckinsCmd.MarkFlagRequired("to")

	habitCmd.AddCommand(habitListCmd, habitGetCmd, habitSectionsCmd,
		habitCreateCmd, habitUpdateCmd, habitCheckinCmd, habitCheckinsCmd)
}
