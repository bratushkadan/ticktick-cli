package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ticktick-cli/api"
)

// taskCmtTestServer is a fake TickTick API covering the three comment
// endpoints. Requests must carry the expected bearer token.
type taskCmtTestServer struct {
	srv *httptest.Server

	mu           sync.Mutex
	addBodies    []string
	deletedPaths []string
	gets         map[string]int
}

// taskCmtNewServer starts the fake API. Responses:
//   - p1/t1 → 2 comments, p2/t2 → 1 comment, p1/empty → []
//   - any task named "bad" → HTTP 404
//   - POST comment → {"id":"comment-9", ...} echoing the request title
//   - DELETE comment → 200, empty body
func taskCmtNewServer(t *testing.T) *taskCmtTestServer {
	t.Helper()

	s := &taskCmtTestServer{gets: make(map[string]int)}
	handler := func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// /open/v1/project/{p}/task/{t}/comments      (GET)
		// /open/v1/project/{p}/task/{t}/comment       (POST)
		// /open/v1/project/{p}/task/{t}/comment/{id}  (DELETE)
		if len(parts) < 7 || parts[0] != "open" || parts[1] != "v1" ||
			parts[2] != "project" || parts[4] != "task" || parts[3] == "" || parts[5] == "" {
			http.NotFound(w, r)
			return
		}
		projectID, taskID := parts[3], parts[5]

		if taskID == "bad" {
			http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
			return
		}

		switch {
		case r.Method == http.MethodGet && len(parts) == 7 && parts[6] == "comments":
			s.mu.Lock()
			s.gets[taskID]++
			s.mu.Unlock()
			var list []api.OpenComment
			switch {
			case projectID == "p1" && taskID == "t1":
				list = []api.OpenComment{
					{
						Id:          Ptr("comment-1"),
						Title:       Ptr("Comment text"),
						CreatedTime: Ptr(int64(1775552400000)),
						ModifiedTime: Ptr(
							int64(1775552400000)),
					},
					{
						Id:          Ptr("comment-2"),
						Title:       Ptr("Second comment"),
						CreatedTime: Ptr(int64(1775556000000)),
					},
				}
			case projectID == "p2" && taskID == "t2":
				list = []api.OpenComment{{
					Id:          Ptr("comment-3"),
					Title:       Ptr("Comment on p2/t2"),
					CreatedTime: Ptr(int64(1775559600000)),
				}}
			default:
				// Tasks with no comments (including "empty").
				list = []api.OpenComment{}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)

		case r.Method == http.MethodPost && len(parts) == 7 && parts[6] == "comment":
			body, _ := io.ReadAll(r.Body)
			var req api.CommentCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, `{"error":"bad request body"}`, http.StatusBadRequest)
				return
			}
			s.mu.Lock()
			s.addBodies = append(s.addBodies, req.Title)
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(api.OpenComment{
				Id:          Ptr("comment-9"),
				Title:       Ptr(req.Title),
				CreatedTime: Ptr(int64(1775563200000)),
				ModifiedTime: Ptr(
					int64(1775563200000)),
			})

		case r.Method == http.MethodDelete && len(parts) == 8 && parts[6] == "comment":
			s.mu.Lock()
			s.deletedPaths = append(s.deletedPaths, r.URL.Path)
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)

		default:
			http.NotFound(w, r)
		}
	}

	s.srv = httptest.NewTLSServer(http.HandlerFunc(handler))
	t.Cleanup(s.srv.Close)
	return s
}

