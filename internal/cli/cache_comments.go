package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"time"

	"ticktick-cli/api"
	"ticktick-cli/internal/comments"
)

const commentCacheTTL = 24 * time.Hour

type taskCommentVersion struct {
	ProjectID    string          `json:"projectId"`
	ModifiedTime json.RawMessage `json:"modifiedTime"`
	CommentCount *int            `json:"commentCount"`
}

type commentCacheSnapshot struct {
	version    string
	generation uint64
}

type commentCache struct {
	dir      string
	scope    string
	snapshot map[string]commentCacheSnapshot
}

func commentTaskRevision(raw json.RawMessage) string {
	var version taskCommentVersion
	if len(raw) == 0 || json.Unmarshal(raw, &version) != nil {
		return ""
	}
	if len(version.ModifiedTime) > 0 && version.ModifiedTime[0] == '"' {
		var timestamp string
		if err := json.Unmarshal(version.ModifiedTime, &timestamp); err == nil {
			if t, err := v2ParseTime(timestamp); err == nil {
				version.ModifiedTime, _ = json.Marshal(t)
			}
		}
	}
	data, _ := json.Marshal(version)
	return string(data)
}

func invalidateCommentEntry(entries map[string]cacheRecord, id string) {
	entry := entries[id]
	entry.Payload = nil
	entry.Metadata.Generation++
	entries[id] = entry
}

func commentCacheExpired(entry cacheRecord, now time.Time) bool {
	return entry.Metadata.UpdatedAt == nil || now.Sub(*entry.Metadata.UpdatedAt) > commentCacheTTL
}

// prepareCommentCache plans the API requests and records the cache versions
// needed to store their results safely. With useCache off, every requested task
// goes to the API. With useCache on, fresh lists are reused and expired lists
// for the same credentials are queued for refresh, even outside refs.
func prepareCommentCache(ctx context.Context, scope [32]byte, refs []comments.TaskRef, useCache bool) (commentCache, map[string][]api.OpenComment, []comments.TaskRef, error) {
	dir, err := taskCacheDir()
	if err != nil {
		return commentCache{}, nil, nil, err
	}
	cache := commentCache{dir: dir, scope: hex.EncodeToString(scope[:]), snapshot: make(map[string]commentCacheSnapshot, len(refs))}
	hits := make(map[string][]api.OpenComment)
	var fetch []comments.TaskRef
	err = updateTaskCache(ctx, dir, func(tasks, entries map[string]cacheRecord) (bool, error) {
		now := time.Now()
		changed := false
		if useCache {
			requested := make(map[string]bool, len(refs))
			for _, ref := range refs {
				requested[ref.TaskID] = true
			}

			// Evict expired payloads before starting HTTP work. Keep their routing
			// metadata so a failed refresh can be retried by a later batch.
			for id, entry := range entries {
				if entry.Metadata.Scope != cache.scope || !commentCacheExpired(entry, now) {
					continue
				}
				if payload := bytes.TrimSpace(entry.Payload); len(payload) > 0 && !bytes.Equal(payload, []byte("null")) {
					invalidateCommentEntry(entries, id)
					changed = true
				}
				if !requested[id] && entry.Metadata.ProjectID != "" {
					fetch = append(fetch, comments.TaskRef{TaskID: id, ProjectID: entry.Metadata.ProjectID})
				}
			}
		}
		for _, ref := range refs {
			entry := entries[ref.TaskID]
			var list []api.OpenComment
			if useCache && entry.Metadata.Scope == cache.scope && entry.Metadata.ProjectID == ref.ProjectID &&
				!commentCacheExpired(entry, now) && json.Unmarshal(entry.Payload, &list) == nil && list != nil {
				hits[ref.TaskID] = list
				continue
			}
			fetch = append(fetch, ref)
		}
		for _, ref := range fetch {
			cache.snapshot[ref.TaskID] = commentCacheSnapshot{
				version:    commentTaskRevision(tasks[ref.TaskID].Payload),
				generation: entries[ref.TaskID].Metadata.Generation,
			}
		}
		return changed, nil
	})
	return cache, hits, fetch, err
}

func (c commentCache) store(ctx context.Context, bodies map[string]json.RawMessage, projects map[string]string) error {
	if len(bodies) == 0 {
		return nil
	}
	return updateTaskCache(ctx, c.dir, func(tasks, entries map[string]cacheRecord) (bool, error) {
		now := time.Now().UTC()
		changed := false
		for id, body := range bodies {
			before := c.snapshot[id]
			current := entries[id]
			if before.generation != current.Metadata.Generation || before.version != commentTaskRevision(tasks[id].Payload) {
				// A concurrent sync, comment mutation, or fetch updated the slot.
				continue
			}
			entries[id] = cacheRecord{Payload: body, Metadata: cacheMetadata{
				UpdatedAt:  &now,
				Generation: current.Metadata.Generation + 1,
				Scope:      c.scope,
				ProjectID:  projects[id],
			}}
			changed = true
		}
		return changed, nil
	})
}

func invalidateTaskComments(ctx context.Context, taskID string) error {
	dir, err := taskCacheDir()
	if err != nil {
		return err
	}
	return updateTaskCache(ctx, dir, func(_, entries map[string]cacheRecord) (bool, error) {
		invalidateCommentEntry(entries, taskID)
		return true, nil
	})
}
