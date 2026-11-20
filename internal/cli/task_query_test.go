package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"ticktick-cli/api"
)

// Response payloads copied from docs/openapi.md (List Completed Tasks and
// Filter Tasks examples); the filter task is reused for undone/search, whose
// docs show no response example.
const taskQueryCompletedJSON = `[
  {
    "id": "69a850f8b9061f374d54a046",
    "projectId": "69a850f41c20d2030e148fdf",
    "sortOrder": -1099511627776,
    "title": "update",
    "content": "",
    "timeZone": "America/Los_Angeles",
    "isAllDay": false,
    "priority": 0,
    "completedTime": "2026-03-04T23:58:20.000+0000",
    "status": 2,
    "etag": "t3kc5m5f",
    "kind": "TEXT"
  }
]`

const taskQueryFilterJSON = `[
  {
    "id": "69a85785b9061f3c217e9de6",
    "projectId": "69a850f41c20d2030e148fdf",
    "sortOrder": -2199023255552,
    "title": "task1",
    "content": "",
    "desc": "",
    "startDate": "2026-03-05T00:00:00.000+0000",
    "dueDate": "2026-03-05T00:00:00.000+0000",
    "timeZone": "America/Los_Angeles",
    "isAllDay": false,
    "priority": 0,
    "status": 0,
    "tags": [
      "tag"
    ],
    "etag": "cic6e3cg",
    "kind": "TEXT"
  }
]`

// taskQueryServer starts an httptest server that asserts the POST path and the
// bearer token, decodes the JSON request body into T, and replies with
// respJSON. The decoded body is delivered on the returned channel before the
// response is written, so reading it after runCmd is race-free.
func taskQueryServer[T any](t *testing.T, wantPath, respJSON string) (*httptest.Server, <-chan T) {
	t.Helper()
	bodyCh := make(chan T, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		var body T
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		bodyCh <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respJSON)
	}))
	t.Cleanup(srv.Close)
	return srv, bodyCh
}

// taskQueryEnv points the CLI at the test server.
func taskQueryEnv(t *testing.T, srv *httptest.Server) {
	t.Helper()
	useTestAPIServer(t, srv)
}

// taskQueryBody returns the request body captured by taskQueryServer.
func taskQueryBody[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	default:
		t.Fatal("server received no request body")
		var zero T
		return zero
	}
}

func TestTaskQueryCompletedJSON(t *testing.T) {
	srv, bodyCh := taskQueryServer[api.CompletedTasksFilter](t, "/open/v1/task/completed", taskQueryCompletedJSON)
	taskQueryEnv(t, srv)

	out := captureOutput(t, func() {
		if err := runCmd(t, "task", "completed",
			"--projects", "p1,p2", "--start", "2026-03-01", "--end", "2026-03-05", "--json"); err != nil {
			t.Fatalf("task completed: %v", err)
		}
	})

	got := taskQueryBody(t, bodyCh)
	if got.ProjectIds == nil || !reflect.DeepEqual(*got.ProjectIds, []string{"p1", "p2"}) {
		t.Errorf("projectIds = %v, want [p1 p2]", got.ProjectIds)
	}
	if got.StartDate == nil || *got.StartDate != "2026-03-01T00:00:00+0000" {
		t.Errorf("startDate = %v, want 2026-03-01T00:00:00+0000", got.StartDate)
	}
	if got.EndDate == nil || *got.EndDate != "2026-03-05T00:00:00+0000" {
		t.Errorf("endDate = %v, want 2026-03-05T00:00:00+0000", got.EndDate)
	}
	if !strings.Contains(out, "69a850f8b9061f374d54a046") {
		t.Errorf("--json output missing task id:\n%s", out)
	}
	if !strings.Contains(out, `"title": "update"`) {
		t.Errorf("--json output missing pretty-printed title:\n%s", out)
	}
}

