// Package cli implements the ticktick command-line interface.
//
// Output contract (applies to EVERY command):
//   - default: compact human-readable text on stdout;
//   - --json:  raw API response JSON (pretty-printed) on stdout;
//   - errors:  stderr, exit code 1.
package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// Global persistent flags. Declared here, registered in init().
var (
	flagJSON    bool
	flagToken   string
	flagEnvFile string
	flagBaseURL string
	flagTimeout time.Duration
)

const version = "1.1.0"

var rootCmd = &cobra.Command{
	Use:   "ticktick",
	Short: "TickTick Open API CLI — fast, scriptable, full API coverage",
	Long: `ticktick — CLI for the TickTick Open API (https://developer.ticktick.com/docs#/openapi).

Covers all business entities: tasks (incl. comments, batch ops, filters, search),
projects, project groups, columns, tags, focus sessions, countdowns, habits.

OUTPUT
  Default output is compact human-readable text.
  Every command accepts --json to print the raw API response as JSON
  (pretty-printed, stdout) — designed for scripts and AI skills.

AUTHENTICATION
  Token resolution order: --token > environment > dotenv > ~/.config/ticktick/token.
  The environment and dotenv accept TICKTICK_TOKEN, then TICKTICK_MCP_TOKEN.
  Dotenv defaults to ~/.config/ticktick/.env; override it with --env-file.
  Create a personal token: TickTick web app > Settings > Account > API Token.

ENVIRONMENT
  TICKTICK_TOKEN     API token
  TICKTICK_MCP_TOKEN  Fall-back API token (also used by TickTick MCP)
  TICKTICK_BASE_URL  HTTPS API base URL (default https://api.ticktick.com)

EXAMPLES
  ticktick project list
  ticktick task create --project <projectId> --title "Buy milk" --json
  ticktick task comments-batch --project <projectId> --ids t1,t2 --workers 8 --json`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command; errors go to stderr with exit code 1.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false,
		"print raw API response as JSON (works with every command)")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "",
		"API token (overrides environment and config files)")
	rootCmd.PersistentFlags().StringVar(&flagEnvFile, "env-file", "",
		"TickTick dotenv file (default ~/.config/ticktick/.env)")
	rootCmd.PersistentFlags().StringVar(&flagBaseURL, "base-url", "",
		"HTTPS API base URL (overrides TICKTICK_BASE_URL from environment and dotenv)")
	rootCmd.PersistentFlags().DurationVar(&flagTimeout, "timeout", 30*time.Second,
		"HTTP request timeout")

	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print CLI version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return Render([]byte(fmt.Sprintf(`{"name":"ticktick-cli","version":%q}`, version)), func(w io.Writer) {
			fmt.Fprintln(w, "ticktick-cli", version)
		})
	},
}
