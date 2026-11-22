package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type v2TestTransport func(*http.Request) (*http.Response, error)

func (f v2TestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestV2Commands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(v2CookieKey, "")
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte(v2CookieKey+"='t=private-cookie'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldTransport, oldJSON, oldEnvFile := apiTransport, flagJSON, flagEnvFile
	t.Cleanup(func() { apiTransport, flagJSON, flagEnvFile = oldTransport, oldJSON, oldEnvFile })
	flagEnvFile = ""
	task := `{"id":"private-task","projectId":"private-project","title":"private-title","content":"private-body","kind":"TEXT","status":2,"tags":["private-tag"],"completedTime":"2026-11-20T19:00:00.123+0000","dueDate":"private-bad-date"}`
	focusArgs := []string{"--from", "2026-11-20", "--to", "20261121"}
	const focusPath = "/api/v2/pomodoros/statistics/"
	tests := []struct {
		name string
		args []string
		path string
		q    url.Values
		body string
		res  string
		want string
	}{
		{
			name: "search repeated filters",
			args: []string{"search", "a + б", "--status", "completed", "--projects", "p1,p2", "--tags", "t1", "--tags", "t2", "--due-from", "0", "--include-closed"},
			path: "/api/v2/search/all",
			q:    url.Values{"keywords": {"a + б"}, "status": {"1", "2"}, "projectId": {"p1", "p2"}, "tags": {"t1", "t2"}, "dueFrom": {"0"}, "includeClosed": {"true"}},
			res:  `{"tasks":[` + task + `],"comments":[{"id":"private-comment","taskId":"private-task","content":"private-text"}],"hits":[{"id":"private-task","highlight":"private-highlight"}]}`,
			want: "anon-",
		},
		{
			name: "search due to only",
			args: []string{"search", "test", "--status", "open", "--due-to", "123"},
			path: "/api/v2/search/all",
			q:    url.Values{"keywords": {"test"}, "status": {"0"}, "dueTo": {"123"}},
			res:  `{"tasks":[],"comments":[],"hits":[]}`,
			want: "tasks",
		},
		{name: "empty search", args: []string{"search", "  "}, want: `"tasks": []`},
		{
			name: "completed in all",
			args: []string{"completed", "--to", "2026-11-21T03:00:00+03:00"},
			path: "/api/v2/project/all/completedInAll/",
			q:    url.Values{"from": {""}, "to": {"2026-11-21 00:00:00"}, "limit": {"100"}},
			res:  "[" + task + "]", want: `"nextTo": "2026-11-20 19:00:00"`,
		},
		{
			name: "completed all",
			args: []string{"completed", "--all", "--from", "2026-11-19", "--to", "2026-11-21", "--limit", "1200"},
			path: "/api/v2/project/all/completed/",
			q:    url.Values{"from": {"2026-11-19 00:00:00"}, "to": {"2026-11-21 00:00:00"}, "limit": {"1200"}},
			res:  "[]", want: "tasks",
		},
		{
			name: "completed project",
			args: []string{"completed", "--project", "p1", "--to", "2026-11-21"},
			path: "/api/v2/project/p1/completed/",
			q:    url.Values{"from": {""}, "to": {"2026-11-21 00:00:00"}, "limit": {"100"}},
			res:  "[]", want: "tasks",
		},
		{
			name: "focus heatmap", args: append([]string{"focus", "heatmap"}, focusArgs...),
			path: focusPath + "heatmap/20261120/20261121",
			res:  `[{"day":"20261120","duration":42,"timezone":"private-timezone"}]`, want: "2026-11-20",
		},
		{
			name: "focus distribution", args: append([]string{"focus", "dist"}, focusArgs...),
			path: focusPath + "dist/20261120/20261121",
			res:  `{"taskDurations":{"private-task":42},"projectDurations":{"private-project":42},"tagDurations":{"private-tag":42}}`, want: "anon-",
		},
		{
			name: "focus clock", args: append([]string{"focus", "clock"}, focusArgs...),
			path: focusPath + "dist/clock/20261120/20261121",
			res:  `{"timeDurations":{"3":42}}`, want: `"3": 42`,
		},
		{
			name: "focus clock by day", args: append([]string{"focus", "clock-by-day"}, focusArgs...),
			path: focusPath + "dist/clockByDay/20261120/20261121",
			res:  `[{"day":"2026-11-20","timeDurations":{"3":42},"timezone":"private-timezone"}]`, want: `"3": 42`,
		},
		{
			name: "focus general", args: []string{"focus", "general"},
			path: focusPath + "generalForDesktop", res: `{"todayPomoCount":42}`, want: `"todayPomoCount": 42`,
		},
		{
			name: "habit checkins without bound",
			args: []string{"habit-checkins", "--ids", "h1,h2"}, path: "/api/v2/habitCheckins/query",
			body: `{"habitIds":["h1","h2"]}`, res: `{"checkins":{"private-habit":[]}}`, want: "anon-",
		},
		{
			name: "habit checkins with bound",
			args: []string{"habit-checkins", "--ids", "h1", "--after", "2026-11-20"}, path: "/api/v2/habitCheckins/query",
			body: `{"habitIds":["h1"],"afterStamp":20261120}`,
			res:  `{"checkins":{"private-habit":[{"id":"private-checkin","checkinStamp":20261121,"value":1.5,"goal":2,"note":"private-note"}]}}`, want: `"value": 1.5`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			apiTransport = v2TestTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Scheme != "https" || req.URL.Host != "ticktick.com" || req.URL.EscapedPath() != tt.path {
					t.Errorf("unexpected URL %s", req.URL)
				}
				if q := req.URL.Query(); !reflect.DeepEqual(q, tt.q) && (len(q) != 0 || len(tt.q) != 0) {
					t.Errorf("query = %v, want %v", q, tt.q)
				}
				if req.Header.Get("Cookie") != "t=private-cookie" || req.Header.Get("Authorization") != "" {
					t.Error("expected only session-cookie auth")
				}
				if tt.body == "" {
					if req.Method != http.MethodGet || req.Body != nil {
						t.Error("expected GET without a body")
					}
				} else {
					if req.Method != http.MethodPost || req.Body == nil || req.Header.Get("Content-Type") != "application/json" {
						t.Fatal("expected JSON POST")
					}
					body, err := io.ReadAll(req.Body)
					if err != nil || string(body) != tt.body {
						t.Errorf("body = %s, error = %v, want %s", body, err, tt.body)
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tt.res))}, nil
			})
			for _, asJSON := range []bool{false, true} {
				flagJSON = asJSON
				cmd := newV2Cmd()
				cmd.SetArgs(tt.args)
				output := captureOutput(t, func() {
					if err := cmd.Execute(); err != nil {
						t.Error(err)
					}
				})
				if strings.Contains(output, "private-") {
					t.Error("output exposed a private value")
				}
				if asJSON && (!json.Valid([]byte(output)) || !strings.Contains(output, tt.want)) {
					t.Errorf("JSON %q did not contain %q", output, tt.want)
				}
			}
			wantRequests := 0
			if tt.path != "" {
				wantRequests = 2
			}
			if requests != wantRequests {
				t.Errorf("request count = %d, want %d", requests, wantRequests)
			}
		})
	}
}

func TestV2ErrorsSuppressResponseBodyAndRedirect(t *testing.T) {
	t.Setenv(v2CookieKey, "t=private-cookie")
	oldTransport := apiTransport
	t.Cleanup(func() { apiTransport = oldTransport })

	for _, code := range []int{http.StatusUnauthorized, http.StatusBadGateway, http.StatusTemporaryRedirect} {
		requests := 0
		apiTransport = v2TestTransport(func(req *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: code,
				Header:     http.Header{"Location": {"https://example.invalid"}},
				Body:       io.NopCloser(strings.NewReader("private-cookie private-title")),
			}, nil
		})
		cmd := newV2Cmd()
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		cmd.SetArgs([]string{"focus", "general"})
		err := cmd.Execute()
		if err == nil || strings.Contains(err.Error(), "private-") || requests != 1 {
			t.Errorf("HTTP %d: unexpected error or redirect, requests = %d", code, requests)
		}
	}
}
