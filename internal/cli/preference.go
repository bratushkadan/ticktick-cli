package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// preferenceCmd is declared in groups.go as a runnable parent command;
// this file assigns its behavior (never re-add it via AddCommand).
func init() {
	preferenceCmd.Args = cobra.NoArgs
	preferenceCmd.Example = `  ticktick preference
  ticktick preference --json

Example API response:
{
  "timeZone": "America/Los_Angeles"
}`
	preferenceCmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.GetUserPreferencesWithResponse(cmd.Context())
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			var pref api.Preference
			if resp.JSON200 != nil {
				pref = *resp.JSON200
			}
			fmt.Fprintf(w, "Time zone: %s\n", Str(pref.TimeZone))
		})
	}
}
