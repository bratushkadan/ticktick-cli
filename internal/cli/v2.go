package cli

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

const v2CookieKey = "TICKTICK_V2_API_COOKIE"

type v2Output struct {
	key []byte
}

func resolveV2Cookie(envFile string) (string, error) {
	cookie := strings.TrimSpace(os.Getenv(v2CookieKey))
	if cookie == "" {
		path := strings.TrimSpace(envFile)
		explicit := path != ""
		if !explicit {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			path = filepath.Join(home, ".env")
		}
		data, err := os.ReadFile(path)
		if err != nil && !(errors.Is(err, os.ErrNotExist) && !explicit) {
			return "", fmt.Errorf("read TickTick v2 env file: %w", err)
		}
		values, err := readDotenvSettings(path, data, v2CookieKey)
		if err != nil {
			return "", err
		}
		cookie = values[v2CookieKey]
	}
	if cookie == "" {
		return "", errors.New("set TICKTICK_V2_API_COOKIE in the environment, ~/.env, or --env-file")
	}
	return cookie, nil
}

func newV2CLIClient() (*api.V2Client, v2Output, error) {
	return newV2CLIClientAt(api.V2Origin)
}

func newV2CLIClientAt(origin string) (*api.V2Client, v2Output, error) {
	cookie, err := resolveV2Cookie(flagEnvFile)
	if err != nil {
		return nil, v2Output{}, err
	}
	client, err := api.NewV2Client(origin, cookie, &http.Client{
		Timeout: flagTimeout, Transport: apiTransport,
	})
	return client, v2Output{key: []byte(strings.TrimPrefix(cookie, "t="))}, err
}

func (o v2Output) id(s string) string {
	if s == "" {
		return ""
	}
	mac := hmac.New(sha256.New, o.key)
	mac.Write([]byte("ticktick-v2-output\x00"))
	mac.Write([]byte(s))
	return "anon-" + hex.EncodeToString(mac.Sum(nil)[:12])
}

func (o v2Output) tasks(tasks []api.V2Task) []api.V2Task {
	out := make([]api.V2Task, 0, len(tasks))
	for _, t := range tasks {
		t.ID, t.ProjectID = o.id(t.ID), o.id(t.ProjectID)
		t.StatusSlug = taskStatusSlug(t.Status, false)
		t.DueDate = v2SafeTime(t.DueDate, time.RFC3339)
		t.CompletedTime = v2SafeTime(t.CompletedTime, time.RFC3339)
		switch t.Kind {
		case "TEXT", "NOTE", "CHECKLIST":
		default:
			t.Kind = ""
		}
		tags := make([]string, 0, len(t.Tags))
		for _, tag := range t.Tags {
			tags = append(tags, o.id(tag))
		}
		t.Tags = tags
		out = append(out, t)
	}
	return out
}

func (o v2Output) cachedTasks(tasks []api.V2Task, cacheIDs bool) []api.V2Task {
	out := o.tasks(tasks)
	if cacheIDs {
		for i, task := range tasks {
			out[i].ID = task.ID
		}
	}
	return out
}

func addCacheIDsFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("cache-ids", false, "with --json, retain original task IDs for local cache lookup")
	cmd.Flags().String("snapshot-file", "", "with --cache-ids, write original task records to a new private file")
}

func cacheIDsRequested(cmd *cobra.Command) (bool, error) {
	cacheIDs, _ := cmd.Flags().GetBool("cache-ids")
	if cmd.Flags().Changed("snapshot-file") {
		path, _ := cmd.Flags().GetString("snapshot-file")
		if !cacheIDs || path == "" {
			return false, errors.New("--snapshot-file requires a path and --cache-ids")
		}
	}
	if cacheIDs && !flagJSON {
		return false, errors.New("--cache-ids requires --json")
	}
	return cacheIDs, nil
}

