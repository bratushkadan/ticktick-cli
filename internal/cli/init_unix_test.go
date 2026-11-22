//go:build unix

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitRequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires a non-root user")
	}
	if err := runCmd(t, "init"); err == nil || !strings.Contains(err.Error(), "sudo ticktick init") {
		t.Fatalf("expected sudo instructions, got %v", err)
	}
}

func TestInitializeCachePreservesContentsAndRepairsPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".config", "ticktick", "cache")
	path := filepath.Join(dir, "cache.json")
	if err := initializeCache(dir); err != nil {
		t.Fatal(err)
	}
	assertCacheMode(t, dir, os.ModeDir|0o700)
	assertCacheMode(t, path, 0o600)
	initial, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cache map[string]map[string]json.RawMessage
	if err := json.Unmarshal(initial, &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache) != 2 || cache["tasks"] == nil || len(cache["tasks"]) != 0 ||
		cache["comments"] == nil || len(cache["comments"]) != 0 {
		t.Fatalf("unexpected initial cache: %s", initial)
	}

	saved := `{"tasks":{"task-1":{"id":"task-1"}},"comments":{}}`
	if err := os.WriteFile(path, []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := initializeCache(dir); err != nil {
		t.Fatal(err)
	}
	assertCacheMode(t, dir, os.ModeDir|0o700)
	assertCacheMode(t, path, 0o600)
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != saved {
		t.Fatalf("cache changed on repeat init: %s", current)
	}
}

func TestInitializeCacheRejectsSymlink(t *testing.T) {
	temp := t.TempDir()
	target := filepath.Join(temp, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(temp, "cache")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "cache.json")); err != nil {
		t.Fatal(err)
	}
	if err := initializeCache(dir); err == nil {
		t.Fatal("expected init to reject a cache file symlink")
	}
	assertCacheMode(t, target, 0o644)
}

func assertCacheMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != want {
		t.Errorf("%s mode = %s, want %s", path, info.Mode(), want)
	}
}
