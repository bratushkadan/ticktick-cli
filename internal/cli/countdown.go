package cli

import (
	"io"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

var countdownListCmd = &cobra.Command{
	Use:   "list",
	Short: "List countdowns",
	Args:  cobra.NoArgs,
	Example: `  ticktick countdown list
  ticktick countdown list --json

Example API response:
[
  {
    "id": "countdown-1",
    "type": 0,
    "name": "Birthday",
    "date": 20260407,
    "color": "#4D8CF5",
    "reminders": [],
    "sortOrder": 0
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListCountdownsWithResponse(cmd.Context())
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			var countdowns []api.OpenCountdown
			if resp.JSON200 != nil {
				countdowns = *resp.JSON200
			}
			rows := make([][]string, 0, len(countdowns))
			for _, cd := range countdowns {
				rows = append(rows, []string{
					Str(cd.Id),
					Str(cd.Name),
					StampToDate(I32(cd.Date)),
					Str(cd.Color),
					Str(cd.RepeatFlag),
				})
			}
			Table(w, []string{"ID", "NAME", "DATE", "COLOR", "REPEAT"}, rows)
		})
	},
}

func init() {
	countdownCmd.AddCommand(countdownListCmd)
}
