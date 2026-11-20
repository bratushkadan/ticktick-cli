package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Documented example payloads from docs/openapi.md (Habit section).
const (
	habitDocList = `[
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
]`

	habitDocGet = `{
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
  "unit": "Count",
  "etag": "habit-etag",
  "repeatRule": "RRULE:FREQ=DAILY;INTERVAL=1",
  "reminders": [],
  "recordEnable": false,
  "sectionId": "section-1",
  "targetDays": 0,
  "targetStartDate": 20240101,
  "completedCycles": 12,
  "exDates": [],
  "style": 0
}`

	habitDocSections = `[
  {
    "id": "section-1",
    "name": "Health",
    "sortOrder": 12345,
    "createdTime": "2026-07-01T00:00:00+0000",
    "modifiedTime": "2026-07-01T00:00:00+0000",
    "etag": "section-etag"
  }
]`

	habitDocCreate = `{
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
}`

	habitDocUpdate = `{
  "id": "habit-1",
  "name": "Read more",
  "goal": 2.0,
  "repeatRule": "RRULE:FREQ=DAILY;INTERVAL=1"
}`

	habitDocCheckin = `{
  "habitId": "habit-1",
  "year": 2026,
  "checkins": [
    {
      "stamp": 20260407,
      "value": 1.0,
      "goal": 1.0
    }
  ]
}`

	habitDocCheckins = `[
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
]`
)

// habitJSONEqual reports whether two JSON documents are semantically equal.
func habitJSONEqual(got, want string) (bool, string) {
	var gotV, wantV any
	if err := json.Unmarshal([]byte(got), &gotV); err != nil {
		return false, "stdout is not valid JSON: " + err.Error() + "\nstdout:\n" + got
	}
	if err := json.Unmarshal([]byte(want), &wantV); err != nil {
		return false, "expected JSON in test is invalid: " + err.Error()
	}
	if !reflect.DeepEqual(gotV, wantV) {
		return false, "\n got: " + strings.TrimSpace(got) + "\nwant: " + strings.TrimSpace(want)
	}
	return true, ""
}

// habitAssertJSON fails the test fatally when JSON documents differ.
func habitAssertJSON(t *testing.T, got, want string) {
	t.Helper()
	if ok, msg := habitJSONEqual(got, want); !ok {
		t.Fatalf("JSON mismatch: %s", msg)
	}
}

// habitCheckJSON reports a mismatch from inside an HTTP handler goroutine.
func habitCheckJSON(t *testing.T, got, want string) {
	t.Helper()
	if ok, msg := habitJSONEqual(got, want); !ok {
		t.Errorf("JSON mismatch: %s", msg)
	}
}

