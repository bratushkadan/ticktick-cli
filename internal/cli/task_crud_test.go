package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// taskCrudSampleTaskJSON mirrors the official docs example Task response.
const taskCrudSampleTaskJSON = `{
  "id": "63b7bebb91c0a5474805fcd4",
  "projectId": "6226ff9877acee87727f6bca",
  "title": "Task Title",
  "content": "Task Content",
  "desc": "Task Description",
  "isAllDay": true,
  "startDate": "2019-11-13T03:00:00+0000",
  "dueDate": "2019-11-14T03:00:00+0000",
  "timeZone": "America/Los_Angeles",
  "repeatFlag": "RRULE:FREQ=DAILY;INTERVAL=1",
  "reminders": ["TRIGGER:P0DT9H0M0S", "TRIGGER:PT0S"],
  "tags": ["work", "urgent"],
  "priority": 1,
  "status": 0,
  "completedTime": "2019-11-13T03:00:00+0000",
  "sortOrder": 12345,
  "parentId": "63b7bebb91c0a5474805fcd0",
  "kind": "CHECKLIST",
  "items": [
    {"id": "6435074647fd2e6387145f20", "status": 0, "title": "Item A"},
    {"id": "6435074647fd2e6387145f21", "status": 1, "title": "Item B"}
  ]
}`

// taskCrudServer starts an httptest server that asserts bearer auth, method
// and path, then delegates the response to fn.
func taskCrudServer(t *testing.T, method, path string, fn func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-token")
		}
		if r.Method != method {
			t.Errorf("method = %s, want %s", r.Method, method)
		}
		if r.URL.Path != path {
			t.Errorf("path = %s, want %s", r.URL.Path, path)
		}
		fn(w, r)
	}))
	t.Cleanup(srv.Close)
	useTestAPIServer(t, srv)
}

// taskCrudWriteJSON writes the standard 200 JSON task response.
func taskCrudWriteJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, taskCrudSampleTaskJSON)
}

// taskCrudCaptureBody decodes and returns the JSON request body.
func taskCrudCaptureBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return body
}

func taskCrudBodyString(t *testing.T, body map[string]any, key, want string) {
	t.Helper()
	got, ok := body[key]
	if !ok {
		t.Errorf("request body missing %q; body = %v", key, body)
		return
	}
	if got != want {
		t.Errorf("body[%q] = %v, want %q", key, got, want)
	}
}

func taskCrudBodyStrings(t *testing.T, body map[string]any, key string, want []string) {
	t.Helper()
	raw, ok := body[key]
	if !ok {
		t.Errorf("request body missing %q; body = %v", key, body)
		return
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Errorf("body[%q] = %v (%T), want array", key, raw, raw)
		return
	}
	got := make([]string, 0, len(arr))
	for _, v := range arr {
		s, _ := v.(string)
		got = append(got, s)
	}
	if !slices.Equal(got, want) {
		t.Errorf("body[%q] = %v, want %v", key, got, want)
	}
}

func taskCrudBodyNumber(t *testing.T, body map[string]any, key string, want float64) {
	t.Helper()
	got, ok := body[key]
	if !ok {
		t.Errorf("request body missing %q; body = %v", key, body)
		return
	}
	n, ok := got.(float64)
	if !ok {
		t.Errorf("body[%q] = %v (%T), want number", key, got, got)
		return
	}
	if n != want {
		t.Errorf("body[%q] = %v, want %v", key, n, want)
	}
}

func taskCrudBodyItemTitles(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["items"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("body[items] = %v (%T), want array", raw, raw)
	}
	titles := make([]string, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("items entry = %v (%T), want object", it, it)
		}
		s, _ := m["title"].(string)
		titles = append(titles, s)
	}
	return titles
}

func taskCrudWantContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestTaskCrudGetJSON(t *testing.T) {
	taskCrudServer(t, http.MethodGet, "/open/v1/project/p1/task/t1",
		func(w http.ResponseWriter, r *http.Request) { taskCrudWriteJSON(w) })

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "get", "t1", "--project", "p1", "--json")
	})
	if err != nil {
		t.Fatalf("task get --json: %v", err)
	}
	taskCrudWantContains(t, out,
		`"id": "63b7bebb91c0a5474805fcd4"`,
		`"title": "Task Title"`,
		`"status_slug": "open"`,
		`"status_slug": "completed_subtask"`,
		`"tags": [`,
		`"priority": 1`,
	)
}

