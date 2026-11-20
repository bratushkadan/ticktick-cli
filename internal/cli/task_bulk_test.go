package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ticktick-cli/api"
)

// Reset every local flag variable of the bulk task commands so tests do not
// inherit values from earlier runCmd calls (cobra only writes flags that are
// explicitly present on the command line).
func taskBulkResetFlags() {
	taskBulkCompleteManyIDs = ""
	taskBulkCompleteManyProject = ""
	taskBulkMoveFrom = ""
	taskBulkMoveTo = ""
	taskBulkMoveIDs = ""
	taskBulkMoveData = ""
	taskBulkBatchData = ""
	taskBulkBatchFile = ""
	taskBulkAssignProject = ""
	taskBulkAssignUsername = ""
	taskBulkUnassignProject = ""
}

// taskBulkSetup starts an httptest server that asserts the POST path, bearer
// token and body of the inbound request, then replies with respBody.
func taskBulkSetup(t *testing.T, wantPath, respBody string, check func(t *testing.T, body []byte)) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		if check != nil {
			check(t, body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	useTestAPIServer(t, srv)
}

// taskBulkDocTask is the trimmed Task JSON from the official docs (GET task).
const taskBulkDocTask = `{
  "id": "63b7bebb91c0a5474805fcd4",
  "isAllDay": true,
  "projectId": "6226ff9877acee87727f6bca",
  "title": "Task Title",
  "content": "Task Content",
  "desc": "Task Description",
  "timeZone": "America/Los_Angeles",
  "repeatFlag": "RRULE:FREQ=DAILY;INTERVAL=1",
  "startDate": "2019-11-13T03:00:00+0000",
  "dueDate": "2019-11-14T03:00:00+0000",
  "reminders": [ "TRIGGER:P0DT9H0M0S", "TRIGGER:PT0S" ],
  "tags": [ "work", "urgent" ],
  "priority": 1,
  "status": 0,
  "completedTime": "2019-11-13T03:00:00+0000",
  "sortOrder": 12345
}`

func TestTaskBulkCompleteManyJSON(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/completeTasks", `["taskId1","taskId2"]`,
		func(t *testing.T, body []byte) {
			var req api.CompleteTasksRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatalf("unmarshal complete request: %v (body %s)", err, body)
			}
			if req.ProjectId == nil || *req.ProjectId != "proj-1" {
				t.Errorf("projectId = %v, want proj-1", req.ProjectId)
			}
			if len(req.TaskIds) != 2 || req.TaskIds[0] != "taskId1" || req.TaskIds[1] != "taskId2" {
				t.Errorf("taskIds = %v, want [taskId1 taskId2]", req.TaskIds)
			}
		})

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "complete-many", "--ids", "taskId1, taskId2",
			"--project", "proj-1", "--json")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	if !strings.Contains(out, `"taskId1"`) || !strings.Contains(out, `"taskId2"`) {
		t.Errorf("JSON output missing ids:\n%s", out)
	}
	if !strings.Contains(out, "\n  \"taskId1\"") {
		t.Errorf("JSON output is not pretty-printed:\n%s", out)
	}
}

func TestTaskBulkCompleteManyText(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/completeTasks", `["taskId1","taskId2"]`,
		func(t *testing.T, body []byte) {
			var req api.CompleteTasksRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatalf("unmarshal complete request: %v", err)
			}
			if req.ProjectId != nil {
				t.Errorf("projectId = %q, want omitted for inbox", *req.ProjectId)
			}
			if len(req.TaskIds) != 2 {
				t.Errorf("taskIds = %v, want 2 ids", req.TaskIds)
			}
		})

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "complete-many", "--ids", "taskId1,taskId2")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	want := "Completed 2 task(s):\ntaskId1\ntaskId2\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestTaskBulkMoveFlagsJSON(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/move", `[{"id":"69a850f8b9061f374d54a046","etag":"43p2zso1"}]`,
		func(t *testing.T, body []byte) {
			var moves []api.TaskMove
			if err := json.Unmarshal(body, &moves); err != nil {
				t.Fatalf("unmarshal move request: %v (body %s)", err, body)
			}
			if len(moves) != 2 {
				t.Fatalf("moves = %v, want 2 entries", moves)
			}
			for i, m := range moves {
				if m.FromProjectId != "from-p" || m.ToProjectId != "to-p" {
					t.Errorf("moves[%d] = %+v, want from-p/to-p", i, m)
				}
			}
			if moves[0].TaskId != "id1" || moves[1].TaskId != "id2" {
				t.Errorf("moves taskIds = %q,%q, want id1,id2", moves[0].TaskId, moves[1].TaskId)
			}
		})

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "move", "--from", "from-p", "--to", "to-p",
			"--ids", "id1,id2", "--json")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	if !strings.Contains(out, `"id": "69a850f8b9061f374d54a046"`) ||
		!strings.Contains(out, `"etag": "43p2zso1"`) {
		t.Errorf("JSON output missing move result:\n%s", out)
	}
}

