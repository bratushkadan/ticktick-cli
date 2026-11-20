package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// countdownTestServer starts an httptest server wired into the CLI env.
func countdownTestServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("TICKTICK_BASE_URL", srv.URL)
	t.Setenv("TICKTICK_TOKEN", "test-token")
	return srv
}

// countdownWriteJSON writes a JSON response body.
func countdownWriteJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// countdownRunOK runs the CLI and fails the test on error, returning stdout.
func countdownRunOK(t *testing.T, args ...string) string {
	t.Helper()
	var out string
	var runErr error
	out = captureOutput(t, func() { runErr = runCmd(t, args...) })
	if runErr != nil {
		t.Fatalf("runCmd(%v) returned error: %v", args, runErr)
	}
	return out
}

const countdownBody = `[
  {
    "id": "countdown-1",
    "type": 0,
    "name": "Birthday",
    "date": 20260407,
    "color": "#4D8CF5",
    "reminders": [],
    "sortOrder": 0
  },
  {
    "id": "countdown-2",
    "type": 0,
    "name": "Launch",
    "date": 20260410,
    "color": "#FF0000",
    "repeatFlag": "RRULE:FREQ=YEARLY"
  }
]`

func TestCountdownListJSON(t *testing.T) {
	countdownTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/open/v1/countdown" {
			t.Errorf("path = %q, want /open/v1/countdown", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", r.URL.RawQuery)
		}
		countdownWriteJSON(w, countdownBody)
	})

	out := countdownRunOK(t, "countdown", "list", "--json")
	for _, want := range []string{`"name": "Birthday"`, `"date": 20260407`, `"color": "#4D8CF5"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %q:\n%s", want, out)
		}
	}
}

func TestCountdownListText(t *testing.T) {
	countdownTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open/v1/countdown" {
			t.Errorf("path = %q, want /open/v1/countdown", r.URL.Path)
		}
		countdownWriteJSON(w, countdownBody)
	})

	out := countdownRunOK(t, "countdown", "list")
	for _, want := range []string{
		"ID", "NAME", "DATE", "COLOR", "REPEAT",
		"countdown-1", "Birthday", "2026-04-07", "#4D8CF5",
		"countdown-2", "Launch", "2026-04-10", "RRULE:FREQ=YEARLY",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}