func TestTaskCrudGetText(t *testing.T) {
	taskCrudServer(t, http.MethodGet, "/open/v1/project/p1/task/t1",
		func(w http.ResponseWriter, r *http.Request) { taskCrudWriteJSON(w) })

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "get", "t1", "--project", "p1")
	})
	if err != nil {
		t.Fatalf("task get: %v", err)
	}
	// Dates are rendered in local time, so only the month is asserted.
	taskCrudWantContains(t, out,
		"ID: 63b7bebb91c0a5474805fcd4",
		"Title: Task Title",
		"Status: open",
		"Priority: low",
		"Project: 6226ff9877acee87727f6bca",
		"Start: 2019-11-",
		"Due: 2019-11-",
		"Time zone: America/Los_Angeles",
		"Kind: CHECKLIST",
		"Parent: 63b7bebb91c0a5474805fcd0",
		"Content: Task Content",
		"Desc: Task Description",
		"Tags: work, urgent",
		"Repeat: RRULE:FREQ=DAILY;INTERVAL=1",
		"Checklist:",
		"  [ ] Item A",
		"  [x] Item B",
	)
}

// taskCrudClearChangedFlags marks every flag of cmd as "not changed" so a
// test run does not inherit flag state from earlier Execute calls on the same
// command instance (cobra/pflag keep Changed between executions).
func taskCrudClearChangedFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	cmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
}

// TestTaskCrudCreateDataMerge checks the --data merge order: the raw JSON is
// applied first and explicit flags win. Flag state is cleared first so the
// assertion does not depend on test execution order.
func TestTaskCrudCreateDataMerge(t *testing.T) {
	var got map[string]any
	taskCrudServer(t, http.MethodPost, "/open/v1/task", func(w http.ResponseWriter, r *http.Request) {
		got = taskCrudCaptureBody(t, r)
		taskCrudWriteJSON(w)
	})

	taskCrudClearChangedFlags(t, taskCrudCreateCmd)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "create",
			"--title", "Flag Title",
			"--project", "p1",
			"--content", "flag content",
			"--data", `{"title":"Data Title","projectId":"pdata","content":"data content","desc":"data desc","tags":["data-tag"],"priority":5}`,
		)
	})
	if err != nil {
		t.Fatalf("task create --data: %v", err)
	}
	if got == nil {
		t.Fatal("server received no request body")
	}
	// Fields only present in --data survive the merge.
	taskCrudBodyString(t, got, "desc", "data desc")
	taskCrudBodyStrings(t, got, "tags", []string{"data-tag"})
	taskCrudBodyNumber(t, got, "priority", 5)
	// Explicit flags win over --data.
	taskCrudBodyString(t, got, "title", "Flag Title")
	taskCrudBodyString(t, got, "projectId", "p1")
	taskCrudBodyString(t, got, "content", "flag content")
	taskCrudWantContains(t, out, "Created task 63b7bebb91c0a5474805fcd4: Task Title")
}

func TestTaskCrudCreate(t *testing.T) {
	var got map[string]any
	taskCrudServer(t, http.MethodPost, "/open/v1/task", func(w http.ResponseWriter, r *http.Request) {
		got = taskCrudCaptureBody(t, r)
		taskCrudWriteJSON(w)
	})

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "create",
			"--title", "Buy milk",
			"--project", "p1",
			"--content", "2 litres",
			"--desc", "shopping",
			"--all-day=false",
			"--start", "2026-01-02 03:04",
			"--due", "2026-01-03",
			"--tz", "UTC",
			"--reminder", "TRIGGER:PT0S",
			"--tag", "work",
			"--tag", "urgent",
			"--repeat", "RRULE:FREQ=DAILY;INTERVAL=1",
			"--repeat-from", "2",
			"--priority", "3",
			"--sort-order", "7",
			"--parent", "par1",
			"--kind", "CHECKLIST",
			"--item", "Sub A",
			"--item", "Sub B",
			"--data", "",
		)
	})
	if err != nil {
		t.Fatalf("task create: %v", err)
	}
	if got == nil {
		t.Fatal("server received no request body")
	}
	taskCrudBodyString(t, got, "title", "Buy milk")
	taskCrudBodyString(t, got, "projectId", "p1")
	taskCrudBodyString(t, got, "content", "2 litres")
	taskCrudBodyString(t, got, "desc", "shopping")
	taskCrudBodyString(t, got, "startDate", "2026-01-02T03:04:00+0000")
	taskCrudBodyString(t, got, "dueDate", "2026-01-03T00:00:00+0000")
	taskCrudBodyString(t, got, "timeZone", "UTC")
	taskCrudBodyString(t, got, "repeatFlag", "RRULE:FREQ=DAILY;INTERVAL=1")
	taskCrudBodyString(t, got, "repeatFrom", "2")
	taskCrudBodyString(t, got, "parentId", "par1")
	taskCrudBodyString(t, got, "kind", "CHECKLIST")
	taskCrudBodyStrings(t, got, "reminders", []string{"TRIGGER:PT0S"})
	taskCrudBodyStrings(t, got, "tags", []string{"work", "urgent"})
	taskCrudBodyNumber(t, got, "priority", 3)
	taskCrudBodyNumber(t, got, "sortOrder", 7)
	if v, ok := got["isAllDay"]; !ok || v != false {
		t.Errorf("body[isAllDay] = %v (present=%v), want false", v, ok)
	}
	if titles := taskCrudBodyItemTitles(t, got); !slices.Equal(titles, []string{"Sub A", "Sub B"}) {
		t.Errorf("body[items] titles = %v, want [Sub A Sub B]", titles)
	}
	taskCrudWantContains(t, out,
		"Created task 63b7bebb91c0a5474805fcd4: Task Title",
		"Tags: work, urgent",
	)
}