// habitServerQuery starts a test API server that asserts the outgoing request
// (method, path, optional JSON body, optional query params) and replies with
// respBody, then points the CLI at it for the duration of the test.
func habitServerQuery(t *testing.T, wantMethod, wantPath, wantBody string, wantQuery map[string]string, respBody string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != wantMethod {
			t.Errorf("method = %s, want %s", r.Method, wantMethod)
		}
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
		}
		for k, want := range wantQuery {
			if got := r.URL.Query().Get(k); got != want {
				t.Errorf("query %s = %q, want %q", k, got, want)
			}
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if wantBody != "" {
			habitCheckJSON(t, string(body), wantBody)
		} else if strings.TrimSpace(string(body)) != "" {
			t.Errorf("request body = %q, want empty", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	useTestAPIServer(t, srv)
}

// habitServer is habitServerQuery without query-parameter assertions.
func habitServer(t *testing.T, wantMethod, wantPath, wantBody, respBody string) {
	t.Helper()
	habitServerQuery(t, wantMethod, wantPath, wantBody, nil, respBody)
}

// habitRun executes the CLI with args and returns captured stdout.
// Every flag a test depends on must be passed explicitly: cobra flag values
// persist between Execute calls in the same process.
func habitRun(t *testing.T, args ...string) string {
	t.Helper()
	var runErr error
	out := captureOutput(t, func() { runErr = runCmd(t, args...) })
	if runErr != nil {
		t.Fatalf("command %v failed: %v", args, runErr)
	}
	return out
}

func TestHabitListJSON(t *testing.T) {
	habitServer(t, http.MethodGet, "/open/v1/habit", "", habitDocList)
	out := habitRun(t, "habit", "list", "--json")
	habitAssertJSON(t, out, habitDocList)
}

func TestHabitListText(t *testing.T) {
	habitServer(t, http.MethodGet, "/open/v1/habit", "", habitDocList)
	out := habitRun(t, "habit", "list")
	for _, want := range []string{"ID", "NAME", "TYPE", "GOAL", "UNIT", "REPEAT", "STATUS",
		"habit-1", "Read", "RRULE:FREQ=DAILY;INTERVAL=1", "habit-2", "Exercise"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"id"`) {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}

func TestHabitGetJSON(t *testing.T) {
	habitServer(t, http.MethodGet, "/open/v1/habit/habit-1", "", habitDocGet)
	out := habitRun(t, "habit", "get", "habit-1", "--json")
	habitAssertJSON(t, out, habitDocGet)
}

// habitLocalStamp renders an API timestamp the way FmtTime does (local zone),
// so assertions stay valid in any test-runner time zone.
func habitLocalStamp(apiTime string) string {
	t, err := time.Parse(APITimeLayout, apiTime)
	if err != nil {
		return apiTime
	}
	return t.Local().Format("2006-01-02 15:04")
}

func TestHabitGetText(t *testing.T) {
	habitServer(t, http.MethodGet, "/open/v1/habit/habit-1", "", habitDocGet)
	out := habitRun(t, "habit", "get", "habit-1")
	for _, want := range []string{
		"ID: habit-1",
		"Name: Read",
		"Type: Boolean",
		"Goal: 1",
		"Step: 1",
		"Unit: Count",
		"Color: #4D8CF5",
		"Icon: habit_reading",
		"Status: 0",
		"Total check-ins: 12",
		"Repeat rule: RRULE:FREQ=DAILY;INTERVAL=1",
		"Section: section-1",
		"Target days: 0",
		"Target start: 2024-01-01",
		"Completed cycles: 12",
		"Encouragement: Keep going",
		"Created: " + habitLocalStamp("2024-01-01T00:00:00+0000"),
		"Modified: " + habitLocalStamp("2024-01-08T00:00:00+0000"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"id"`) {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}

func TestHabitSectionsJSON(t *testing.T) {
	habitServer(t, http.MethodGet, "/open/v1/habit/sections", "", habitDocSections)
	out := habitRun(t, "habit", "sections", "--json")
	habitAssertJSON(t, out, habitDocSections)
}

func TestHabitSectionsText(t *testing.T) {
	habitServer(t, http.MethodGet, "/open/v1/habit/sections", "", habitDocSections)
	out := habitRun(t, "habit", "sections")
	for _, want := range []string{"ID", "NAME", "SORTORDER", "section-1", "Health", "12345"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"id"`) {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}

func TestHabitCreateJSON(t *testing.T) {
	habitServer(t, http.MethodPost, "/open/v1/habit",
		`{"name":"Read","goal":2}`, habitDocCreate)
	out := habitRun(t, "habit", "create", "--name", "Read", "--goal", "2", "--json")
	habitAssertJSON(t, out, habitDocCreate)
}

func TestHabitCreateDataMergeJSON(t *testing.T) {
	// --data is unmarshalled first, then explicit flags override it.
	habitServer(t, http.MethodPost, "/open/v1/habit",
		`{"name":"Read","color":"#ffffff","goal":2}`, habitDocCreate)
	out := habitRun(t, "habit", "create",
		"--name", "Read", "--goal", "2",
		"--data", `{"name":"Data name","color":"#ffffff","goal":9}`, "--json")
	habitAssertJSON(t, out, habitDocCreate)
}

func TestHabitCreateText(t *testing.T) {
	habitServer(t, http.MethodPost, "/open/v1/habit",
		`{"name":"Read","goal":2}`, habitDocCreate)
	out := habitRun(t, "habit", "create", "--name", "Read", "--goal", "2", "--data", "")
	if want := "Created habit habit-1: Read"; !strings.Contains(out, want) {
		t.Errorf("text output missing %q:\n%s", want, out)
	}
}

func TestHabitUpdateJSON(t *testing.T) {
	habitServer(t, http.MethodPost, "/open/v1/habit/habit-1",
		`{"name":"Read more","goal":2}`, habitDocUpdate)
	out := habitRun(t, "habit", "update", "habit-1",
		"--name", "Read more", "--goal", "2", "--json")
	habitAssertJSON(t, out, habitDocUpdate)
}

func TestHabitUpdateText(t *testing.T) {
	habitServer(t, http.MethodPost, "/open/v1/habit/habit-1",
		`{"name":"Read more","goal":2}`, habitDocUpdate)
	out := habitRun(t, "habit", "update", "habit-1", "--name", "Read more", "--goal", "2")
	if want := "Updated habit habit-1: Read more"; !strings.Contains(out, want) {
		t.Errorf("text output missing %q:\n%s", want, out)
	}
}

func TestHabitCheckinJSON(t *testing.T) {
	habitServer(t, http.MethodPost, "/open/v1/habit/habit-1/checkin",
		`{"stamp":20260407,"value":2,"goal":1}`, habitDocCheckin)
	out := habitRun(t, "habit", "checkin", "habit-1",
		"--stamp", "20260407", "--value", "2", "--goal", "1", "--json")
	habitAssertJSON(t, out, habitDocCheckin)
}

func TestHabitCheckinText(t *testing.T) {
	habitServer(t, http.MethodPost, "/open/v1/habit/habit-1/checkin",
		`{"stamp":20260407,"value":2,"goal":1}`, habitDocCheckin)
	out := habitRun(t, "habit", "checkin", "habit-1",
		"--stamp", "2026-04-07", "--value", "2", "--goal", "1")
	for _, want := range []string{
		"Check-in recorded for habit-1 (2026):",
		"STAMP", "VALUE", "GOAL", "STATUS",
		"2026-04-07", "1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"habitId"`) {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}

func TestHabitCheckinTimeJSON(t *testing.T) {
	// --time/--op-time are normalized to the TickTick wire format
	// "yyyy-MM-dd'T'HH:mm:ssZ" before the request is sent.
	habitServer(t, http.MethodPost, "/open/v1/habit/habit-1/checkin",
		`{"stamp":20260407,"value":2,"goal":1,"status":1,
		  "time":"2026-04-07T08:00:00+0000","opTime":"2026-04-07T09:30:00+0000"}`,
		habitDocCheckin)
	out := habitRun(t, "habit", "checkin", "habit-1",
		"--stamp", "20260407", "--value", "2", "--goal", "1", "--status", "1",
		"--time", "2026-04-07 08:00", "--op-time", "2026-04-07 09:30", "--json")
	habitAssertJSON(t, out, habitDocCheckin)
}

func TestHabitCheckinsJSON(t *testing.T) {
	habitServerQuery(t, http.MethodGet, "/open/v1/habit/checkins", "",
		map[string]string{
			"habitIds": "habit-1,habit-2",
			"from":     "20260401",
			"to":       "20260407",
		}, habitDocCheckins)
	out := habitRun(t, "habit", "checkins",
		"--ids", "habit-1,habit-2", "--from", "20260401", "--to", "20260407", "--json")
	habitAssertJSON(t, out, habitDocCheckins)
}

func TestHabitCheckinsText(t *testing.T) {
	habitServerQuery(t, http.MethodGet, "/open/v1/habit/checkins", "",
		map[string]string{
			"habitIds": "habit-1,habit-2",
			"from":     "20260401",
			"to":       "20260407",
		}, habitDocCheckins)
	out := habitRun(t, "habit", "checkins",
		"--ids", "habit-1,habit-2", "--from", "2026-04-01", "--to", "2026-04-07")
	for _, want := range []string{
		"habit habit-1 (2026):",
		"STAMP", "VALUE", "GOAL", "STATUS",
		"2026-04-07",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"habitId"`) {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}
