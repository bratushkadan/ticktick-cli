package cli

import (
	"bytes"
	"encoding/json"
	"io"
)

// taskStatusSlug interprets status in the schema of the containing record.
func taskStatusSlug(status int, checklistItem bool) string {
	if checklistItem {
		switch status {
		case 0:
			return "open"
		case 1:
			return "completed_subtask"
		}
		return "unknown"
	}

	switch status {
	case -1:
		return "abandoned"
	case 0:
		return "open"
	case 2:
		return "completed"
	default:
		return "unknown"
	}
}

// renderTasks preserves API fields while adding slugs to task and checklist records.
func renderTasks(raw []byte, textFn func(io.Writer)) error {
	if !flagJSON {
		return Render(raw, textFn)
	}
	body, err := addTaskStatusSlugs(raw, false)
	if err != nil {
		return err
	}
	return Render(body, textFn)
}

func addTaskStatusSlugs(raw json.RawMessage, checklistItem bool) (json.RawMessage, error) {
	var rows []json.RawMessage
	var object map[string]json.RawMessage
	switch trimmed := bytes.TrimSpace(raw); {
	case len(trimmed) == 0:
		return raw, nil
	case trimmed[0] == '[':
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, err
		}
		for i, row := range rows {
			updated, err := addTaskStatusSlugs(row, checklistItem)
			if err != nil {
				return nil, err
			}
			rows[i] = updated
		}
		return json.Marshal(rows)
	case trimmed[0] == '{':
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		var status *int
		if json.Unmarshal(object["status"], &status) == nil && status != nil {
			object["status_slug"], _ = json.Marshal(taskStatusSlug(*status, checklistItem))
		}
		for _, key := range []string{"tasks", "items"} {
			if nested, ok := object[key]; ok {
				updated, err := addTaskStatusSlugs(nested, key == "items")
				if err != nil {
					return nil, err
				}
				object[key] = updated
			}
		}
		return json.Marshal(object)
	default:
		return raw, nil
	}
}
