package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Documented example responses (docs/openapi.md, "Project" section).
const (
	projectListJSONResponse = `[{
"id": "6226ff9877acee87727f6bca",
"name": "project name",
"color": "#F18181",
"closed": false,
"groupId": "6436176a47fd2e05f26ef56e",
"viewMode": "list",
"permission": "write",
"kind": "TASK"
}]`

	projectGetJSONResponse = `{
	"id": "6226ff9877acee87727f6bca",
	"name": "project name",
	"color": "#F18181",
	"closed": false,
	"groupId": "6436176a47fd2e05f26ef56e",
	"viewMode": "list",
	"kind": "TASK"
}`

	projectDataJSONResponse = `{
"project": {
	"id": "6226ff9877acee87727f6bca",
	"name": "project name",
	"color": "#F18181",
	"closed": false,
	"groupId": "6436176a47fd2e05f26ef56e",
	"viewMode": "list",
	"kind": "TASK"
},
"tasks": [{
	"id": "6247ee29630c800f064fd145",
	"isAllDay": true,
	"projectId": "6226ff9877acee87727f6bca",
	"title": "Task Title",
	"content": "Task Content",
	"priority": 1,
	"status": 0,
	"sortOrder": 12345
}],
"columns": [{
	"id": "6226ff9e76e5fc39f2862d1b",
	"projectId": "6226ff9877acee87727f6bca",
	"name": "Column Name",
	"sortOrder": 0
}]
}`

	projectMembersJSONResponse = `[
  {
    "username": "member@example.com",
    "displayName": "Project member",
    "self": false
  }
]`

	projectCreateJSONResponse = `{
"id": "6226ff9877acee87727f6bca",
"name": "project name",
"color": "#F18181",
"sortOrder": 0,
"viewMode": "list",
"kind": "TASK"
}`

	projectUpdateJSONResponse = `{
"id": "6226ff9877acee87727f6bca",
"name": "Project Name",
"color": "#F18181",
"sortOrder": 7,
"viewMode": "kanban",
"kind": "TASK"
}`

	projectID = "6226ff9877acee87727f6bca"
)

// projectSetup starts an httptest server wired to all project endpoints and
// points the CLI at it. The handler asserts method, path, auth header and
// request payloads; it returns documented example responses.
func projectSetup(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /open/v1/project":
			q := r.URL.Query()
			if got, want := q.Get("offset"), "5"; got != want {
				t.Errorf("offset query = %q, want %q", got, want)
			}
			if got, want := q.Get("limit"), "10"; got != want {
				t.Errorf("limit query = %q, want %q", got, want)
			}
			_, _ = io.WriteString(w, projectListJSONResponse)
		case "GET /open/v1/project/" + projectID:
			_, _ = io.WriteString(w, projectGetJSONResponse)
		case "GET /open/v1/project/" + projectID + "/data":
			_, _ = io.WriteString(w, projectDataJSONResponse)
		case "GET /open/v1/project/" + projectID + "/members":
			_, _ = io.WriteString(w, projectMembersJSONResponse)
		case "POST /open/v1/project":
			body := projectReadBody(t, r)
			var req map[string]any
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatalf("create body is not JSON: %v (%s)", err, body)
			}
			if got, want := req["name"], "project name"; got != want {
				t.Errorf("create name = %v, want %v", got, want)
			}
			if got, want := req["color"], "#F18181"; got != want {
				t.Errorf("create color = %v, want %v", got, want)
			}
			if got, want := req["sortOrder"], float64(0); got != want {
				t.Errorf("create sortOrder = %v, want %v", got, want)
			}
			if got, want := req["viewMode"], "list"; got != want {
				t.Errorf("create viewMode = %v, want %v", got, want)
			}
			if got, want := req["kind"], "TASK"; got != want {
				t.Errorf("create kind = %v, want %v", got, want)
			}
			_, _ = io.WriteString(w, projectCreateJSONResponse)
		case "POST /open/v1/project/" + projectID:
			body := projectReadBody(t, r)
			var req map[string]any
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatalf("update body is not JSON: %v (%s)", err, body)
			}
			if got, want := req["name"], "Project Name"; got != want {
				t.Errorf("update name = %v, want %v", got, want)
			}
			if got, want := req["color"], "#F18181"; got != want {
				t.Errorf("update color = %v, want %v", got, want)
			}
			if got, want := req["sortOrder"], float64(7); got != want {
				t.Errorf("update sortOrder = %v, want %v", got, want)
			}
			if got, want := req["viewMode"], "kanban"; got != want {
				t.Errorf("update viewMode = %v, want %v", got, want)
			}
			if got, want := req["kind"], "TASK"; got != want {
				t.Errorf("update kind = %v, want %v", got, want)
			}
			_, _ = io.WriteString(w, projectUpdateJSONResponse)
		case "DELETE /open/v1/project/" + projectID:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	useTestAPIServer(t, srv)
	return srv
}

func projectReadBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	return b
}

func TestProjectListJSON(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "list", "--offset", "5", "--limit", "10", "--json"); err != nil {
			t.Fatalf("project list --json: %v", err)
		}
	})
	if !strings.Contains(out, "6226ff9877acee87727f6bca") {
		t.Errorf("list --json missing project id:\n%s", out)
	}
	if !strings.Contains(out, `"name": "project name"`) {
		t.Errorf("list --json missing pretty-printed name:\n%s", out)
	}
}

