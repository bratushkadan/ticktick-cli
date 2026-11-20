package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ticktick-cli/api"
)

// focusTestServer starts an httptest server wired into the CLI env and
// returns it. Handlers assert method/path/query themselves.
func focusTestServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	useTestAPIServer(t, srv)
	return srv
}

// focusWriteJSON writes a JSON response body.
func focusWriteJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// focusRunOK runs the CLI and fails the test on error, returning stdout.
func focusRunOK(t *testing.T, args ...string) string {
	t.Helper()
	var out string
	var runErr error
	out = captureOutput(t, func() { runErr = runCmd(t, args...) })
	if runErr != nil {
		t.Fatalf("runCmd(%v) returned error: %v", args, runErr)
	}
	return out
}

const focusGetBody = `{
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
}`

const focusListBody = `[
  {
    "id": "focus-1",
    "type": 1,
    "note": "Timing",
    "startTime": "2026-04-01T09:00:00+0800",
    "endTime": "2026-04-01T10:30:00+0800",
    "duration": 5400
  }
]`

const focusCreateBody = `{
  "id": "focus-1",
  "type": 0,
  "taskId": "task-1",
  "startTime": "2026-04-07T09:00:00+0800",
  "endTime": "2026-04-07T09:25:00+0800",
  "duration": 1500
}`

const focusDeleteBody = `{
  "id": "focus-1",
  "type": 0
}`

func TestFocusGetJSON(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/open/v1/focus/focus-1" {
			t.Errorf("path = %q, want /open/v1/focus/focus-1", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("type"); got != "0" {
			t.Errorf("type = %q, want 0", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		focusWriteJSON(w, focusGetBody)
	})

	out := focusRunOK(t, "focus", "get", "focus-1", "--type", "0", "--json")
	for _, want := range []string{`"id": "focus-1"`, `"note": "Deep work"`, `"duration": 1500`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %q:\n%s", want, out)
		}
	}
}

func TestFocusGetText(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open/v1/focus/focus-1" {
			t.Errorf("path = %q, want /open/v1/focus/focus-1", r.URL.Path)
		}
		if got := r.URL.Query().Get("type"); got != "0" {
			t.Errorf("type = %q, want 0", got)
		}
		focusWriteJSON(w, focusGetBody)
	})

	out := focusRunOK(t, "focus", "get", "focus-1", "--type", "0")
	for _, want := range []string{
		"ID: focus-1",
		"Type: pomodoro",
		"Task: task-1",
		"Status: 2",
		"Start: 2026-04-07",
		"End: 2026-04-07",
		"Duration(s): 1500",
		"Pause(s): 0",
		"Note: Deep work",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestFocusListJSON(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/open/v1/focus" {
			t.Errorf("path = %q, want /open/v1/focus", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("from"); got != "2026-04-01T00:00:00+0000" {
			t.Errorf("from = %q, want 2026-04-01T00:00:00+0000", got)
		}
		if got := q.Get("to"); got != "2026-04-02T00:00:00+0000" {
			t.Errorf("to = %q, want 2026-04-02T00:00:00+0000", got)
		}
		if got := q.Get("type"); got != "1" {
			t.Errorf("type = %q, want 1", got)
		}
		focusWriteJSON(w, focusListBody)
	})

	out := focusRunOK(t, "focus", "list",
		"--from", "2026-04-01", "--to", "2026-04-02", "--type", "1", "--json")
	for _, want := range []string{`"id": "focus-1"`, `"duration": 5400`, `"type": 1`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %q:\n%s", want, out)
		}
	}
}

func TestFocusListText(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open/v1/focus" {
			t.Errorf("path = %q, want /open/v1/focus", r.URL.Path)
		}
		focusWriteJSON(w, focusListBody)
	})

	out := focusRunOK(t, "focus", "list",
		"--from", "2026-04-01T00:00:00+0800", "--to", "2026-04-02T00:00:00+0800", "--type", "1")
	for _, want := range []string{"ID", "DURATION(s)", "NOTE", "focus-1", "timing", "5400", "Timing"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestFocusCreateJSON(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/open/v1/focus" {
			t.Errorf("path = %q, want /open/v1/focus", r.URL.Path)
		}
		var req api.FocusCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}
		if req.Type != 0 {
			t.Errorf("type = %d, want 0", req.Type)
		}
		if got := Str(req.TaskId); got != "task-1" {
			t.Errorf("taskId = %q, want task-1", got)
		}
		if got := Str(req.StartTime); got != "2026-04-07T09:00:00+0800" {
			t.Errorf("startTime = %q, want 2026-04-07T09:00:00+0800", got)
		}
		if got := Str(req.EndTime); got != "2026-04-07T09:25:00+0800" {
			t.Errorf("endTime = %q, want 2026-04-07T09:25:00+0800", got)
		}
		if got := I32(req.PauseDuration); got != 0 {
			t.Errorf("pauseDuration = %d, want 0", got)
		}
		if got := I64(req.Duration); got != 1500 {
			t.Errorf("duration = %d, want 1500", got)
		}
		if req.RelationType != nil {
			t.Errorf("relationType = %v, want omitted", *req.RelationType)
		}
		focusWriteJSON(w, focusCreateBody)
	})

	out := focusRunOK(t, "focus", "create",
		"--type", "0",
		"--task", "task-1",
		"--note", "",
		"--start", "2026-04-07T09:00:00+0800",
		"--end", "2026-04-07T09:25:00+0800",
		"--pause", "0",
		"--duration", "1500",
		"--relation", "",
		"--data", "",
		"--json")
	for _, want := range []string{`"id": "focus-1"`, `"duration": 1500`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %q:\n%s", want, out)
		}
	}
}