func cacheTaskPage(cmd *cobra.Command, records []api.V2Task) error {
	if err := cacheWebTasks(cmd.Context(), records); err != nil {
		return err
	}
	path, _ := cmd.Flags().GetString("snapshot-file")
	if path == "" {
		return nil
	}
	data := make([]json.RawMessage, 0, len(records))
	for _, task := range records {
		data = append(data, task.OriginalJSON())
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("snapshot_write_failed: %w", err)
	}
	writeErr := json.NewEncoder(file).Encode(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("snapshot_write_failed: %w", err)
	}
	return nil
}

func (o v2Output) durations(values map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(values))
	for label, duration := range values {
		out[o.id(label)] = duration
	}
	return out
}

func v2ParseTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, inputLayout := range inputLayouts {
		if t, err := time.Parse(inputLayout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("use a date, UTC date-time, or RFC3339 timestamp")
}

func v2SafeTime(value, layout string) string {
	t, err := v2ParseTime(value)
	if err != nil {
		return ""
	}
	return t.Format(layout)
}

func v2PrintTasks(w io.Writer, tasks []api.V2Task) {
	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, []string{
			t.ID, t.ProjectID, strconv.Itoa(t.Status), strconv.Itoa(t.Priority),
			t.Kind, t.DueDate, t.CompletedTime, strings.Join(t.Tags, ","),
		})
	}
	Table(w, []string{"ID", "PROJECT", "STATUS", "PRIORITY", "KIND", "DUE (UTC)", "COMPLETED (UTC)", "TAGS"}, rows)
}

func v2Render(result any, print func(io.Writer)) error {
	if flagJSON {
		return PrintJSON(result)
	}
	print(os.Stdout)
	return nil
}

func v2SearchStatus(values []string) ([]int, error) {
	if len(values) == 1 {
		switch strings.ToLower(strings.TrimSpace(values[0])) {
		case "open":
			return []int{0}, nil
		case "completed":
			return []int{1, 2}, nil
		}
	}
	items, err := taskQueryInt32List("--status", strings.Join(values, ","))
	if err != nil {
		return nil, err
	}
	status := make([]int, 0, len(items))
	for _, item := range items {
		status = append(status, int(item))
	}
	return status, nil
}

func newV2SearchCmd() *cobra.Command {
	var opts api.V2SearchOptions
	var statuses []string
	var from, to int64
	cmd := &cobra.Command{
		Use:   "search <keywords>",
		Short: "Search tasks and comments through the TickTick web API",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result := api.V2SearchResult{Tasks: []api.V2Task{}}
			if strings.TrimSpace(args[0]) != "" {
				query := opts
				query.Keywords = args[0]
				var err error
				query.Status, err = v2SearchStatus(statuses)
				if err != nil {
					return err
				}
				if cmd.Flags().Changed("due-from") {
					query.DueFrom = &from
				}
				if cmd.Flags().Changed("due-to") {
					query.DueTo = &to
				}
				c, output, err := newV2CLIClient()
				if err != nil {
					return err
				}
				result, err = c.Search(cmd.Context(), query)
				if err != nil {
					return err
				}
				result.Tasks = output.tasks(result.Tasks)
				for i := range result.Comments {
					comment := &result.Comments[i]
					comment.ID, comment.TaskID = output.id(comment.ID), output.id(comment.TaskID)
				}
				for i := range result.Hits {
					result.Hits[i].ID = output.id(result.Hits[i].ID)
				}
			}
			if result.Comments == nil {
				result.Comments = []api.V2SearchComment{}
			}
			if result.Hits == nil {
				result.Hits = []api.V2SearchHit{}
			}
			return v2Render(result, func(w io.Writer) {
				fmt.Fprintf(w, "Tasks: %d, comments: %d, hits: %d\n", len(result.Tasks), len(result.Comments), len(result.Hits))
				v2PrintTasks(w, result.Tasks)
				if len(result.Comments) > 0 {
					fmt.Fprintln(w)
					rows := make([][]string, 0, len(result.Comments))
					for _, comment := range result.Comments {
						rows = append(rows, []string{comment.ID, comment.TaskID})
					}
					Table(w, []string{"COMMENT", "TASK"}, rows)
				}
			})
		},
	}
	cmd.Flags().StringSliceVar(&statuses, "status", nil, "open, completed, or numeric status codes")
	cmd.Flags().StringSliceVar(&opts.ProjectIDs, "projects", nil, "project IDs (comma-separated or repeat the flag)")
	cmd.Flags().StringSliceVar(&opts.Tags, "tags", nil, "tags (comma-separated or repeat the flag)")
	cmd.Flags().Int64Var(&from, "due-from", 0, "lower due date bound in Unix milliseconds")
	cmd.Flags().Int64Var(&to, "due-to", 0, "upper due date bound in Unix milliseconds")
	cmd.Flags().BoolVar(&opts.IncludeClosed, "include-closed", false, "include closed projects in the search")
	return cmd
}

