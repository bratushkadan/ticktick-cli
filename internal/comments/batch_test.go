package comments

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ticktick-cli/api"
)

// taskCmtPtr returns a pointer to v (helpers in package cli are not
// importable here).
func taskCmtPtr[T any](v T) *T { return &v }

// taskCmtServer is a fake TickTick comments endpoint that tracks
// concurrency (max in-flight requests) and can fail selected task IDs.
type taskCmtServer struct {
	srv         *httptest.Server
	inFlight    int32
	maxInFlight int32
}

// newTaskCmtServer starts a test server. Each comments request sleeps
// `delay`, so tests can prove concurrency from wall-clock time and from
// the observed maximum number of simultaneous requests.
func newTaskCmtServer(t *testing.T, delay time.Duration, notFound ...string) *taskCmtServer {
	t.Helper()

	missing := make(map[string]bool, len(notFound))
	for _, id := range notFound {
		missing[id] = true
	}

	s := &taskCmtServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Track concurrent in-flight requests and the observed maximum.
		cur := atomic.AddInt32(&s.inFlight, 1)
		defer atomic.AddInt32(&s.inFlight, -1)
		for {
			old := atomic.LoadInt32(&s.maxInFlight)
			if cur <= old || atomic.CompareAndSwapInt32(&s.maxInFlight, old, cur) {
				break
			}
		}

		// Path shape: /open/v1/project/{projectId}/task/{taskId}/comments
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 7 || parts[0] != "open" || parts[1] != "v1" || parts[2] != "project" || parts[4] != "task" || parts[6] != "comments" {
			http.NotFound(w, r)
			return
		}
		projectID, taskID := parts[3], parts[5]
		if projectID == "" || taskID == "" {
			http.NotFound(w, r)
			return
		}

		time.Sleep(delay)

		if missing[taskID] {
			http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
			return
		}

		list := []api.OpenComment{
			{
				Id:          taskCmtPtr("comment-" + taskID + "-1"),
				Title:       taskCmtPtr("Comment on " + taskID),
				CreatedTime: taskCmtPtr(int64(1775552400000)),
			},
			{
				Id:          taskCmtPtr("comment-" + taskID + "-2"),
				Title:       taskCmtPtr("Second comment on " + taskID),
				CreatedTime: taskCmtPtr(int64(1775557800000)),
			},
		}
		if taskID == "t3" {
			// One task intentionally has no comments.
			list = []api.OpenComment{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *taskCmtServer) max() int32 {
	return atomic.LoadInt32(&s.maxInFlight)
}

// refsFor builds n refs t1..tn in the same project.
func refsFor(n int) []TaskRef {
	out := make([]TaskRef, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, TaskRef{ProjectID: "p1", TaskID: fmt.Sprintf("t%d", i)})
	}
	return out
}

func TestFetchAllIsConcurrent(t *testing.T) {
	srv := newTaskCmtServer(t, 60*time.Millisecond)
	client, err := api.NewClientWithResponses(srv.srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}

	refs := refsFor(8)
	start := time.Now()
	res := FetchAll(context.Background(), client, refs, 4)
	elapsed := time.Since(start)

	for _, ref := range refs {
		if res.Errors[ref.TaskID] != nil {
			t.Fatalf("task %s: unexpected error: %v", ref.TaskID, res.Errors[ref.TaskID])
		}
		if _, ok := res.Comments[ref.TaskID]; !ok {
			t.Fatalf("task %s: missing from Comments map", ref.TaskID)
		}
	}
	// t3 has no comments: present, non-nil, empty.
	if got, ok := res.Comments["t3"]; !ok {
		t.Fatal("t3: missing from Comments map")
	} else if got == nil {
		t.Fatal("t3: comments slice is nil, want non-nil empty slice")
	} else if len(got) != 0 {
		t.Fatalf("t3: got %d comments, want 0", len(got))
	}
	// All other tasks have exactly two comments.
	for _, id := range []string{"t1", "t2", "t4", "t5", "t6", "t7", "t8"} {
		if got := len(res.Comments[id]); got != 2 {
			t.Errorf("task %s: got %d comments, want 2", id, got)
		}
	}

	// 8 refs × 60ms would be ~480ms serial; with 4 workers it is ~120ms.
	// The strict proof of concurrency is maxInFlight > 1, the time bound
	// guards against a fully serial implementation.
	if elapsed >= 500*time.Millisecond {
		t.Errorf("elapsed %v, want < 500ms (serial would be ~480ms)", elapsed)
	}
	if srv.max() <= 1 {
		t.Errorf("max in-flight requests = %d, want > 1 (requests ran serially)", srv.max())
	}
	if srv.max() > 4 {
		t.Errorf("max in-flight requests = %d, want <= 4", srv.max())
	}
}

func TestFetchAllClampsMaxWorkers(t *testing.T) {
	srv := newTaskCmtServer(t, 60*time.Millisecond)
	client, err := api.NewClientWithResponses(srv.srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}

	if got := ClampWorkers(100); got != MaxWorkers {
		t.Fatalf("ClampWorkers(100) = %d, want %d", got, MaxWorkers)
	}
	res := FetchAll(context.Background(), client, refsFor(12), 100)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Comments) != 12 {
		t.Fatalf("got %d comment entries, want 12", len(res.Comments))
	}
	if srv.max() > 8 {
		t.Errorf("max in-flight requests = %d, want <= 8 (clamped)", srv.max())
	}
}

