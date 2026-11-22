package cli

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// Restrict the free-form string to recurrence syntax before emitting it.
var v2HabitRule = regexp.MustCompile(`(?i)^(?:(?:RRULE|LUNAR):)?FREQ=(?:YEARLY|MONTHLY|WEEKLY|DAILY|HOURLY|MINUTELY|SECONDLY)(?:;` +
	`(?:(?:INTERVAL|COUNT|TT_TIMES)=[0-9]+|UNTIL=[0-9]{8}(?:T[0-9]{6}Z?)?|WKST=(?:MO|TU|WE|TH|FR|SA|SU)|` +
	`BYDAY=(?:[+-]?[0-9]+)?(?:MO|TU|WE|TH|FR|SA|SU)(?:,(?:[+-]?[0-9]+)?(?:MO|TU|WE|TH|FR|SA|SU))*|` +
	`(?:BYSECOND|BYMINUTE|BYHOUR|BYMONTHDAY|BYYEARDAY|BYWEEKNO|BYMONTH|BYSETPOS)=[+-]?[0-9]+(?:,[+-]?[0-9]+)*))*$`)

func v2HabitTime(value *string) *string {
	formatted := v2SafeTime(Str(value), time.RFC3339Nano)
	if formatted == "" {
		return nil
	}
	return &formatted
}

func (o v2Output) habit(h api.OpenHabit) api.OpenHabit {
	safe := api.OpenHabit{
		Status: h.Status, SortOrder: h.SortOrder, TotalCheckIns: h.TotalCheckIns,
		Goal: h.Goal, Step: h.Step, RecordEnable: h.RecordEnable,
		TargetStartDate: h.TargetStartDate, TargetDays: h.TargetDays, CompletedCycles: h.CompletedCycles,
		CreatedTime: v2HabitTime(h.CreatedTime), ModifiedTime: v2HabitTime(h.ModifiedTime),
		ArchivedTime: v2HabitTime(h.ArchivedTime),
	}
	if h.Id != nil {
		safe.Id = Ptr(o.id(*h.Id))
	}
	if h.SectionId != nil {
		safe.SectionId = Ptr(o.id(*h.SectionId))
	}
	if v2HabitRule.MatchString(Str(h.RepeatRule)) {
		safe.RepeatRule = h.RepeatRule
	}
	if h.ExDates != nil {
		dates := make([]string, 0, len(*h.ExDates))
		for _, value := range *h.ExDates {
			if day, err := v2ParseDay(value); err == nil {
				dates = append(dates, day.Format(api.V2DayLayout))
			}
		}
		safe.ExDates = &dates
	}
	return safe
}

func newV2HabitsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "habits",
		Short:   "List habit schedules and statuses through the TickTick web API",
		Example: "  ticktick v2 habits --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, output, err := newV2CLIClientAt(api.V2HabitOrigin)
			if err != nil {
				return err
			}
			habits, err := client.Habits(cmd.Context())
			if err != nil {
				return err
			}
			safe := make([]api.OpenHabit, 0, len(habits))
			for _, habit := range habits {
				safe = append(safe, output.habit(habit))
			}
			return v2Render(safe, func(w io.Writer) {
				rows := make([][]string, 0, len(safe))
				for _, habit := range safe {
					rows = append(rows, []string{
						Str(habit.Id), Str(habit.SectionId), habitFmtInt32(habit.Status), Str(habit.RepeatRule),
						habitFmtStamp(habit.TargetStartDate), Str(habit.ArchivedTime),
					})
				}
				Table(w, []string{"HABIT", "SECTION", "STATUS", "REPEAT", "TARGET START", "ARCHIVED (UTC)"}, rows)
			})
		},
	}
}

func newV2HabitSectionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "habit-sections",
		Short:   "List habit section metadata through the TickTick web API",
		Example: "  ticktick v2 habit-sections --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, output, err := newV2CLIClientAt(api.V2HabitOrigin)
			if err != nil {
				return err
			}
			sections, err := client.HabitSections(cmd.Context())
			if err != nil {
				return err
			}
			safe := make([]api.OpenHabitSection, 0, len(sections))
			for _, section := range sections {
				safe = append(safe, api.OpenHabitSection{
					Id: Ptr(output.id(Str(section.Id))), SortOrder: section.SortOrder,
					CreatedTime: v2HabitTime(section.CreatedTime), ModifiedTime: v2HabitTime(section.ModifiedTime),
				})
			}
			return v2Render(safe, func(w io.Writer) {
				rows := make([][]string, 0, len(safe))
				for _, section := range safe {
					rows = append(rows, []string{Str(section.Id), habitFmtInt64(section.SortOrder)})
				}
				Table(w, []string{"SECTION", "SORT ORDER"}, rows)
			})
		},
	}
}

func v2ParseHabitQuery(ids []string, after string) (api.V2HabitCheckinOptions, error) {
	opts := api.V2HabitCheckinOptions{HabitIDs: taskQuerySplitCSV(strings.Join(ids, ","))}
	if len(opts.HabitIDs) == 0 {
		return opts, errors.New("--ids requires at least one habit ID")
	}
	if after != "" {
		day, err := v2ParseDay(after)
		if err != nil {
			return opts, fmt.Errorf("--after: %w", err)
		}
		opts.AfterStamp = Ptr(int32(day.Year()*10000 + int(day.Month())*100 + day.Day()))
	}
	return opts, nil
}

func newV2HabitRecordsCmd() *cobra.Command {
	var ids []string
	var after string
	cmd := &cobra.Command{
		Use:     "habit-records",
		Short:   "Query habit journal entry metadata through the TickTick web API",
		Long:    "Query habit journal entry dates with original habit IDs. Use habit-checkins for completion history.",
		Example: "  ticktick v2 habit-records --ids h1,h2 --after 2026-11-01 --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := v2ParseHabitQuery(ids, after)
			if err != nil {
				return err
			}
			client, output, err := newV2CLIClientAt(api.V2HabitOrigin)
			if err != nil {
				return err
			}
			result, err := client.HabitRecords(cmd.Context(), opts)
			if err != nil {
				return err
			}
			safe := api.V2HabitRecords{Records: make(map[string][]api.V2HabitRecord, len(result.Records))}
			for habit, records := range result.Records {
				entries := make([]api.V2HabitRecord, 0, len(records))
				for _, record := range records {
					entries = append(entries, api.V2HabitRecord{
						ID: output.id(record.ID), HabitID: output.id(record.HabitID), Stamp: record.Stamp,
						OpTime: v2SafeTime(record.OpTime, time.RFC3339Nano),
					})
				}
				safe.Records[output.id(habit)] = entries
			}
			return v2Render(safe, func(w io.Writer) {
				habits := make([]string, 0, len(safe.Records))
				for habit := range safe.Records {
					habits = append(habits, habit)
				}
				sort.Strings(habits)
				var rows [][]string
				for _, habit := range habits {
					for _, record := range safe.Records[habit] {
						rows = append(rows, []string{habit, record.ID, StampToDate(record.Stamp), record.OpTime})
					}
				}
				Table(w, []string{"HABIT", "RECORD", "DAY", "OP TIME (UTC)"}, rows)
			})
		},
	}
	cmd.Flags().StringSliceVar(&ids, "ids", nil, "original habit IDs (comma-separated or repeat the flag)")
	cmd.Flags().StringVar(&after, "after", "", "lower date bound (YYYYMMDD or YYYY-MM-DD)")
	_ = cmd.MarkFlagRequired("ids")
	return cmd
}