func TestTaskQueryFilterJSON(t *testing.T) {
	srv, bodyCh := taskQueryServer[api.TaskFilter](t, "/open/v1/task/filter", taskQueryFilterJSON)
	taskQueryEnv(t, srv)

	out := captureOutput(t, func() {
		if err := runCmd(t, "task", "filter",
			"--projects", "p1", "--start", "2026-03-01", "--end", "2026-03-06",
			"--priority", "0,3", "--tags", "urgent", "--kind", "TEXT,CHECKLIST",
			"--status", "0", "--json"); err != nil {
			t.Fatalf("task filter: %v", err)
		}
	})

	got := taskQueryBody(t, bodyCh)
	if got.ProjectIds == nil || !reflect.DeepEqual(*got.ProjectIds, []string{"p1"}) {
		t.Errorf("projectIds = %v, want [p1]", got.ProjectIds)
	}
	if got.StartDate == nil || *got.StartDate != "2026-03-01T00:00:00+0000" {
		t.Errorf("startDate = %v, want 2026-03-01T00:00:00+0000", got.StartDate)
	}
	if got.EndDate == nil || *got.EndDate != "2026-03-06T00:00:00+0000" {
		t.Errorf("endDate = %v, want 2026-03-06T00:00:00+0000", got.EndDate)
	}
	if got.Priority == nil || !reflect.DeepEqual(*got.Priority, []int32{0, 3}) {
		t.Errorf("priority = %v, want [0 3]", got.Priority)
	}
	if got.Tag == nil || !reflect.DeepEqual(*got.Tag, []string{"urgent"}) {
		t.Errorf("tag = %v, want [urgent]", got.Tag)
	}
	if got.Kind == nil || !reflect.DeepEqual(*got.Kind, []string{"TEXT", "CHECKLIST"}) {
		t.Errorf("kind = %v, want [TEXT CHECKLIST]", got.Kind)
	}
	if got.Status == nil || !reflect.DeepEqual(*got.Status, []int32{0}) {
		t.Errorf("status = %v, want [0]", got.Status)
	}
	if !strings.Contains(out, "69a85785b9061f3c217e9de6") {
		t.Errorf("--json output missing task id:\n%s", out)
	}
}

func TestTaskQueryUndoneJSON(t *testing.T) {
	srv, bodyCh := taskQueryServer[api.UndoneTasksFilter](t, "/open/v1/task/undone", taskQueryFilterJSON)
	taskQueryEnv(t, srv)

	out := captureOutput(t, func() {
		if err := runCmd(t, "task", "undone",
			"--start", "2026-07-01", "--end", "2026-07-14", "--projects", "inbox",
			"--ids", "t1,t2", "--json"); err != nil {
			t.Fatalf("task undone: %v", err)
		}
	})

	got := taskQueryBody(t, bodyCh)
	if got.StartDate != "2026-07-01T00:00:00+0000" {
		t.Errorf("startDate = %q, want 2026-07-01T00:00:00+0000", got.StartDate)
	}
	if got.EndDate != "2026-07-14T00:00:00+0000" {
		t.Errorf("endDate = %q, want 2026-07-14T00:00:00+0000", got.EndDate)
	}
	if got.ProjectIds == nil || !reflect.DeepEqual(*got.ProjectIds, []string{"inbox"}) {
		t.Errorf("projectIds = %v, want [inbox]", got.ProjectIds)
	}
	if got.TaskIds == nil || !reflect.DeepEqual(*got.TaskIds, []string{"t1", "t2"}) {
		t.Errorf("taskIds = %v, want [t1 t2]", got.TaskIds)
	}
	if !strings.Contains(out, "69a85785b9061f3c217e9de6") {
		t.Errorf("--json output missing task id:\n%s", out)
	}
}

