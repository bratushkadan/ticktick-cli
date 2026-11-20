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

// Documented example payloads from docs/openapi.md (Tag section).
const (
	tagDocList   = `[{"name":"work","label":"work","sortOrder":0,"color":"#F18181","type":1}]`
	tagDocCreate = `{"name":"urgent","label":"urgent","sortOrder":0,"type":1}`
)

// tagJSONEqual reports whether two JSON documents are semantically equal.
func tagJSONEqual(got, want string) (bool, string) {
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

// tagAssertJSON fails the test fatally when JSON documents differ.
func tagAssertJSON(t *testing.T, got, want string) {
	t.Helper()
	if ok, msg := tagJSONEqual(got, want); !ok {
		t.Fatalf("JSON mismatch: %s", msg)
	}
}

// tagCheckJSON reports a mismatch from inside an HTTP handler goroutine.
func tagCheckJSON(t *testing.T, got, want string) {
	t.Helper()
	if ok, msg := tagJSONEqual(got, want); !ok {
		t.Errorf("JSON mismatch: %s", msg)
	}
}

// tagServer starts a test API server that asserts the outgoing request and
// replies with respBody, then points the CLI at it for the duration of the test.
func tagServer(t *testing.T, wantMethod, wantPath, wantBody, respBody string) {
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
			tagCheckJSON(t, string(body), wantBody)
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

// tagRun executes the CLI with args and returns captured stdout.
func tagRun(t *testing.T, args ...string) string {
	t.Helper()
	var runErr error
	out := captureOutput(t, func() { runErr = runCmd(t, args...) })
	if runErr != nil {
		t.Fatalf("command %v failed: %v", args, runErr)
	}
	return out
}

func TestTagListJSON(t *testing.T) {
	tagServer(t, http.MethodGet, "/open/v1/tag", "", tagDocList)
	out := tagRun(t, "tag", "list", "--json")
	tagAssertJSON(t, out, tagDocList)
}

func TestTagListText(t *testing.T) {
	tagServer(t, http.MethodGet, "/open/v1/tag", "", tagDocList)
	out := tagRun(t, "tag", "list")
	for _, want := range []string{"NAME", "LABEL", "COLOR", "TYPE", "PARENT", "work", "#F18181", "personal"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"name"`) {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}

func TestTagCreateJSON(t *testing.T) {
	tagServer(t, http.MethodPost, "/open/v1/tag",
		`{"name":"urgent","label":"urgent"}`, tagDocCreate)
	out := tagRun(t, "tag", "create", "--name", "urgent", "--label", "urgent", "--json")
	tagAssertJSON(t, out, tagDocCreate)
}

func TestTagCreateNormalizesName(t *testing.T) {
	// Names are trimmed and lowercased; the label only has to match when
	// lowercased, so it is sent with its original casing.
	tagServer(t, http.MethodPost, "/open/v1/tag",
		`{"name":"urgent","label":"URGENT"}`, tagDocCreate)
	out := tagRun(t, "tag", "create",
		"--name", "  Urgent ", "--label", " URGENT ", "--json")
	tagAssertJSON(t, out, tagDocCreate)
}

func TestTagCreateRejectsMismatchedLabel(t *testing.T) {
	// Validation happens before any HTTP request, so no server is needed.
	t.Setenv("TICKTICK_TOKEN", "test-token")
	var runErr error
	_ = captureOutput(t, func() {
		runErr = runCmd(t, "tag", "create", "--name", "urgent", "--label", "other")
	})
	if runErr == nil {
		t.Fatal("expected error for a label that does not match the name")
	}
	if !strings.Contains(runErr.Error(), "label") {
		t.Fatalf("error = %v, want it to mention the label", runErr)
	}
}
