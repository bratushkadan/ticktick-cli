// Package comments fetches TickTick task comments concurrently.
//
// The TickTick Open API only exposes a single-task comments endpoint
// (GET /open/v1/project/{projectId}/task/{taskId}/comments), so a batch
// fetch is assembled client-side: FetchAll fans the per-task calls out
// across a small pool of workers and returns a taskID → comments map.
package comments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"ticktick-cli/api"
)

const (
	// DefaultWorkers is used when the requested worker count is <= 0.
	DefaultWorkers = 4
	// MaxWorkers caps the number of concurrent requests (1..MaxWorkers).
	MaxWorkers = 8
)

// TaskRef identifies one task to fetch comments for.
type TaskRef struct {
	ProjectID string
	TaskID    string
}

// Result holds the outcome of a batch fetch. Both maps are always
// non-nil, even when refs is empty.
type Result struct {
	// Comments maps taskID → comments. Tasks without comments get an
	// empty (non-nil) slice.
	Comments map[string][]api.OpenComment
	// Errors maps taskID → per-task failure. A failure never aborts
	// the batch; the other tasks are still fetched.
	Errors map[string]error
}

// ClampWorkers normalizes the requested worker count: <=0 → 4 (default),
// >8 → 8, otherwise the value as given (allowed range 1..8).
func ClampWorkers(workers int) int {
	if workers <= 0 {
		return DefaultWorkers
	}
	if workers > MaxWorkers {
		return MaxWorkers
	}
	return workers
}

// FetchAll fetches comments for all refs concurrently using at most
// `workers` goroutines (clamped to 1..8, defaulting to 4).
//
// Per-task failures (transport errors, non-2xx responses, undecodable
// bodies) are recorded in Result.Errors and never abort the batch.
// Context cancellation stops new work and records the context error
// for the tasks that were not processed.
func FetchAll(ctx context.Context, client *api.ClientWithResponses, refs []TaskRef, workers int) Result {
	res := Result{
		Comments: make(map[string][]api.OpenComment, len(refs)),
		Errors:   make(map[string]error, len(refs)),
	}
	// Empty input still yields initialized (non-nil) maps.
	if len(refs) == 0 {
		return res
	}
	if client == nil {
		for _, ref := range refs {
			res.Errors[ref.TaskID] = fmt.Errorf("nil API client")
		}
		return res
	}

	n := ClampWorkers(workers)

	// Buffered so the producer never blocks and no feeder goroutine is
	// needed; workers drain the channel and check ctx before each job.
	jobs := make(chan TaskRef, len(refs))
	for _, ref := range refs {
		jobs <- ref
	}
	close(jobs)

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			for ref := range jobs {
				// Respect cancellation: skip remaining work and record
				// why the task was not fetched.
				if err := ctx.Err(); err != nil {
					mu.Lock()
					if _, done := res.Comments[ref.TaskID]; !done {
						res.Errors[ref.TaskID] = err
					}
					mu.Unlock()
					continue
				}

				list, err := fetchOne(ctx, client, ref)

				mu.Lock()
				if err != nil {
					delete(res.Comments, ref.TaskID)
					res.Errors[ref.TaskID] = err
				} else {
					delete(res.Errors, ref.TaskID)
					res.Comments[ref.TaskID] = list
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Guarantee non-nil slices for every requested task that succeeded.
	for _, ref := range refs {
		if list, ok := res.Comments[ref.TaskID]; ok && list == nil {
			res.Comments[ref.TaskID] = []api.OpenComment{}
		}
	}
	return res
}

// fetchOne performs the single-task comments request.
func fetchOne(ctx context.Context, client *api.ClientWithResponses, ref TaskRef) ([]api.OpenComment, error) {
	resp, err := client.GetTaskCommentsWithResponse(ctx, ref.ProjectID, ref.TaskID)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("empty response")
	}
	if code := resp.StatusCode(); code < 200 || code >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", code, bytes.TrimSpace(resp.Body))
	}
	if resp.JSON200 != nil {
		out := *resp.JSON200
		if out == nil {
			out = []api.OpenComment{}
		}
		return out, nil
	}

	// The generated parser only populates JSON200 when the response has a
	// JSON content type; fall back to decoding the raw body.
	body := bytes.TrimSpace(resp.Body)
	if len(body) == 0 || bytes.Equal(body, []byte("null")) {
		return []api.OpenComment{}, nil
	}
	var out []api.OpenComment
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode comments: %w", err)
	}
	if out == nil {
		out = []api.OpenComment{}
	}
	return out, nil
}
