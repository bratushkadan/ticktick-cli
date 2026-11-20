package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

// captureOutput runs fn while capturing everything written to os.Stdout.
// Shared test helper for all command test files (package cli).
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// runCmd resets global persistent flags and executes rootCmd with args.
// Always pass every flag your test depends on explicitly: local command
// flags persist between Execute calls.
func runCmd(t *testing.T, args ...string) error {
	t.Helper()
	flagJSON = false
	flagToken = ""
	flagBaseURL = ""
	flagTimeout = 30 * time.Second
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	rootCmd.SetArgs(nil)
	return err
}
