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

// Documented example payloads from docs/openapi.md (Column section).
const (
	columnDocList   = `[{"id":"column-1","projectId":"project-1","name":"Column Name","sortOrder":0}]`
	columnDocCreate = `{"id":"column-1","projectId":"project-1","name":"Column Name","sortOrder":0}`
	columnDocUpdate = `{"id":"column-1","projectId":"project-1","name":"New Column Name","sortOrder":0}`
)

// columnJSONEqual reports whether two JSON documents are semantically equal.
func columnJSONEqual(got, want string) (bool, string) {
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

// columnAssertJSON fails the test fatally when JSON documents differ.
func columnAssertJSON(t *testing.T, got, want string) {
	t.Helper()
	if ok, msg := columnJSONEqual(got, want); !ok {
		t.Fatalf("JSON mismatch: %s", msg)
	}
}

// columnCheckJSON reports a mismatch from inside an HTTP handler goroutine.
func columnCheckJSON(t *testing.T, got, want string) {
	t.Helper()
	if ok, msg := columnJSONEqual(got, want); !ok {
		t.Errorf("JSON mismatch: %s", msg)
	}
}

// columnServer starts a test API server that asserts the outgoing request and
// replies with respBody, then points the CLI at it for the duration of the test.
func columnServer(t *testing.T, wantMethod, wantPath, wantBody, respBody string) {
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
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if wantBody != "" {
			columnCheckJSON(t, string(body), wantBody)
		} else if strings.TrimSpace(string(body)) != "" {
			t.Errorf("request body = %q, want empty", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	useTestAPIServer(t, srv)
}

// columnRun executes the CLI with args and returns captured stdout.
func columnRun(t *testing.T, args ...string) string {
	t.Helper()
	var runErr error
	out := captureOutput(t, func() { runErr = runCmd(t, args...) })
	if runErr != nil {
		t.Fatalf("command %v failed: %v", args, runErr)
	}
	return out
}

func TestColumnListJSON(t *testing.T) {
	columnServer(t, http.MethodGet, "/open/v1/project/project-1/column", "", columnDocList)
	out := columnRun(t, "column", "list", "--project", "project-1", "--json")
	columnAssertJSON(t, out, columnDocList)
}

func TestColumnListText(t *testing.T) {
	columnServer(t, http.MethodGet, "/open/v1/project/project-1/column", "", columnDocList)
	out := columnRun(t, "column", "list", "--project", "project-1")
	for _, want := range []string{"ID", "NAME", "SORTORDER", "column-1", "Column Name", "0"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"id"`) {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}

func TestColumnCreateJSON(t *testing.T) {
	columnServer(t, http.MethodPost, "/open/v1/project/project-1/column",
		`{"name":"Column Name"}`, columnDocCreate)
	out := columnRun(t, "column", "create",
		"--project", "project-1", "--name", "Column Name", "--json")
	columnAssertJSON(t, out, columnDocCreate)
}

func TestColumnUpdateJSON(t *testing.T) {
	columnServer(t, http.MethodPost, "/open/v1/project/project-1/column/column-1",
		`{"name":"New Column Name"}`, columnDocUpdate)
	out := columnRun(t, "column", "update", "column-1",
		"--project", "project-1", "--name", "New Column Name", "--json")
	columnAssertJSON(t, out, columnDocUpdate)
}