func TestTaskCrudUpdate(t *testing.T) {
	var got map[string]any
	taskCrudServer(t, http.MethodPost, "/open/v1/task/t1", func(w http.ResponseWriter, r *http.Request) {
		got = taskCrudCaptureBody(t, r)
		taskCrudWriteJSON(w)
	})

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "update", "t1",
			"--project", "p1",
			"--title", "New Title",
			"--content", "new content",
			"--desc", "new desc",
			"--all-day=true",
			"--start", "2026-02-02",
			"--due", "2026-02-03 10:00",
			"--tz", "Europe/Berlin",
			"--reminder", "TRIGGER:P0DT9H0M0S",
			"--tag", "x",
			"--repeat", "RRULE:FREQ=WEEKLY",
			"--repeat-from", "1",
			"--priority", "5",
			"--sort-order", "9",
			"--parent", "par2",
			"--kind", "NOTE",
			"--item", "Sub C",
			"--data", "",
		)
	})
	if err != nil {
		t.Fatalf("task update: %v", err)
	}
	if got == nil {
		t.Fatal("server received no request body")
	}
	taskCrudBodyString(t, got, "id", "t1")
	taskCrudBodyString(t, got, "projectId", "p1")
	taskCrudBodyString(t, got, "title", "New Title")
	taskCrudBodyString(t, got, "content", "new content")
	taskCrudBodyString(t, got, "desc", "new desc")
	taskCrudBodyString(t, got, "startDate", "2026-02-02T00:00:00+0000")
	taskCrudBodyString(t, got, "dueDate", "2026-02-03T10:00:00+0000")
	taskCrudBodyString(t, got, "timeZone", "Europe/Berlin")
	taskCrudBodyString(t, got, "repeatFlag", "RRULE:FREQ=WEEKLY")
	taskCrudBodyString(t, got, "repeatFrom", "1")
	taskCrudBodyString(t, got, "parentId", "par2")
	taskCrudBodyString(t, got, "kind", "NOTE")
	taskCrudBodyStrings(t, got, "reminders", []string{"TRIGGER:P0DT9H0M0S"})
	taskCrudBodyStrings(t, got, "tags", []string{"x"})
	taskCrudBodyNumber(t, got, "priority", 5)
	taskCrudBodyNumber(t, got, "sortOrder", 9)
	if v, ok := got["isAllDay"]; !ok || v != true {
		t.Errorf("body[isAllDay] = %v (present=%v), want true", v, ok)
	}
	if titles := taskCrudBodyItemTitles(t, got); !slices.Equal(titles, []string{"Sub C"}) {
		t.Errorf("body[items] titles = %v, want [Sub C]", titles)
	}
	taskCrudWantContains(t, out, "Updated task 63b7bebb91c0a5474805fcd4: Task Title")
}

func TestTaskCrudComplete(t *testing.T) {
	calls := 0
	taskCrudServer(t, http.MethodPost, "/open/v1/project/p1/task/t1/complete",
		func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		})

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "complete", "t1", "--project", "p1")
	})
	if err != nil {
		t.Fatalf("task complete: %v", err)
	}
	taskCrudWantContains(t, out, "Task completed.")

	jsonOut := captureOutput(t, func() {
		err = runCmd(t, "task", "complete", "t1", "--project", "p1", "--json")
	})
	if err != nil {
		t.Fatalf("task complete --json: %v", err)
	}
	taskCrudWantContains(t, jsonOut, `{"ok":true}`)
	if calls != 2 {
		t.Errorf("server got %d requests, want 2", calls)
	}
}

func TestTaskCrudDelete(t *testing.T) {
	calls := 0
	taskCrudServer(t, http.MethodDelete, "/open/v1/project/p1/task/t1",
		func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		})

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "delete", "t1", "--project", "p1")
	})
	if err != nil {
		t.Fatalf("task delete: %v", err)
	}
	taskCrudWantContains(t, out, "Task deleted.")

	jsonOut := captureOutput(t, func() {
		err = runCmd(t, "task", "delete", "t1", "--project", "p1", "--json")
	})
	if err != nil {
		t.Fatalf("task delete --json: %v", err)
	}
	taskCrudWantContains(t, jsonOut, `{"ok":true}`)
	if calls != 2 {
		t.Errorf("server got %d requests, want 2", calls)
	}
}
