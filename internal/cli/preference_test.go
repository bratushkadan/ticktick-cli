package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// prefTestServer starts an httptest server wired into the CLI env.
func prefTestServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("TICKTICK_BASE_URL", srv.URL)
	t.Setenv("TICKTICK_TOKEN", "test-token")
	return srv
}

// prefWriteJSON writes a JSON response body.
func prefWriteJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// prefRunOK runs the CLI and fails the test on error, returning stdout.
func prefRunOK(t *testing.T, args ...string) string {
	t.Helper()
	var out string
	var runErr error
	out = captureOutput(t, func() { runErr = runCmd(t, args...) })
	if runErr != nil {
		t.Fatalf("runCmd(%v) returned error: %v", args, runErr)
	}
	return out
}

const prefBody = `{"timeZone":"America/Los_Angeles"}`

func TestPreferenceJSON(t *testing.T) {
	prefTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/open/v1/preference" {
			t.Errorf("path = %q, want /open/v1/preference", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		prefWriteJSON(w, prefBody)
	})

	out := prefRunOK(t, "preference", "--json")
	if !strings.Contains(out, `"timeZone": "America/Los_Angeles"`) {
		t.Errorf("json output missing timeZone:\n%s", out)
	}
}

func TestPreferenceText(t *testing.T) {
	prefTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/open/v1/preference" {
			t.Errorf("path = %q, want /open/v1/preference", r.URL.Path)
		}
		prefWriteJSON(w, prefBody)
	})

	out := prefRunOK(t, "preference")
	if want := "Time zone: America/Los_Angeles\n"; out != want {
		t.Errorf("text output = %q, want %q", out, want)
	}
}

func TestPreferenceRejectsArgs(t *testing.T) {
	if err := runCmd(t, "preference", "extra"); err == nil {
		t.Fatal("expected an error for unexpected positional argument")
	}
}