func TestTaskQuerySearchJSON(t *testing.T) {
	srv, bodyCh := taskQueryServer[api.TaskSearchFilter](t, "/open/v1/task/search", taskQueryFilterJSON)
	taskQueryEnv(t, srv)

	out := captureOutput(t, func() {
		if err := runCmd(t, "task", "search", "quarterly report",
			"--projects", "p1", "--tags", "work", "--status", "0",
			"--due-from", "2026-07-01", "--due-to", "2026-07-31", "--json"); err != nil {
			t.Fatalf("task search: %v", err)
		}
	})

	got := taskQueryBody(t, bodyCh)
	if got.Keywords == nil || *got.Keywords != "quarterly report" {
		t.Errorf("keywords = %v, want %q", got.Keywords, "quarterly report")
	}
	if got.ProjectIds == nil || !reflect.DeepEqual(*got.ProjectIds, []string{"p1"}) {
		t.Errorf("projectIds = %v, want [p1]", got.ProjectIds)
	}
	if got.Tags == nil || !reflect.DeepEqual(*got.Tags, []string{"work"}) {
		t.Errorf("tags = %v, want [work]", got.Tags)
	}
	if got.Status == nil || !reflect.DeepEqual(*got.Status, []int32{0}) {
		t.Errorf("status = %v, want [0]", got.Status)
	}
	if got.DueFrom == nil || *got.DueFrom != "2026-07-01T00:00:00+0000" {
		t.Errorf("dueFrom = %v, want 2026-07-01T00:00:00+0000", got.DueFrom)
	}
	if got.DueTo == nil || *got.DueTo != "2026-07-31T00:00:00+0000" {
		t.Errorf("dueTo = %v, want 2026-07-31T00:00:00+0000", got.DueTo)
	}
	if !strings.Contains(out, "69a85785b9061f3c217e9de6") {
		t.Errorf("--json output missing task id:\n%s", out)
	}
}

func TestTaskQueryFilterText(t *testing.T) {
	srv, bodyCh := taskQueryServer[api.TaskFilter](t, "/open/v1/task/filter", taskQueryFilterJSON)
	taskQueryEnv(t, srv)

	out := captureOutput(t, func() {
		if err := runCmd(t, "task", "filter",
			"--projects", "p1", "--start", "2026-03-01", "--end", "2026-03-06",
			"--priority", "", "--tags", "", "--kind", "", "--status", ""); err != nil {
			t.Fatalf("task filter (text): %v", err)
		}
	})

	got := taskQueryBody(t, bodyCh)
	if got.Priority != nil || got.Tag != nil || got.Kind != nil || got.Status != nil {
		t.Errorf("empty list flags must leave fields nil: priority=%v tag=%v kind=%v status=%v",
			got.Priority, got.Tag, got.Kind, got.Status)
	}
	if !strings.Contains(out, "TITLE") {
		t.Errorf("text output missing table header:\n%s", out)
	}
	if !strings.Contains(out, "task1") {
		t.Errorf("text output missing task title:\n%s", out)
	}
	if strings.Contains(out, "[{") {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}

func TestTaskQueryUndoneRequiresRange(t *testing.T) {
	t.Setenv("TICKTICK_TOKEN", "test-token")
	var err error
	_ = captureOutput(t, func() {
		err = runCmd(t, "task", "undone", "--start", "", "--end", "", "--projects", "", "--ids", "")
	})
	if err == nil {
		t.Fatal("task undone without --start/--end: expected error")
	}
	if !strings.Contains(err.Error(), "--start and --end are required") {
		t.Errorf("error = %v, want mention of --start/--end", err)
	}
}

func TestTaskQueryFilterInvalidPriority(t *testing.T) {
	t.Setenv("TICKTICK_TOKEN", "test-token")
	var err error
	_ = captureOutput(t, func() {
		err = runCmd(t, "task", "filter",
			"--priority", "high", "--projects", "", "--start", "", "--end",
			"", "--tags", "", "--kind", "", "--status", "")
	})
	if err == nil {
		t.Fatal("task filter --priority high: expected error")
	}
	if !strings.Contains(err.Error(), "--priority") {
		t.Errorf("error = %v, want mention of --priority", err)
	}
}