func TestTaskBulkMoveDataJSON(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/move", `[{"id":"69a850f8b9061f374d54a046","etag":"43p2zso1"}]`,
		func(t *testing.T, body []byte) {
			var moves []api.TaskMove
			if err := json.Unmarshal(body, &moves); err != nil {
				t.Fatalf("unmarshal move request: %v (body %s)", err, body)
			}
			want := api.TaskMove{
				FromProjectId: "69a850ef1c20d2030e148fdd",
				ToProjectId:   "69a850f41c20d2030e148fdf",
				TaskId:        "69a850f8b9061f374d54a046",
			}
			if len(moves) != 1 || moves[0] != want {
				t.Errorf("moves = %+v, want [%+v]", moves, want)
			}
		})

	data := `[{"fromProjectId":"69a850ef1c20d2030e148fdd","toProjectId":"69a850f41c20d2030e148fdf","taskId":"69a850f8b9061f374d54a046"}]`
	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "move", "--data", data, "--json")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	if !strings.Contains(out, `"etag": "43p2zso1"`) {
		t.Errorf("JSON output missing etag:\n%s", out)
	}
}

func TestTaskBulkMoveText(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/move", `[{"id":"69a850f8b9061f374d54a046","etag":"43p2zso1"}]`, nil)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "move", "--from", "p1", "--to", "p2", "--ids", "t1")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	for _, want := range []string{"ID", "ETAG", "69a850f8b9061f374d54a046", "43p2zso1"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestTaskBulkBatchDataJSON(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/batch", `{"id2etag":{"taskId":"43p2zso1"},"id2error":{}}`,
		func(t *testing.T, body []byte) {
			var req api.TaskBatchRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatalf("unmarshal batch request: %v (body %s)", err, body)
			}
			if req.Add == nil || len(*req.Add) != 1 {
				t.Fatalf("add = %v, want 1 entry", req.Add)
			}
			if (*req.Add)[0].Title != "New task" || (*req.Add)[0].ProjectId != "projectId" {
				t.Errorf("add[0] = %+v", (*req.Add)[0])
			}
			if req.Update == nil || len(*req.Update) != 1 {
				t.Fatalf("update = %v, want 1 entry", req.Update)
			}
			if (*req.Update)[0].Id != "taskId" || (*req.Update)[0].ProjectId != "projectId" {
				t.Errorf("update[0] = %+v", (*req.Update)[0])
			}
			if title := Str((*req.Update)[0].Title); title != "Updated task" {
				t.Errorf("update[0].title = %q, want Updated task", title)
			}
		})

	data := `{"add":[{"title":"New task","projectId":"projectId"}],"update":[{"id":"taskId","projectId":"projectId","title":"Updated task"}]}`
	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "batch", "--data", data, "--json")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	if !strings.Contains(out, `"id2etag"`) || !strings.Contains(out, `"taskId": "43p2zso1"`) {
		t.Errorf("JSON output missing id2etag:\n%s", out)
	}
}