func TestFetchAllDefaultsWorkersWhenZero(t *testing.T) {
	srv := newTaskCmtServer(t, 60*time.Millisecond)
	client, err := api.NewClientWithResponses(srv.srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}

	if got := ClampWorkers(0); got != DefaultWorkers {
		t.Fatalf("ClampWorkers(0) = %d, want %d", got, DefaultWorkers)
	}
	if got := ClampWorkers(-3); got != DefaultWorkers {
		t.Fatalf("ClampWorkers(-3) = %d, want %d", got, DefaultWorkers)
	}
	if got := ClampWorkers(2); got != 2 {
		t.Fatalf("ClampWorkers(2) = %d, want 2", got)
	}

	res := FetchAll(context.Background(), client, refsFor(8), 0)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Comments) != 8 {
		t.Fatalf("got %d comment entries, want 8", len(res.Comments))
	}
	if srv.max() > 4 {
		t.Errorf("max in-flight requests = %d, want <= 4 (default workers)", srv.max())
	}
	if srv.max() <= 1 {
		t.Errorf("max in-flight requests = %d, want > 1 (default workers should be used)", srv.max())
	}
}

func TestFetchAllRecordsPerTaskErrors(t *testing.T) {
	srv := newTaskCmtServer(t, 0, "bad")
	client, err := api.NewClientWithResponses(srv.srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}

	refs := []TaskRef{
		{ProjectID: "p1", TaskID: "t1"},
		{ProjectID: "p1", TaskID: "bad"},
		{ProjectID: "p1", TaskID: "t2"},
	}
	res := FetchAll(context.Background(), client, refs, 2)

	if res.Errors["bad"] == nil {
		t.Fatal("bad: want an error, got nil")
	}
	if !strings.Contains(res.Errors["bad"].Error(), "404") {
		t.Errorf("bad: error %q should mention HTTP 404", res.Errors["bad"].Error())
	}
	if _, ok := res.Comments["bad"]; ok {
		t.Error("bad: should not appear in Comments map")
	}
	for _, id := range []string{"t1", "t2"} {
		if res.Errors[id] != nil {
			t.Errorf("%s: unexpected error: %v", id, res.Errors[id])
		}
		if len(res.Comments[id]) != 2 {
			t.Errorf("%s: got %d comments, want 2", id, len(res.Comments[id]))
		}
	}
}

func TestFetchAllEmptyRefs(t *testing.T) {
	client, err := api.NewClientWithResponses("http://127.0.0.1:1") // never called
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}
	res := FetchAll(context.Background(), client, nil, 4)
	if res.Comments == nil {
		t.Error("Comments map is nil, want non-nil empty map")
	}
	if res.Errors == nil {
		t.Error("Errors map is nil, want non-nil empty map")
	}
	if len(res.Comments) != 0 || len(res.Errors) != 0 {
		t.Errorf("got %d comments / %d errors, want 0/0", len(res.Comments), len(res.Errors))
	}
}

func TestFetchAllRespectsContextCancellation(t *testing.T) {
	srv := newTaskCmtServer(t, 0)
	client, err := api.NewClientWithResponses(srv.srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the batch starts

	refs := refsFor(3)
	res := FetchAll(ctx, client, refs, 2)
	if len(res.Errors) != len(refs) {
		t.Fatalf("got %d errors, want %d (all tasks cancelled)", len(res.Errors), len(refs))
	}
	for _, ref := range refs {
		if res.Errors[ref.TaskID] == nil {
			t.Errorf("task %s: want cancellation error, got nil", ref.TaskID)
		}
	}
	if len(res.Comments) != 0 {
		t.Errorf("got %d comments, want 0", len(res.Comments))
	}
}