func TestTaskCommentsCacheModesAndEviction(t *testing.T) {
	s := taskCmtSetup(t)
	t.Cleanup(func() { _ = taskCommentsBatchCmd.Flags().Set("use-cache", "false") })
	run := func(args ...string) string {
		t.Helper()
		return captureOutput(t, func() {
			if err := runCmd(t, append([]string{"task"}, args...)...); err != nil {
				t.Fatal(err)
			}
		})
	}
	checkGets := func(t1, empty int) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.gets["t1"] != t1 || s.gets["empty"] != empty {
			t.Fatalf("GET counts = %v, want t1=%d empty=%d", s.gets, t1, empty)
		}
	}
	saveTask := func(timestamp string) {
		t.Helper()
		var task api.V3BatchTask
		raw := []byte(`{"id":"t1","projectId":"p1","modifiedTime":"` + timestamp + `"}`)
		if err := json.Unmarshal(raw, &task); err != nil {
			t.Fatal(err)
		}
		if err := cacheWebTasks(context.Background(), []api.V2Task{task.V2Task}); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := taskCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	getEntries := func() map[string]cacheRecord {
		t.Helper()
		doc, err := readTaskCache(filepath.Join(dir, "cache.json"))
		if err != nil {
			t.Fatal(err)
		}
		var entries map[string]cacheRecord
		if err := json.Unmarshal(doc["comments"], &entries); err != nil {
			t.Fatal(err)
		}
		return entries
	}
	expireEmpty := func() {
		t.Helper()
		err := updateTaskCache(context.Background(), dir, func(_, entries map[string]cacheRecord) (bool, error) {
			entry := entries["empty"]
			stale := time.Now().Add(-25 * time.Hour)
			entry.Metadata.UpdatedAt = &stale
			entries["empty"] = entry
			return true, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	single := []string{"comments", "t1", "--project", "p1", "--json"}
	all := []string{"comments-batch", "--project", "p1", "--ids", "t1,empty", "--tasks", "", "--json"}
	onlyT1 := []string{"comments-batch", "--project", "p1", "--ids", "t1", "--tasks", "", "--json"}
	saveTask("2026-11-21T12:00:00.000+0000")
	run(single...)
	checkGets(1, 0)
	if len(getEntries()) != 0 {
		t.Fatal("single comments must not write to the cache")
	}
	run(all...)
	run(all...)
	run(append(all, "--use-cache")...)
	checkGets(3, 2)
	if entries := getEntries(); entries["t1"].Metadata.UpdatedAt == nil ||
		entries["empty"].Metadata.UpdatedAt == nil || string(entries["empty"].Payload) != "[]" {
		t.Fatal("expected fetched lists with timestamps, including the empty list")
	}

	expireEmpty()
	run(append(onlyT1, "--use-cache=false")...)
	checkGets(4, 2)
	if !commentCacheExpired(getEntries()["empty"], time.Now()) {
		t.Fatal("default batch must leave unrelated expired entries alone")
	}
	output := run(append(onlyT1, "--use-cache")...)
	checkGets(4, 3)
	var result map[string][]api.OpenComment
	if err := json.Unmarshal([]byte(output), &result); err != nil || len(result) != 1 || len(result["t1"]) != 2 {
		t.Fatal("output should contain only the requested task")
	}
	if commentCacheExpired(getEntries()["empty"], time.Now()) {
		t.Fatal("expired list must be refreshed by the opt-in batch")
	}

	saveTask("2026-11-21T12:00:00Z")
	run(append(onlyT1, "--use-cache")...)
	checkGets(4, 3)
	saveTask("2026-11-22T12:00:00Z")
	run(append(onlyT1, "--use-cache")...)
	checkGets(5, 3)
	before := getEntries()["t1"].Metadata.Generation
	run(single...)
	checkGets(6, 3)
	if getEntries()["t1"].Metadata.Generation != before {
		t.Fatal("single comments must not update an existing cache entry")
	}
	run("comment-add", "t1", "--project", "p1", "--text", "new comment")
	run(append(onlyT1, "--use-cache")...)
	checkGets(7, 3)
	run("comment-delete", "t1", "comment-1", "--project", "p1")
	run(append(onlyT1, "--use-cache")...)
	checkGets(8, 3)
}

// taskCmtSetup starts the fake API and points the CLI at it.
func taskCmtSetup(t *testing.T) *taskCmtTestServer {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s := taskCmtNewServer(t)
	useTestAPIServer(t, s.srv)
	return s
}

func (s *taskCmtTestServer) addedTitles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.addBodies...)
}

func (s *taskCmtTestServer) deletes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deletedPaths...)
}

