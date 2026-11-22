package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

type cacheMetadata struct {
	UpdatedAt  *time.Time `json:"updatedAt"`
	Generation uint64     `json:"generation,omitempty"`
	ProjectID  string     `json:"projectId,omitempty"`
	Scope      string     `json:"scope,omitempty"`
}

// cacheRecord holds the original API payload and local cache metadata.
// A nil UpdatedAt means the last successful fetch time is unknown.
type cacheRecord struct {
	Payload  json.RawMessage `json:"payload"`
	Metadata cacheMetadata   `json:"metadata"`
}

var errInvalidTaskResponse = errors.New("task response is missing an ID or original data")

// cacheWebTasks persists each successful web API task response, even an empty one.
func cacheWebTasks(ctx context.Context, records []api.V2Task) error {
	dir, err := taskCacheDir()
	if err != nil {
		return fmt.Errorf("cache_write_failed: %w", err)
	}
	if err := mergeCachedTasks(ctx, dir, records); err != nil {
		if errors.Is(err, errInvalidTaskResponse) {
			return err
		}
		return fmt.Errorf("cache_write_failed: %w", err)
	}
	return nil
}

func taskCacheDir() (string, error) {
	if dir := cacheDirOverride(); dir != "" {
		return filepath.Abs(dir)
	}
	dir := configDirectory()
	if dir == "" {
		return "", errors.New("could not locate TickTick cache directory")
	}
	return filepath.Join(dir, "cache"), nil
}

func cacheDirOverride() string {
	return firstValue(flagCacheDir, os.Getenv("TICKTICK_CACHE_DIR"))
}

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cache", Short: "Inspect the local task and comment cache"}
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Check that the cache is usable without contacting TickTick",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := checkTaskCache()
			if err != nil {
				return err
			}
			if err := updateTaskCache(cmd.Context(), dir, func(map[string]cacheRecord, map[string]cacheRecord) (bool, error) {
				return false, nil
			}); err != nil {
				return fmt.Errorf("cache_unavailable: %w", err)
			}
			path := filepath.Join(dir, "cache.json")
			if flagJSON {
				return PrintJSON(map[string]string{"path": path})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cache available at", path)
			return nil
		},
	})
	return cmd
}

func checkTaskCache() (string, error) {
	dir, err := taskCacheDir()
	if err == nil {
		err = probeCacheDirectory(dir)
	}
	if err != nil {
		return "", fmt.Errorf("cache_unavailable: %w", err)
	}
	return dir, nil
}

func probeCacheDirectory(dir string) error {
	if err := ensureCacheDirectory(dir); err != nil {
		return err
	}
	for _, name := range []string{"cache.json", "cache.lock"} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular file", name)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	// The cache uses an atomic replacement, which requires writes to the directory.
	tmp, err := os.CreateTemp(dir, ".cache-check-*")
	if err != nil {
		return err
	}
	closeErr := tmp.Close()
	return errors.Join(closeErr, os.Remove(tmp.Name()))
}

func ensureCacheDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("expected a cache directory")
	}
	if dir == filepath.Join(configDirectory(), "cache") && info.Mode().Perm() != 0o700 {
		return os.Chmod(dir, 0o700)
	}
	// Do not change the permissions of a caller-supplied directory.
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("cache directory must not be writable by other users")
	}
	return nil
}

func mergeCachedTasks(ctx context.Context, dir string, records []api.V2Task) error {
	return updateTaskCache(ctx, dir, func(tasks, entries map[string]cacheRecord) (bool, error) {
		now := time.Now().UTC()
		for _, task := range records {
			raw := task.OriginalJSON()
			if task.ID == "" || len(raw) == 0 {
				return false, errInvalidTaskResponse
			}
			if _, exists := entries[task.ID]; exists && commentTaskRevision(tasks[task.ID].Payload) != commentTaskRevision(raw) {
				invalidateCommentEntry(entries, task.ID)
			}
			tasks[task.ID] = cacheRecord{Payload: raw, Metadata: cacheMetadata{UpdatedAt: &now}}
		}
		return true, nil
	})
}

func cacheEntries(cache map[string]json.RawMessage, key string) (map[string]cacheRecord, bool, error) {
	rawEntries := make(map[string]json.RawMessage)
	if raw, ok := cache[key]; ok {
		if err := json.Unmarshal(raw, &rawEntries); err != nil || rawEntries == nil {
			return nil, false, fmt.Errorf("invalid %s object in cache.json", key)
		}
	}
	entries := make(map[string]cacheRecord, len(rawEntries))
	migrated := false
	for id, raw := range rawEntries {
		var record cacheRecord
		if err := json.Unmarshal(raw, &record); err == nil && len(record.Payload) != 0 {
			entries[id] = record
			continue
		}
		if key == "comments" {
			var legacy struct {
				cacheMetadata
				Items json.RawMessage `json:"items"`
			}
			if err := json.Unmarshal(raw, &legacy); err == nil && len(legacy.Items) != 0 {
				entries[id] = cacheRecord{Payload: legacy.Items, Metadata: legacy.cacheMetadata}
				migrated = true
				continue
			}
		}
		entries[id] = cacheRecord{Payload: raw}
		migrated = true
	}
	return entries, migrated, nil
}

func updateTaskCache(ctx context.Context, dir string, update func(tasks, comments map[string]cacheRecord) (bool, error)) error {
	if err := ensureCacheDirectory(dir); err != nil {
		return err
	}

	// Lock a separate file so the lock survives replacement of cache.json.
	lockPath := filepath.Join(dir, "cache.lock")
	if info, err := os.Lstat(lockPath); err == nil && !info.Mode().IsRegular() {
		return errors.New("cache.lock must be a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lock := flock.New(lockPath)
	defer lock.Close()
	lockContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	locked, err := lock.TryLockContext(lockContext, 50*time.Millisecond)
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("could not lock the TickTick cache")
	}

	path := filepath.Join(dir, "cache.json")
	cache, err := readTaskCache(path)
	if err != nil {
		return err
	}
	tasks, tasksMigrated, err := cacheEntries(cache, "tasks")
	if err != nil {
		return err
	}
	comments, commentsMigrated, err := cacheEntries(cache, "comments")
	if err != nil {
		return err
	}
	changed, err := update(tasks, comments)
	if err != nil {
		return err
	}
	if !changed && !tasksMigrated && !commentsMigrated {
		return nil
	}
	cache["tasks"], err = json.Marshal(tasks)
	if err != nil {
		return err
	}
	cache["comments"], err = json.Marshal(comments)
	if err != nil {
		return err
	}
	return writeTaskCache(path, cache)
}

func readTaskCache(path string) (map[string]json.RawMessage, error) {
	cache := make(map[string]json.RawMessage)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("cache.json must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cache); err != nil || cache == nil {
		return nil, errors.New("invalid cache.json")
	}
	return cache, nil
}

func writeTaskCache(path string, cache map[string]json.RawMessage) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cache-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cache); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