func newV2CompletedCmd() *cobra.Command {
	var from, to, project string
	var limit int
	var all bool
	cmd := &cobra.Command{
		Use:   "completed",
		Short: "List one page of task completion history",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cacheIDs, err := cacheIDsRequested(cmd)
			if err != nil {
				return err
			}
			if all && strings.TrimSpace(project) != "" {
				return errors.New("use either --all or --project")
			}
			if limit < 1 {
				return errors.New("--limit must be positive")
			}
			end := time.Now().UTC()
			if to != "" {
				var err error
				end, err = v2ParseTime(to)
				if err != nil {
					return fmt.Errorf("--to: %w", err)
				}
			}
			opts := api.V2CompletedOptions{To: end, Limit: limit}
			if from != "" {
				start, err := v2ParseTime(from)
				if err != nil {
					return fmt.Errorf("--from: %w", err)
				}
				if start.After(end) {
					return errors.New("--from must be on or before --to")
				}
				opts.From = &start
			}
			if _, err := checkTaskCache(); err != nil {
				return err
			}
			c, output, err := newV2CLIClient()
			if err != nil {
				return err
			}
			var records []api.V2Task
			switch {
			case all:
				records, err = c.Completed(cmd.Context(), "all", opts)
			case strings.TrimSpace(project) != "":
				records, err = c.Completed(cmd.Context(), strings.TrimSpace(project), opts)
			default:
				records, err = c.CompletedInAll(cmd.Context(), opts)
			}
			if err != nil {
				return err
			}
			if err := cacheTaskPage(cmd, records); err != nil {
				return err
			}
			result := struct {
				Tasks  []api.V2Task `json:"tasks"`
				NextTo string       `json:"nextTo,omitempty"`
			}{Tasks: output.cachedTasks(records, cacheIDs)}
			if len(records) > 0 {
				result.NextTo = v2SafeTime(records[len(records)-1].CompletedTime, api.V2HistoryTimeLayout)
			}
			return v2Render(result, func(w io.Writer) {
				v2PrintTasks(w, result.Tasks)
				KV(w, [2]string{"Next --to (UTC)", result.NextTo})
			})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "lower completion date or timestamp bound (UTC if no offset)")
	cmd.Flags().StringVar(&to, "to", "", "upper completion date or timestamp bound (default now)")
	cmd.Flags().IntVar(&limit, "limit", 100, "page size")
	cmd.Flags().BoolVar(&all, "all", false, "use the all/completed route")
	cmd.Flags().StringVar(&project, "project", "", "use the completed route of this project ID")
	addCacheIDsFlag(cmd)
	return cmd
}

func v2ParseDay(s string) (time.Time, error) {
	for _, layout := range []string{api.V2DayLayout, time.DateOnly} {
		if day, err := time.Parse(layout, s); err == nil {
			return day, nil
		}
	}
	return time.Time{}, errors.New("use a calendar date in YYYYMMDD or YYYY-MM-DD format")
}