func TestTaskCommentsJSON(t *testing.T) {
	taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comments", "t1", "--project", "p1", "--json")
	})
	if err != nil {
		t.Fatalf("task comments --json: %v", err)
	}

	var list []api.OpenComment
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
	}
	if len(list) != 2 {
		t.Fatalf("got %d comments, want 2\n%s", len(list), out)
	}
	if got := Str(list[0].Id); got != "comment-1" {
		t.Errorf("first comment id = %q, want comment-1", got)
	}
	if got := Str(list[0].Title); got != "Comment text" {
		t.Errorf("first comment title = %q, want %q", got, "Comment text")
	}
}

func TestTaskCommentsText(t *testing.T) {
	taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comments", "t1", "--project", "p1")
	})
	if err != nil {
		t.Fatalf("task comments: %v", err)
	}
	if !strings.Contains(out, "  [") {
		t.Errorf("output missing timestamp bracket, got:\n%s", out)
	}
	if !strings.Contains(out, "Comment text") {
		t.Errorf("output missing title, got:\n%s", out)
	}
	if !strings.Contains(out, "(id: comment-1)") {
		t.Errorf("output missing id, got:\n%s", out)
	}
	if strings.Count(strings.TrimSpace(out), "\n") != 1 {
		t.Errorf("want one line per comment (2 lines), got:\n%s", out)
	}
}

func TestTaskCommentsEmptyText(t *testing.T) {
	taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comments", "empty", "--project", "p1")
	})
	if err != nil {
		t.Fatalf("task comments (empty): %v", err)
	}
	if !strings.Contains(out, "(no comments)") {
		t.Errorf("output missing \"(no comments)\", got:\n%s", out)
	}
}

func TestTaskCommentAdd(t *testing.T) {
	s := taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comment-add", "t1", "--project", "p1", "--text", "hello world")
	})
	if err != nil {
		t.Fatalf("task comment-add: %v", err)
	}
	if !strings.Contains(out, "Comment added: comment-9") {
		t.Errorf("output = %q, want to contain %q", out, "Comment added: comment-9")
	}
	titles := s.addedTitles()
	if len(titles) != 1 {
		t.Fatalf("server received %d add requests, want 1", len(titles))
	}
	if titles[0] != "hello world" {
		t.Errorf("request body title = %q, want %q", titles[0], "hello world")
	}
}

func TestTaskCommentAddJSON(t *testing.T) {
	taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comment-add", "t1", "--project", "p1", "--text", "hi", "--json")
	})
	if err != nil {
		t.Fatalf("task comment-add --json: %v", err)
	}
	var oc api.OpenComment
	if err := json.Unmarshal([]byte(out), &oc); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
	}
	if got := Str(oc.Id); got != "comment-9" {
		t.Errorf("comment id = %q, want comment-9", got)
	}
}

func TestTaskCommentDelete(t *testing.T) {
	s := taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comment-delete", "t1", "comment-1", "--project", "p1")
	})
	if err != nil {
		t.Fatalf("task comment-delete: %v", err)
	}
	if strings.TrimSpace(out) != "Comment deleted." {
		t.Errorf("output = %q, want %q", out, "Comment deleted.")
	}
	paths := s.deletes()
	if len(paths) != 1 {
		t.Fatalf("server received %d delete requests, want 1", len(paths))
	}
	if want := "/open/v1/project/p1/task/t1/comment/comment-1"; paths[0] != want {
		t.Errorf("delete path = %q, want %q", paths[0], want)
	}
}

func TestTaskCommentDeleteJSON(t *testing.T) {
	taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comment-delete", "t1", "comment-1", "--project", "p1", "--json")
	})
	if err != nil {
		t.Fatalf("task comment-delete --json: %v", err)
	}
	if strings.TrimSpace(out) != `{"ok":true}` {
		t.Errorf("output = %q, want {\"ok\":true}", out)
	}
}

func TestTaskCommentsBatchProjectIDsJSON(t *testing.T) {
	taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comments-batch",
			"--project", "p1", "--ids", "t1,t2", "--tasks", "", "--workers", "2", "--json")
	})
	if err != nil {
		t.Fatalf("task comments-batch (project+ids): %v", err)
	}

	// stdout must be the pure taskID → []comments map, nothing else.
	var got map[string][]api.OpenComment
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not valid JSON map: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tasks in map, want 2: %v", len(got), got)
	}
	if n := len(got["t1"]); n != 2 {
		t.Errorf("t1: got %d comments, want 2", n)
	}
	// p1/t2 has no comments in the fake API: present as a non-nil empty slice.
	if n := len(got["t2"]); n != 0 {
		t.Errorf("t2 (p1): got %d comments, want 0", n)
	}
	if got["t2"] == nil {
		t.Error("t2: comments slice is nil, want non-nil (possibly empty) slice")
	}
}