func TestFocusCreateText(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/open/v1/focus" {
			t.Errorf("path = %q, want /open/v1/focus", r.URL.Path)
		}
		focusWriteJSON(w, focusCreateBody)
	})

	out := focusRunOK(t, "focus", "create",
		"--type", "0",
		"--task", "",
		"--note", "",
		"--start", "",
		"--end", "",
		"--pause", "0",
		"--duration", "1500",
		"--relation", "",
		"--data", "")
	if want := "Created focus focus-1 (type pomodoro)\n"; out != want {
		t.Errorf("text output = %q, want %q", out, want)
	}
}

func TestFocusCreateDataOverrides(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req api.FocusCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}
		// Explicit flags win over --data.
		if req.Type != 1 {
			t.Errorf("type = %d, want 1 (flag overrides --data)", req.Type)
		}
		if got := I64(req.Duration); got != 60 {
			t.Errorf("duration = %d, want 60 (flag overrides --data)", got)
		}
		if got := Str(req.Note); got != "" {
			t.Errorf("note = %q, want empty (flag overrides --data)", got)
		}
		// Empty --relation does not clear relationType supplied by --data.
		if req.RelationType == nil || len(*req.RelationType) != 1 || (*req.RelationType)[0] != 1 {
			t.Errorf("relationType = %v, want [1] from --data", req.RelationType)
		}
		focusWriteJSON(w, focusCreateBody)
	})

	out := focusRunOK(t, "focus", "create",
		"--data", `{"type":0,"taskId":"task-9","note":"from-data","duration":99,"relationType":[1]}`,
		"--type", "1",
		"--task", "",
		"--note", "",
		"--start", "",
		"--end", "",
		"--pause", "0",
		"--duration", "60",
		"--relation", "",
		"--json")
	if !strings.Contains(out, `"id": "focus-1"`) {
		t.Errorf("json output missing focus id:\n%s", out)
	}
}

func TestFocusCreateRelationList(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req api.FocusCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}
		if req.RelationType == nil {
			t.Errorf("relationType = nil, want [0 1]")
			return
		}
		got := *req.RelationType
		if len(got) != 2 || got[0] != 0 || got[1] != 1 {
			t.Errorf("relationType = %v, want [0 1]", got)
		}
		focusWriteJSON(w, focusCreateBody)
	})

	focusRunOK(t, "focus", "create",
		"--type", "0",
		"--task", "",
		"--note", "",
		"--start", "",
		"--end", "",
		"--pause", "0",
		"--duration", "0",
		"--relation", "0,1",
		"--data", "",
		"--json")
}

func TestFocusDeleteJSON(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/open/v1/focus/focus-1" {
			t.Errorf("path = %q, want /open/v1/focus/focus-1", r.URL.Path)
		}
		if got := r.URL.Query().Get("type"); got != "0" {
			t.Errorf("type = %q, want 0", got)
		}
		focusWriteJSON(w, focusDeleteBody)
	})

	out := focusRunOK(t, "focus", "delete", "focus-1", "--type", "0", "--json")
	// The endpoint returns an OpenFocus body: Render, not RenderOK.
	if !strings.Contains(out, `"id": "focus-1"`) {
		t.Errorf("json output missing focus id:\n%s", out)
	}
	if strings.Contains(out, `"ok": true`) {
		t.Errorf("json output must be the API body, not {\"ok\":true}:\n%s", out)
	}
}

func TestFocusDeleteText(t *testing.T) {
	focusTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if got := r.URL.Query().Get("type"); got != "1" {
			t.Errorf("type = %q, want 1", got)
		}
		focusWriteJSON(w, focusDeleteBody)
	})

	out := focusRunOK(t, "focus", "delete", "focus-1", "--type", "1")
	if want := "Deleted focus focus-1\n"; out != want {
		t.Errorf("text output = %q, want %q", out, want)
	}
}