func TestProjectListText(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "list", "--offset", "5", "--limit", "10"); err != nil {
			t.Fatalf("project list: %v", err)
		}
	})
	for _, want := range []string{"ID", "NAME", "KIND", "VIEW", "CLOSED", "GROUP",
		"6226ff9877acee87727f6bca", "project name", "TASK", "list", "false",
		"6436176a47fd2e05f26ef56e"} {
		if !strings.Contains(out, want) {
			t.Errorf("list text missing %q:\n%s", want, out)
		}
	}
}

func TestProjectGetJSON(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "get", projectID, "--json"); err != nil {
			t.Fatalf("project get --json: %v", err)
		}
	})
	if !strings.Contains(out, `"id": "6226ff9877acee87727f6bca"`) {
		t.Errorf("get --json missing id:\n%s", out)
	}
}

func TestProjectGetText(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "get", projectID); err != nil {
			t.Fatalf("project get: %v", err)
		}
	})
	for _, want := range []string{
		"ID: 6226ff9877acee87727f6bca",
		"Name: project name",
		"Color: #F18181",
		"Kind: TASK",
		"View mode: list",
		"Closed: false",
		"Group: 6436176a47fd2e05f26ef56e",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("get text missing %q:\n%s", want, out)
		}
	}
}

func TestProjectDataJSON(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "data", projectID, "--json"); err != nil {
			t.Fatalf("project data --json: %v", err)
		}
	})
	if !strings.Contains(out, `"title": "Task Title"`) {
		t.Errorf("data --json missing task title:\n%s", out)
	}
	if !strings.Contains(out, `"Column Name"`) {
		t.Errorf("data --json missing column name:\n%s", out)
	}
}

func TestProjectDataText(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "data", projectID); err != nil {
			t.Fatalf("project data: %v", err)
		}
	})
	for _, want := range []string{
		"Name: project name",
		"Tasks (undone):",
		"Task Title",
		"Columns:",
		"Column Name",
		"SORTORDER",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("data text missing %q:\n%s", want, out)
		}
	}
}

func TestProjectMembersJSON(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "members", projectID, "--json"); err != nil {
			t.Fatalf("project members --json: %v", err)
		}
	})
	if !strings.Contains(out, `"username": "member@example.com"`) {
		t.Errorf("members --json missing username:\n%s", out)
	}
}

func TestProjectMembersText(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "members", projectID); err != nil {
			t.Fatalf("project members: %v", err)
		}
	})
	for _, want := range []string{"USERNAME", "DISPLAY NAME", "SELF",
		"member@example.com", "Project member", "false"} {
		if !strings.Contains(out, want) {
			t.Errorf("members text missing %q:\n%s", want, out)
		}
	}
}

func TestProjectCreateJSON(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "create",
			"--name", "project name",
			"--color", "#F18181",
			"--sort-order", "0",
			"--view-mode", "list",
			"--kind", "TASK",
			"--json"); err != nil {
			t.Fatalf("project create --json: %v", err)
		}
	})
	if !strings.Contains(out, `"id": "6226ff9877acee87727f6bca"`) {
		t.Errorf("create --json missing id:\n%s", out)
	}
}

func TestProjectCreateText(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "create",
			"--name", "project name",
			"--color", "#F18181",
			"--sort-order", "0",
			"--view-mode", "list",
			"--kind", "TASK"); err != nil {
			t.Fatalf("project create: %v", err)
		}
	})
	if want := "Created project 6226ff9877acee87727f6bca: project name\n"; out != want {
		t.Errorf("create text = %q, want %q", out, want)
	}
}

func TestProjectUpdateJSON(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "update", projectID,
			"--name", "Project Name",
			"--color", "#F18181",
			"--sort-order", "7",
			"--view-mode", "kanban",
			"--kind", "TASK",
			"--json"); err != nil {
			t.Fatalf("project update --json: %v", err)
		}
	})
	if !strings.Contains(out, `"name": "Project Name"`) {
		t.Errorf("update --json missing new name:\n%s", out)
	}
}

func TestProjectUpdateText(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "update", projectID,
			"--name", "Project Name",
			"--color", "#F18181",
			"--sort-order", "7",
			"--view-mode", "kanban",
			"--kind", "TASK"); err != nil {
			t.Fatalf("project update: %v", err)
		}
	})
	if want := "Updated project 6226ff9877acee87727f6bca: Project Name\n"; out != want {
		t.Errorf("update text = %q, want %q", out, want)
	}
}

func TestProjectDeleteJSON(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "delete", projectID, "--json"); err != nil {
			t.Fatalf("project delete --json: %v", err)
		}
	})
	if want := `{"ok":true}` + "\n"; out != want {
		t.Errorf("delete --json = %q, want %q", out, want)
	}
}

func TestProjectDeleteText(t *testing.T) {
	projectSetup(t)
	out := captureOutput(t, func() {
		if err := runCmd(t, "project", "delete", projectID); err != nil {
			t.Fatalf("project delete: %v", err)
		}
	})
	if want := "Project deleted.\n"; out != want {
		t.Errorf("delete text = %q, want %q", out, want)
	}
}