func TestTaskCommentsBatchTasksForm(t *testing.T) {
	taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comments-batch",
			"--project", "", "--ids", "", "--tasks", "p1:t1,p2:t2", "--workers", "2")
	})
	if err != nil {
		t.Fatalf("task comments-batch (tasks): %v", err)
	}
	if !strings.Contains(out, "task t1 — 2 comment(s):") {
		t.Errorf("missing t1 header, got:\n%s", out)
	}
	if !strings.Contains(out, "task t2 — 1 comment(s):") {
		t.Errorf("missing t2 header, got:\n%s", out)
	}
	if !strings.Contains(out, "(id: comment-1)") {
		t.Errorf("missing t1 comment line, got:\n%s", out)
	}
	if !strings.Contains(out, "(id: comment-3)") {
		t.Errorf("missing t2 comment line, got:\n%s", out)
	}
}

func TestTaskCommentsBatchPartialFailure(t *testing.T) {
	taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comments-batch",
			"--project", "p1", "--ids", "t1,bad", "--tasks", "", "--workers", "2", "--json")
	})
	if err == nil {
		t.Fatal("want an error when one task fails, got nil")
	}
	if !strings.Contains(err.Error(), "1 of 2 tasks failed") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "1 of 2 tasks failed")
	}

	// Partial success still prints the map (t1 only).
	var got map[string][]api.OpenComment
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("stdout is not valid JSON map: %v\n%s", uerr, out)
	}
	if len(got) != 1 {
		t.Fatalf("got %d tasks in map, want 1: %v", len(got), got)
	}
	if n := len(got["t1"]); n != 2 {
		t.Errorf("t1: got %d comments, want 2", n)
	}
	if _, ok := got["bad"]; ok {
		t.Error("failed task should not appear in the comments map")
	}
}

func TestTaskCommentsBatchValidation(t *testing.T) {
	taskCmtSetup(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no input form",
			args: []string{"task", "comments-batch", "--project", "", "--ids", "", "--tasks", "", "--workers", "4"},
			want: "specify either",
		},
		{
			name: "project without ids",
			args: []string{"task", "comments-batch", "--project", "p1", "--ids", "", "--tasks", "", "--workers", "4"},
			want: "specify either",
		},
		{
			name: "ids without project",
			args: []string{"task", "comments-batch", "--project", "", "--ids", "t1", "--tasks", "", "--workers", "4"},
			want: "specify either",
		},
		{
			name: "both forms",
			args: []string{"task", "comments-batch", "--project", "p1", "--ids", "t1", "--tasks", "p1:t1", "--workers", "4"},
			want: "not both",
		},
		{
			name: "malformed tasks entry",
			args: []string{"task", "comments-batch", "--project", "", "--ids", "", "--tasks", "p1-t1", "--workers", "4"},
			want: "PROJECT_ID:TASK_ID",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			out := captureOutput(t, func() { err = runCmd(t, tc.args...) })
			if err == nil {
				t.Fatalf("want validation error, got nil (stdout %q)", out)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestTaskCommentsBatchJSONIsPureMap guards the output contract: with
// --json, no human-readable text may leak onto stdout.
func TestTaskCommentsBatchJSONIsPureMap(t *testing.T) {
	taskCmtSetup(t)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "comments-batch",
			"--project", "p1", "--ids", "t1", "--tasks", "", "--workers", "1", "--json")
	})
	if err != nil {
		t.Fatalf("task comments-batch --json: %v", err)
	}
	if strings.Contains(out, "comment(s):") || strings.Contains(out, "task t1") {
		t.Fatalf("text output leaked into --json stdout:\n%s", out)
	}
	var got map[string][]api.OpenComment
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", uerr, out)
	}
	if n := len(got["t1"]); n != 2 {
		t.Fatalf("t1: got %d comments, want 2", n)
	}
}