func TestTaskBulkBatchFileText(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/batch",
		`{"id2etag":{"taskId":"43p2zso1","taskId2":"etag2"},"id2error":{"badId":"NOT_EXISTED"}}`,
		func(t *testing.T, body []byte) {
			var req api.TaskBatchRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatalf("unmarshal batch request: %v (body %s)", err, body)
			}
			if req.Add == nil || len(*req.Add) != 1 || (*req.Add)[0].Title != "From file" {
				t.Errorf("add = %v, want From file entry", req.Add)
			}
		})

	dir := t.TempDir()
	path := filepath.Join(dir, "batch.json")
	content := `{"add":[{"title":"From file","projectId":"p1"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp batch file: %v", err)
	}

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "batch", "--file", path)
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	for _, want := range []string{
		"OK: 2 task(s)",
		"taskId -> 43p2zso1",
		"taskId2 -> etag2",
		"Errors:",
		"badId -> NOT_EXISTED",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestTaskBulkBatchRequiresDataOrFile(t *testing.T) {
	taskBulkResetFlags()
	var err error
	_ = captureOutput(t, func() {
		err = runCmd(t, "task", "batch")
	})
	if err == nil {
		t.Fatal("expected error when neither --data nor --file is given")
	}
	if !strings.Contains(err.Error(), "one of --data or --file is required") {
		t.Errorf("error = %v, want data/file message", err)
	}
}

func TestTaskBulkAssignJSON(t *testing.T) {
	taskBulkResetFlags()
	respBody := `{
  "id": "63b7bebb91c0a5474805fcd4",
  "projectId": "6226ff9877acee87727f6bca",
  "title": "Task Title",
  "assigneeUsername": "alice",
  "status": 0
}`
	taskBulkSetup(t, "/open/v1/task/assign", respBody,
		func(t *testing.T, body []byte) {
			var req api.TaskAssignRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatalf("unmarshal assign request: %v (body %s)", err, body)
			}
			if req.ProjectId != "6226ff9877acee87727f6bca" {
				t.Errorf("projectId = %q", req.ProjectId)
			}
			if req.TaskId != "63b7bebb91c0a5474805fcd4" {
				t.Errorf("taskId = %q", req.TaskId)
			}
			if req.AssigneeUsername != "alice" {
				t.Errorf("assigneeUsername = %q", req.AssigneeUsername)
			}
		})

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "assign", "63b7bebb91c0a5474805fcd4",
			"--project", "6226ff9877acee87727f6bca", "--username", "alice", "--json")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	if !strings.Contains(out, `"assigneeUsername": "alice"`) {
		t.Errorf("JSON output missing assigneeUsername:\n%s", out)
	}
}

func TestTaskBulkAssignText(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/assign", taskBulkDocTask, nil)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "assign", "63b7bebb91c0a5474805fcd4",
			"--project", "6226ff9877acee87727f6bca", "--username", "alice")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	for _, want := range []string{
		"Assigned 63b7bebb91c0a5474805fcd4 to alice",
		"ID: 63b7bebb91c0a5474805fcd4",
		"Title: Task Title",
		"Assignee: alice",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestTaskBulkUnassignJSON(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/unassign", taskBulkDocTask,
		func(t *testing.T, body []byte) {
			var req api.TaskUnassignRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatalf("unmarshal unassign request: %v (body %s)", err, body)
			}
			if req.ProjectId != "6226ff9877acee87727f6bca" {
				t.Errorf("projectId = %q", req.ProjectId)
			}
			if req.TaskId != "63b7bebb91c0a5474805fcd4" {
				t.Errorf("taskId = %q", req.TaskId)
			}
		})

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "unassign", "63b7bebb91c0a5474805fcd4",
			"--project", "6226ff9877acee87727f6bca", "--json")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	if !strings.Contains(out, `"id": "63b7bebb91c0a5474805fcd4"`) {
		t.Errorf("JSON output missing task id:\n%s", out)
	}
}

func TestTaskBulkUnassignText(t *testing.T) {
	taskBulkResetFlags()
	taskBulkSetup(t, "/open/v1/task/unassign", taskBulkDocTask, nil)

	var err error
	out := captureOutput(t, func() {
		err = runCmd(t, "task", "unassign", "63b7bebb91c0a5474805fcd4",
			"--project", "6226ff9877acee87727f6bca")
	})
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	want := "Unassigned 63b7bebb91c0a5474805fcd4\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}