func v2SafeDay(s string) string {
	t, err := v2ParseDay(s)
	if err != nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

func v2Hours(clock api.V2FocusClock) map[int]int64 {
	hours := make(map[int]int64, len(clock.TimeDurations))
	for hour, duration := range clock.TimeDurations {
		if 0 <= hour && hour <= 23 {
			hours[hour] = duration
		}
	}
	return hours
}

func v2HourRows(rows [][]string, prefix []string, hours map[int]int64) [][]string {
	for hour := 0; hour < 24; hour++ {
		if duration, ok := hours[hour]; ok {
			row := append([]string{}, prefix...)
			rows = append(rows, append(row, strconv.Itoa(hour), strconv.FormatInt(duration, 10)))
		}
	}
	return rows
}

func newV2FocusRangeCmd(use, short string, run func(*cobra.Command, *api.V2Client, v2Output, time.Time, time.Time) error) *cobra.Command {
	var from, to string
	cmd := &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			start, err := v2ParseDay(from)
			if err != nil {
				return fmt.Errorf("--from: %w", err)
			}
			end, err := v2ParseDay(to)
			if err != nil {
				return fmt.Errorf("--to: %w", err)
			}
			if end.Before(start) {
				return errors.New("--to must be on or after --from")
			}
			c, output, err := newV2CLIClient()
			if err != nil {
				return err
			}
			return run(cmd, c, output, start, end)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "first day (YYYYMMDD or YYYY-MM-DD)")
	cmd.Flags().StringVar(&to, "to", "", "last day (YYYYMMDD or YYYY-MM-DD)")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newV2FocusCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "focus", Short: "Read focus statistics from the TickTick web API"}
	cmd.AddCommand(
		newV2FocusRangeCmd("heatmap", "Show focus duration by day", v2FocusHeatmap),
		newV2FocusRangeCmd("dist", "Show focus duration by project, task and tag", v2FocusDist),
		newV2FocusRangeCmd("clock", "Show focus duration by hour", v2FocusClock),
		newV2FocusRangeCmd("clock-by-day", "Show focus duration by hour for each day", v2FocusClockByDay),
		&cobra.Command{
			Use: "general", Short: "Show today's and lifetime Pomodoro totals", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				c, _, err := newV2CLIClient()
				if err != nil {
					return err
				}
				stats, err := c.FocusGeneral(cmd.Context())
				if err != nil {
					return err
				}
				return v2Render(stats, func(w io.Writer) {
					Table(w, []string{"PERIOD", "COUNT", "DURATION"}, [][]string{
						{"today", strconv.FormatInt(stats.TodayPomoCount, 10), strconv.FormatInt(stats.TodayPomoDuration, 10)},
						{"total", strconv.FormatInt(stats.TotalPomoCount, 10), strconv.FormatInt(stats.TotalPomoDuration, 10)},
					})
				})
			},
		},
	)
	return cmd
}

func v2FocusHeatmap(cmd *cobra.Command, c *api.V2Client, _ v2Output, from, to time.Time) error {
	stats, err := c.FocusHeatmap(cmd.Context(), from, to)
	if err != nil {
		return err
	}
	for i := range stats {
		stats[i].Day = v2SafeDay(stats[i].Day)
	}
	return v2Render(stats, func(w io.Writer) {
		rows := make([][]string, 0, len(stats))
		for _, day := range stats {
			rows = append(rows, []string{day.Day, strconv.FormatInt(day.Duration, 10)})
		}
		Table(w, []string{"DAY", "DURATION"}, rows)
	})
}

func v2FocusDist(cmd *cobra.Command, c *api.V2Client, output v2Output, from, to time.Time) error {
	stats, err := c.FocusDistribution(cmd.Context(), from, to)
	if err != nil {
		return err
	}
	stats.ProjectDurations = output.durations(stats.ProjectDurations)
	stats.TaskDurations = output.durations(stats.TaskDurations)
	stats.TagDurations = output.durations(stats.TagDurations)
	return v2Render(stats, func(w io.Writer) {
		var rows [][]string
		for _, group := range []struct {
			label  string
			values map[string]int64
		}{
			{"project", stats.ProjectDurations},
			{"task", stats.TaskDurations},
			{"tag", stats.TagDurations},
		} {
			for id, duration := range group.values {
				rows = append(rows, []string{group.label, id, strconv.FormatInt(duration, 10)})
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i][0] == rows[j][0] {
				return rows[i][1] < rows[j][1]
			}
			return rows[i][0] < rows[j][0]
		})
		Table(w, []string{"TYPE", "ID", "DURATION"}, rows)
	})
}

