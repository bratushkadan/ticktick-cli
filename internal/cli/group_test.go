package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Documented example payloads from docs/openapi.md (Project Group section).
const (
	groupDocList   = `[{"id":"group-1","name":"Group Name","sortOrder":0,"showAll":true,"viewMode":"list"}]`
	groupDocCreate = `{"id":"group-1","name":"Group Name","sortOrder":0}`
	groupDocUpdate = `{"id":"group-1","name":"New Group Name","sortOrder":0}`
)

// groupJSONEqual reports whether two JSON documents are semantically equal.
func groupJSONEqual(got, want string) (bool, string) {
	var gotV, wantV any
	if err := json.Unmarshal([]byte(got), &gotV); err != nil {
		return false, "stdout is not valid JSON: " + err.Error() + "\nstdout:\n" + got
	}
	if err := json.Unmarshal([]byte(want), &wantV); err != nil {
		return false, "expected JSON in test is invalid: " + err.Error()
	}
	if !reflect.DeepEqual(gotV, wantV) {
		return false, "\n got: " + strings.TrimSpace(got) + "\nwant: " + want
	}
	return true, ""
}

// groupAssertJSON fails the test fatally when JSON documents differ.
func groupAssertJSON(t *testing.T, got, want string) {
	t.Helper()
	if ok, msg := groupJSONEqual(got, want); !ok {
		t.Fatalf("JSON mismatch: %s", msg)
	}
}

// groupCheckJSON reports a mismatch from inside an HTTP handler goroutine.
func groupCheckJSON(t *testing.T, got, want string) {
	t.Helper()
	if ok, msg := groupJSONEqual(got, want); !ok {
		t.Errorf("JSON mismatch: %s", msg)
	}
}

// groupServer starts a test API server that asserts the outgoing request and
// replies with respBody, then points the CLI at it for the duration of the test.
func groupServer(t *testing.T, wantMethod, wantPath, wantBody, respBody string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != wantMethod {
			t.Errorf("method = %s, want %s", r.Method, wantMethod)
		}
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if wantBody != "" {
			groupCheckJSON(t, string(body), wantBody)
		} else if strings.TrimSpace(string(body)) != "" {
			t.Errorf("request body = %q, want empty", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TICKTICK_BASE_URL", srv.URL)
	t.Setenv("TICKTICK_TOKEN", "test-token")
}

// groupRun executes the CLI with args and returns captured stdout.
func groupRun(t *testing.T, args ...string) string {
	t.Helper()
	var runErr error
	out := captureOutput(t, func() { runErr = runCmd(t, args...) })
	if runErr != nil {
		t.Fatalf("command %v failed: %v", args, runErr)
	}
	return out
}

func TestGroupListJSON(t *testing.T) {
	groupServer(t, http.MethodGet, "/open/v1/project/group", "", groupDocList)
	out := groupRun(t, "group", "list", "--json")
	groupAssertJSON(t, out, groupDocList)
}

func TestGroupListText(t *testing.T) {
	groupServer(t, http.MethodGet, "/open/v1/project/group", "", groupDocList)
	out := groupRun(t, "group", "list", "--token", "test-token")
	for _, want := range []string{"ID", "NAME", "SORTORDER", "SHOWALL", "VIEWMODE", "group-1", "Group Name", "true", "list"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"id"`) {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}

func TestGroupCreateJSON(t *testing.T) {
	groupServer(t, http.MethodPost, "/open/v1/project/group",
		`{"name":"Group Name"}`, groupDocCreate)
	out := groupRun(t, "group", "create", "--name", "Group Name", "--json")
	groupAssertJSON(t, out, groupDocCreate)
}

func TestGroupUpdateJSON(t *testing.T) {
	groupServer(t, http.MethodPost, "/open/v1/project/group/group-1",
		`{"name":"New Group Name"}`, groupDocUpdate)
	out := groupRun(t, "group", "update", "group-1", "--name", "New Group Name", "--json")
	groupAssertJSON(t, out, groupDocUpdate)
}

func TestGroupDeleteJSON(t *testing.T) {
	groupServer(t, http.MethodDelete, "/open/v1/project/group/group-1", "", "")
	out := groupRun(t, "group", "delete", "group-1", "--json")
	groupAssertJSON(t, out, `{"ok":true}`)
}

func TestGroupDeleteText(t *testing.T) {
	groupServer(t, http.MethodDelete, "/open/v1/project/group/group-1", "", "")
	out := groupRun(t, "group", "delete", "group-1")
	if got := strings.TrimSpace(out); got != "Project group deleted." {
		t.Fatalf("text output = %q, want %q", got, "Project group deleted.")
	}
}

func TestGroupCreateRejectsLongName(t *testing.T) {
	// Validation happens before any HTTP request, so no server is needed.
	t.Setenv("TICKTICK_TOKEN", "test-token")
	var runErr error
	_ = captureOutput(t, func() {
		runErr = runCmd(t, "group", "create", "--name", strings.Repeat("x", 65))
	})
	if runErr == nil {
		t.Fatal("expected error for a name longer than 64 chars")
	}
	if !strings.Contains(runErr.Error(), "64") {
		t.Fatalf("error = %v, want it to mention the 64 char limit", runErr)
	}
}
