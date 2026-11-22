package api

import (
	"context"
	"net/http"
)

const V2HabitOrigin = "https://api.ticktick.com"

// Habits returns current habit definitions, including archived habits.
func (c *V2Client) Habits(ctx context.Context) ([]OpenHabit, error) {
	return v2Request[[]OpenHabit](ctx, c, http.MethodGet, "/habits", nil, nil)
}

// HabitSections returns the web account's habit sections.
func (c *V2Client) HabitSections(ctx context.Context) ([]OpenHabitSection, error) {
	return v2Request[[]OpenHabitSection](ctx, c, http.MethodGet, "/habitSections", nil, nil)
}

// V2HabitRecordOptions uses the same request fields as HabitCheckins.
type V2HabitRecordOptions = V2HabitCheckinOptions

// V2HabitRecord is a journal entry associated with a habit day.
type V2HabitRecord struct {
	ID      string `json:"id"`
	HabitID string `json:"habitId,omitempty"`
	Stamp   int32  `json:"stamp"`
	OpTime  string `json:"opTime,omitempty"`
	Content string `json:"content,omitempty"`
	Emoji   *int   `json:"emoji,omitempty"`
}

type V2HabitRecords struct {
	Records map[string][]V2HabitRecord `json:"habitRecords"`
}

// HabitRecords queries journal entries for the specified habits.
func (c *V2Client) HabitRecords(ctx context.Context, o V2HabitRecordOptions) (V2HabitRecords, error) {
	if o.HabitIDs == nil {
		o.HabitIDs = []string{}
	}
	return v2Request[V2HabitRecords](ctx, c, http.MethodPost, "/getHabitRecords", nil, o)
}
