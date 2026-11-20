package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenSources(t *testing.T) {
	for _, tc := range []struct {
		name, flag, env, mcp, dotenv, tokenFile, want string
		explicit                                      bool
	}{
		{name: "flag", flag: "flag-token", env: "env-token", mcp: "mcp-token", want: "flag-token"},
		{name: "environment", env: "env-token", mcp: "mcp-token", want: "env-token"},
		{name: "MCP environment", mcp: "mcp-token", dotenv: "TICKTICK_TOKEN=file-token", want: "mcp-token"},
		{name: "default dotenv", dotenv: "export TICKTICK_TOKEN='file-token' # comment", tokenFile: "raw-token", want: "file-token"},
		{name: "explicit dotenv", explicit: true, dotenv: `TICKTICK_MCP_TOKEN="mcp-token"`, want: "mcp-token"},
		{name: "legacy raw file", tokenFile: "raw-token=\n", want: "raw-token="},
		{name: "legacy assignment", tokenFile: "TICKTICK_TOKEN=file-token\n", want: "file-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+tc.want {
					t.Error("unexpected authorization header")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[]"))
			}))
			t.Cleanup(srv.Close)
			useTestAPIServer(t, srv)
			t.Setenv("HOME", t.TempDir())
			t.Setenv("TICKTICK_TOKEN", tc.env)
			t.Setenv("TICKTICK_MCP_TOKEN", tc.mcp)
			dir := configDirectory()
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			args := []string{"project", "list", "--json"}
			if tc.flag != "" {
				args = append(args, "--token", tc.flag)
			}
			if tc.dotenv != "" {
				path := filepath.Join(dir, ".env")
				if tc.explicit {
					path = filepath.Join(t.TempDir(), ".env")
					args = append(args, "--env-file", path)
				}
				if err := os.WriteFile(path, []byte(tc.dotenv), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.tokenFile != "" {
				if err := os.WriteFile(filepath.Join(dir, "token"), []byte(tc.tokenFile), 0600); err != nil {
					t.Fatal(err)
				}
			}
			captureOutput(t, func() {
				if err := runCmd(t, args...); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestUnauthorizedDoesNotEchoToken(t *testing.T) {
	const secret = "mock-echoed-credential"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, "Invalid access token: %s", r.Header.Get("Authorization"))
	}))
	t.Cleanup(srv.Close)
	useTestAPIServer(t, srv)
	t.Setenv("TICKTICK_TOKEN", secret)
	err := runCmd(t, "project", "list", "--json")
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), secret) {
		t.Fatalf("expected sanitized HTTP 401, got %v", err)
	}
}

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
