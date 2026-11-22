package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ticktick-cli/api"
	"ticktick-cli/internal/comments"
)

func TestWebCommandsPopulateTaskCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(v2CookieKey, "t=fake-cookie")
	dir := filepath.Join(home, ".config", "ticktick", "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cache.json")
	initial := `{"tasks":{"old":{"id":"old"}},"comments":{"old":[{"id":"comment"}]},"other":{"keep":true}}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	old := apiTransport
	t.Cleanup(func() { apiTransport = old })

	const active = `{"id":"task-active","projectId":"private-project","status":0,"deleted":0,"title":"private-active","custom":{"x":1}}`
	const completed = `{"id":"task-done","projectId":"private-project","status":2,"title":"private-done","completedTime":"2026-11-21T14:00:00.000+0000","checklist":[{"title":"private-item"}]}`
	apiTransport = v2TestTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/api/v3/batch/check/0":
			body = `{"checkPoint":123,"syncTaskBean":{"add":[{"id":"task-active","title":"stale-active"},{"id":"task-tombstone","status":0},{"id":"task-closed","status":2}],"update":[` +
				active + `,{"id":"task-tombstone","status":0,"deleted":1}]}}`
		case "/api/v2/project/all/completedInAll/":
			if req.URL.Query().Get("limit") != "2000" {
				t.Error("expected limit=2000")
			}
			body = "[" + completed + "]"
		default:
			return nil, fmt.Errorf("unexpected route %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})

	for _, args := range [][]string{
		{"v3", "active", "--json"},
		{"v2", "completed", "--limit", "2000", "--json"},
		{"v3", "active", "--json"},
	} {
		output := captureOutput(t, func() {
			if err := runCmd(t, args...); err != nil {
				t.Fatal(err)
			}
		})
		if strings.Contains(output, "private-") || strings.Contains(output, "task-active") || !strings.Contains(output, "anon-") {
			t.Error("output should use pseudonyms and omit private fields")
		}
		if args[0] == "v3" {
			var tasks []api.V2Task
			if err := json.Unmarshal([]byte(output), &tasks); err != nil || len(tasks) != 1 {
				t.Fatal("expected one active task in the output")
			}
		}
	}

	for _, test := range []struct {
		args []string
		id   string
	}{
		{[]string{"v3", "active"}, "task-active"},
		{[]string{"v2", "completed", "--limit", "2000"}, "task-done"},
	} {
		if err := runCmd(t, append(test.args, "--cache-ids")...); err == nil || !strings.Contains(err.Error(), "requires --json") {
			t.Fatal("expected --cache-ids without --json to fail")
		}
		output := captureOutput(t, func() {
			if err := runCmd(t, append(test.args, "--cache-ids", "--json")...); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(output, `"id": "`+test.id+`"`) || strings.Contains(output, "private-") {
			t.Fatal("expected the original task ID without other private fields")
		}
		command, _, err := rootCmd.Find(test.args)
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Flags().Set("cache-ids", "false"); err != nil {
			t.Fatal(err)
		}
	}

	cache, err := readTaskCache(path)
	if err != nil {
		t.Fatal(err)
	}
	var tasks map[string]cacheRecord
	if err := json.Unmarshal(cache["tasks"], &tasks); err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, tasks["task-active"].Payload, active)
	assertJSONEqual(t, tasks["task-done"].Payload, completed)
	assertJSONEqual(t, tasks["old"].Payload, `{"id":"old"}`)
	assertJSONEqual(t, cache["comments"], `{"old":{"payload":[{"id":"comment"}],"metadata":{"updatedAt":null}}}`)
	if tasks["task-active"].Metadata.UpdatedAt == nil || tasks["task-done"].Metadata.UpdatedAt == nil || tasks["old"].Metadata.UpdatedAt != nil {
		t.Fatal("expected timestamps for fetched tasks and an unknown timestamp for the legacy task")
	}
	assertJSONEqual(t, cache["other"], `{"keep":true}`)
	if len(tasks) != 3 {
		t.Errorf("expected only active and completed records plus the previous cache, got %d tasks", len(tasks))
	}
}

func TestConcurrentCacheWritesMerge(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	errs := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		var task api.V3BatchTask
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":%q}`, id)), &task); err != nil {
			t.Fatal(err)
		}
		go func(task api.V2Task) {
			errs <- mergeCachedTasks(context.Background(), dir, []api.V2Task{task})
		}(task.V2Task)
	}
	for i := 0; i < cap(errs); i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	cache, err := readTaskCache(filepath.Join(dir, "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tasks map[string]cacheRecord
	if err := json.Unmarshal(cache["tasks"], &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected two tasks, got %d", len(tasks))
	}
	assertJSONEqual(t, tasks["one"].Payload, `{"id":"one"}`)
	assertJSONEqual(t, tasks["two"].Payload, `{"id":"two"}`)
}

func TestInFlightCommentsCannotRestoreInvalidatedEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	const id = "task"
	refs := []comments.TaskRef{{TaskID: id, ProjectID: "project"}}
	scope := [32]byte{1}
	cache, _, missing, err := prepareCommentCache(ctx, scope, refs, true)
	if err != nil || len(missing) != 1 {
		t.Fatalf("expected a cache miss, got %d misses and %v", len(missing), err)
	}
	if err := invalidateTaskComments(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := cache.store(ctx,
		map[string]json.RawMessage{id: json.RawMessage(`[{"id":"stale"}]`)},
		map[string]string{id: refs[0].ProjectID},
	); err != nil {
		t.Fatal(err)
	}
	_, _, missing, err = prepareCommentCache(ctx, scope, refs, true)
	if err != nil || len(missing) != 1 {
		t.Fatalf("stale fetch restored a cache hit, got %d misses and %v", len(missing), err)
	}
}

func TestExpiredCommentPayloadEvictedBeforeFetch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	dir, err := taskCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	scope := [32]byte{}
	stale := time.Now().UTC().Add(-25 * time.Hour)
	err = updateTaskCache(ctx, dir, func(_, entries map[string]cacheRecord) (bool, error) {
		entries["expired"] = cacheRecord{
			Payload: json.RawMessage(`[{"id":"stale"}]`),
			Metadata: cacheMetadata{
				UpdatedAt: &stale, ProjectID: "project", Scope: strings.Repeat("0", 64),
			},
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := comments.TaskRef{TaskID: "expired", ProjectID: "project"}
	for i := 0; i < 2; i++ {
		_, _, fetch, err := prepareCommentCache(ctx, scope, nil, true)
		if err != nil || len(fetch) != 1 || fetch[0] != want {
			t.Fatalf("expected expired task to be queued for refresh, got %v, %v", fetch, err)
		}
	}
	doc, err := readTaskCache(filepath.Join(dir, "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err := cacheEntries(doc, "comments")
	if err != nil {
		t.Fatal(err)
	}
	entry := entries["expired"]
	if string(entry.Payload) != "null" || entry.Metadata.Generation != 1 || entry.Metadata.UpdatedAt == nil ||
		!stale.Equal(*entry.Metadata.UpdatedAt) {
		t.Fatal("expected the expired payload to be evicted once and to retain its previous timestamp for retries")
	}
}

func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(a)
	expected, _ := json.Marshal(b)
	if string(actual) != string(expected) {
		t.Errorf("JSON = %s, want %s", actual, expected)
	}
}
