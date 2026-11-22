package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

type V3BatchTask struct {
	V2Task
	Deleted int `json:"deleted"`
}

func (t *V3BatchTask) UnmarshalJSON(data []byte) error {
	task, deleted, err := decodeWebTask(data)
	if err != nil {
		return err
	}
	*t = V3BatchTask{V2Task: task, Deleted: deleted}
	return nil
}

type V3SyncTasks struct {
	Add       []V3BatchTask   `json:"add"`
	Update    []V3BatchTask   `json:"update"`
	Delete    json.RawMessage `json:"delete"`
	TagUpdate json.RawMessage `json:"tagUpdate"`
}

type V3BatchCheckResult struct {
	CheckPoint   int64       `json:"checkPoint"`
	SyncTaskBean V3SyncTasks `json:"syncTaskBean"`
}

// BatchCheck requests a full task sync at checkpoint zero or changes since
// the supplied checkpoint. Pass the returned CheckPoint for a subsequent sync.
func (c *V2Client) BatchCheck(ctx context.Context, checkPoint int64) (V3BatchCheckResult, error) {
	if checkPoint < 0 {
		return V3BatchCheckResult{}, errors.New("batch checkpoint must be nonnegative")
	}
	path := "/batch/check/" + strconv.FormatInt(checkPoint, 10)
	return webRequest[V3BatchCheckResult](ctx, c, "v3", http.MethodGet, path, nil, nil)
}
