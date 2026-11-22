package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthStatusSeparateFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TICKTICK_TOKEN", "")
	t.Setenv("TICKTICK_MCP_TOKEN", "")
	t.Setenv("TICKTICK_V2_API_COOKIE", "")
	t.Setenv("TICKTICK_BASE_URL", "invalid-url")
	openFile := filepath.Join(home, "open.env")
	if err := os.WriteFile(openFile, []byte("TICKTICK_TOKEN=fake-token"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("TICKTICK_V2_API_COOKIE=t=fake-cookie"), 0600); err != nil {
		t.Fatal(err)
	}
	out := captureOutput(t, func() {
		if err := runCmd(t, "auth-status", "--env-file", openFile, "--web-env-file=", "--json"); err != nil {
			t.Fatal(err)
		}
	})
	var status map[string]bool
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	if len(status) != 2 || !status["token_configured"] || !status["cookie_configured"] ||
		strings.Contains(out, "fake-") {
		t.Fatal("expected only local credential availability")
	}
}
