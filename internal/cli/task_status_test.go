package cli

import "testing"

func TestTaskStatusSlugsRespectRecordType(t *testing.T) {
	raw := []byte(`{"tasks":[{"status":-1},{"status":1,"items":[{"status":1},{"status":0}]},{"status":2},{"status":null,"custom":9007199254740993}]}`)
	got, err := addTaskStatusSlugs(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, `{"tasks":[{"status":-1,"status_slug":"abandoned"},{"status":1,"status_slug":"unknown","items":[{"status":1,"status_slug":"completed_subtask"},{"status":0,"status_slug":"open"}]},{"status":2,"status_slug":"completed"},{"status":null,"custom":9007199254740993}]}`)
}
