package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRejectHTTPBaseURL(t *testing.T) {
	t.Setenv("TICKTICK_TOKEN", "test-token")
	for _, tc := range []struct {
		name string
		env  string
		args []string
	}{
		{name: "environment", env: "http://127.0.0.1:1"},
		{name: "flag", env: defaultBaseURL, args: []string{"--base-url", "http://127.0.0.1:1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TICKTICK_BASE_URL", tc.env)
			err := runCmd(t, append([]string{"project", "list"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), "API base URL must be an absolute HTTPS URL") {
				t.Fatalf("expected HTTPS base URL error, got %v", err)
			}
		})
	}
}

func TestAPIRedirectRequiresHTTPS(t *testing.T) {
	for _, name := range []string{"https", "http"} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/projects" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("[]"))
					return
				}
				http.Redirect(w, r, name+"://"+r.Host+"/projects", http.StatusFound)
			}))
			t.Cleanup(srv.Close)
			useTestAPIServer(t, srv)

			var err error
			captureOutput(t, func() { err = runCmd(t, "project", "list", "--json") })
			if name == "https" {
				if err != nil {
					t.Fatalf("expected HTTPS redirect to succeed, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "API redirect must use HTTPS") {
				t.Fatalf("expected HTTPS redirect error, got %v", err)
			}
		})
	}
}