func v2FocusClock(cmd *cobra.Command, c *api.V2Client, _ v2Output, from, to time.Time) error {
	stats, err := c.FocusClock(cmd.Context(), from, to)
	if err != nil {
		return err
	}
	stats.TimeDurations = v2Hours(stats)
	return v2Render(stats, func(w io.Writer) {
		Table(w, []string{"HOUR", "DURATION"}, v2HourRows(nil, nil, stats.TimeDurations))
	})
}

func v2FocusClockByDay(cmd *cobra.Command, c *api.V2Client, _ v2Output, from, to time.Time) error {
	stats, err := c.FocusClockByDay(cmd.Context(), from, to)
	if err != nil {
		return err
	}
	for i := range stats {
		stats[i].Day = v2SafeDay(stats[i].Day)
		stats[i].TimeDurations = v2Hours(stats[i].V2FocusClock)
	}
	return v2Render(stats, func(w io.Writer) {
		var rows [][]string
		for _, day := range stats {
			if len(day.TimeDurations) == 0 {
				rows = append(rows, []string{day.Day, "", "0"})
			}
			rows = v2HourRows(rows, []string{day.Day}, day.TimeDurations)
		}
		Table(w, []string{"DAY", "HOUR", "DURATION"}, rows)
	})
}

func newV2HabitCheckinsCmd() *cobra.Command {
	var ids []string
	var after string
	cmd := &cobra.Command{
		Use:   "habit-checkins",
		Short: "Query habit check-ins through the TickTick web API",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := v2ParseHabitQuery(ids, after)
			if err != nil {
				return err
			}
			c, output, err := newV2CLIClient()
			if err != nil {
				return err
			}
			res, err := c.HabitCheckins(cmd.Context(), opts)
			if err != nil {
				return err
			}
			safe := api.V2HabitCheckins{Checkins: make(map[string][]api.V2HabitCheckin, len(res.Checkins))}
			for habitID, entries := range res.Checkins {
				list := make([]api.V2HabitCheckin, 0, len(entries))
				for _, entry := range entries {
					entry.ID = output.id(entry.ID)
					list = append(list, entry)
				}
				safe.Checkins[output.id(habitID)] = list
			}
			return v2Render(safe, func(w io.Writer) {
				var rows [][]string
				habits := make([]string, 0, len(safe.Checkins))
				for habit := range safe.Checkins {
					habits = append(habits, habit)
				}
				sort.Strings(habits)
				for _, habit := range habits {
					for _, entry := range safe.Checkins[habit] {
						status := ""
						if entry.Status != nil {
							status = strconv.Itoa(*entry.Status)
						}
						rows = append(rows, []string{habit, entry.ID, StampToDate(entry.Stamp),
							strconv.FormatFloat(entry.Value, 'g', -1, 64), strconv.FormatFloat(entry.Goal, 'g', -1, 64), status})
					}
				}
				Table(w, []string{"HABIT", "CHECK-IN", "DAY", "VALUE", "GOAL", "STATUS"}, rows)
			})
		},
	}
	cmd.Flags().StringSliceVar(&ids, "ids", nil, "habit IDs (comma-separated or repeat the flag)")
	cmd.Flags().StringVar(&after, "after", "", "lower date bound (YYYYMMDD or YYYY-MM-DD)")
	_ = cmd.MarkFlagRequired("ids")
	return cmd
}

func newV2Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "v2",
		Short: "Read the TickTick web API with a session cookie",
		Long:  "Read the TickTick web API. Text and JSON output use pseudonyms for IDs and tags.",
	}
	cmd.AddCommand(newV2SearchCmd(), newV2CompletedCmd(), newV2FocusCmd(), newV2HabitCheckinsCmd())
	cmd.AddCommand(newV2HabitsCmd(), newV2HabitRecordsCmd(), newV2HabitSectionsCmd())
	return cmd
}

func init() {
	rootCmd.AddCommand(newV2Cmd())
}
