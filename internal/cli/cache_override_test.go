package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ticktick-cli/api"
)

func TestCacheOverridePreservesAuthAndWritesPrivateTaskSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(v2CookieKey, "")
	t.Setenv("TICKTICK_TOKEN", "")
	t.Setenv("TICKTICK_MCP_TOKEN", "")
	config := filepath.Join(home, ".config", "ticktick")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		filepath.Join(config, "cache"): "blocked directory",
		filepath.Join(config, "token"): "fixture-token",
		filepath.Join(home, ".env"):    v2CookieKey + "=fixture-cookie\n",
	} {
		if err := os.WriteFile(name, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := apiTransport
	t.Cleanup(func() { apiTransport = old })
	requests := 0
	const task = `{"id":"task","projectId":"project","status":0,"title":"Private fixture"}`
	const comment = `{"id":"comment","title":"Private comment"}`
	apiTransport = v2TestTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		var body string
		switch req.URL.Path {
		case "/api/v3/batch/check/0":
			if req.Header.Get("Cookie") != "t=fixture-cookie" {
				t.Error("cookie did not come from the original home")
			}
			body = `{"syncTaskBean":{"update":[` + task + `]}}`
		case "/open/v1/project/project/task/task/comments":
			if req.Header.Get("Authorization") != "Bearer fixture-token" {
				t.Error("token did not come from the original home")
			}
			body = `[` + comment + `]`
		default:
			return nil, fmt.Errorf("unexpected route %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	t.Setenv("TICKTICK_CACHE_DIR", "")
	if err := runCmd(t, "v3", "active", "--json"); err == nil || !strings.Contains(err.Error(), "cache_unavailable:") || requests != 0 {
		t.Fatalf("expected cache failure before any request, got err=%v and %d requests", err, requests)
	}
	if err := runCmd(t, "v2", "completed", "--json"); err == nil || !strings.Contains(err.Error(), "cache_unavailable:") || requests != 0 {
		t.Fatalf("expected cache failure before any request, got err=%v and %d requests", err, requests)
	}

	parent := t.TempDir()
	cacheDir := filepath.Join(parent, "cache")
	envDir := filepath.Join(parent, "env-cache")
	t.Setenv("TICKTICK_CACHE_DIR", envDir)
	path := filepath.Join(parent, "snapshot.json")
	active, _, _ := rootCmd.Find([]string{"v3", "active"})
	t.Cleanup(func() {
		_ = active.Flags().Set("cache-ids", "false")
		_ = active.Flags().Set("snapshot-file", "")
		active.Flags().Lookup("snapshot-file").Changed = false
		flagCacheDir = ""
	})
	args := []string{"v3", "active", "--cache-ids", "--snapshot-file", path, "--cache-dir", cacheDir, "--json"}
	output := captureOutput(t, func() {
		if err := runCmd(t, args...); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(output, "Private fixture") || !strings.Contains(output, `"id": "task"`) {
		t.Fatalf("unexpected public JSON: %s", output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, data, `[`+task+`]`)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot must be private: %v, %v", info, err)
	}
	if err := runCmd(t, args...); err == nil || !strings.Contains(err.Error(), "snapshot_write_failed:") {
		t.Fatalf("expected protection against overwriting snapshot input, got %v", err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(data, after) {
		t.Fatalf("snapshot changed despite exclusive creation: %v", err)
	}

	for _, name := range []string{"tasks", "use-cache"} {
		flag := taskCommentsBatchCmd.Flags().Lookup(name)
		value, changed := flag.Value.String(), flag.Changed
		t.Cleanup(func() {
			_ = taskCommentsBatchCmd.Flags().Set(name, value)
			flag.Changed = changed
		})
	}
	for range 2 {
		captureOutput(t, func() {
			if err := runCmd(t, "task", "comments-batch", "--tasks", "project:task", "--use-cache", "--cache-dir", cacheDir, "--json"); err != nil {
				t.Fatal(err)
			}
		})
	}
	if requests != 3 {
		t.Fatalf("two active reads and one comment request expected, got %d requests", requests)
	}
	cache, err := readTaskCache(filepath.Join(cacheDir, "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{"tasks": task, "comments": `[` + comment + `]`} {
		items, _, err := cacheEntries(cache, name)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONEqual(t, items["task"].Payload, expected)
	}
	if _, err := os.Stat(envDir); !os.IsNotExist(err) {
		t.Fatalf("flag should take precedence over environment: %v", err)
	}
	output = captureOutput(t, func() {
		if err := runCmd(t, "cache", "check", "--json"); err != nil {
			t.Fatal(err)
		}
	})
	var result struct{ Path string }
	if json.Unmarshal([]byte(output), &result) != nil || result.Path != filepath.Join(envDir, "cache.json") {
		t.Fatalf("cache check failed to resolve the environment override: %s", output)
	}
}

func TestCacheWritersInSeparateProcesses(t *testing.T) {
	if dir, id := os.Getenv("TICKTICK_TEST_CACHE"), os.Getenv("TICKTICK_TEST_TASK"); dir != "" && id != "" {
		var task api.V3BatchTask
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":%q}`, id)), &task); err != nil {
			t.Fatal(err)
		}
		if err := mergeCachedTasks(context.Background(), dir, []api.V2Task{task.V2Task}); err != nil {
			t.Fatal(err)
		}
		return
	}

	dir := filepath.Join(t.TempDir(), "cache")
	ids := []string{"one", "two", "three", "four"}
	processes := make([]*exec.Cmd, 0, len(ids))
	for _, id := range ids {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCacheWritersInSeparateProcesses$")
		cmd.Env = append(os.Environ(), "TICKTICK_TEST_CACHE="+dir, "TICKTICK_TEST_TASK="+id)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		processes = append(processes, cmd)
	}
	for _, process := range processes {
		if err := process.Wait(); err != nil {
			t.Error(err)
		}
	}
	cache, err := readTaskCache(filepath.Join(dir, "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err := cacheEntries(cache, "tasks")
	if err != nil || len(entries) != len(ids) {
		t.Fatalf("cache lost task writes: %v, %v", entries, err)
	}
}
