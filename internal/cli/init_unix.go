//go:build unix

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

const cacheWorkerCommand = "__init-cache"

func init() {
	rootCmd.AddCommand(
		&cobra.Command{
			Use:   "init",
			Short: "Initialize the private local cache",
			Args:  cobra.NoArgs,
			RunE:  runInit,
		},
		&cobra.Command{
			Use:    cacheWorkerCommand,
			Hidden: true,
			Args:   cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return initializeCache(args[0])
			},
		},
	)
}

func runInit(cmd *cobra.Command, args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("ticktick init requires root; run sudo ticktick init")
	}

	uid := os.Getenv("SUDO_UID")
	if uid == "" {
		uid = "0"
	}
	owner, err := user.LookupId(uid)
	if err != nil {
		return fmt.Errorf("resolve cache owner: %w", err)
	}
	if !filepath.IsAbs(owner.HomeDir) {
		return fmt.Errorf("cache owner %s has no absolute home directory", owner.Username)
	}
	dir := filepath.Join(owner.HomeDir, ".config", "ticktick", "cache")

	if owner.Uid == "0" {
		err = initializeCache(dir)
	} else {
		err = initializeCacheAsUser(cmd, owner, dir)
	}
	if err != nil {
		return err
	}
	if flagJSON {
		return PrintJSON(map[string]string{"path": filepath.Join(dir, "cache.json")})
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Cache ready at", filepath.Join(dir, "cache.json"))
	return nil
}

func initializeCacheAsUser(cmd *cobra.Command, owner *user.User, dir string) error {
	uid, err := strconv.ParseUint(owner.Uid, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid cache owner UID: %w", err)
	}
	gid, err := strconv.ParseUint(owner.Gid, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid cache owner GID: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// Run filesystem operations with the cache owner's privileges under sudo.
	worker := exec.CommandContext(cmd.Context(), executable, cacheWorkerCommand, "--", dir)
	worker.Env = []string{}
	worker.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
	}
	output, err := worker.CombinedOutput()
	if err != nil {
		if message := strings.TrimSpace(string(output)); message != "" {
			return errors.New(strings.TrimPrefix(message, "Error: "))
		}
		return fmt.Errorf("start cache initializer: %w", err)
	}
	return nil
}

func initializeCache(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := setCacheMode(dir, os.ModeDir|0o700); err != nil {
		return err
	}

	path := filepath.Join(dir, "cache.json")
	if _, err := os.Lstat(path); err == nil {
		return setCacheMode(path, 0o600)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Publish the complete file atomically without replacing another init's file.
	file, err := os.CreateTemp(dir, ".cache-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()

	if _, err := file.WriteString("{\n  \"tasks\": {},\n  \"comments\": {}\n}\n"); err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(file.Name(), path); errors.Is(err, os.ErrExist) {
		return setCacheMode(path, 0o600)
	} else {
		return err
	}
}

func setCacheMode(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().Type() != mode.Type() {
		return fmt.Errorf("unexpected file type at %s", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	openedInfo, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, openedInfo) {
		return fmt.Errorf("file changed while setting permissions: %s", path)
	}
	if openedInfo.Mode() != mode {
		return f.Chmod(mode.Perm())
	}
	return nil
}
