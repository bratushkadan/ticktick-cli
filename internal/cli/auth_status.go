package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newAuthStatusCmd() *cobra.Command {
	var webEnvFile string
	cmd := &cobra.Command{
		Use:   "auth-status",
		Short: "Check local API authentication configuration without making requests",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := configDirectory()
			values, openErr := loadDotenv(dir)
			if openErr == nil {
				_, openErr = resolveToken(dir, values)
			}
			webFile := flagEnvFile
			if cmd.Flags().Changed("web-env-file") {
				webFile = webEnvFile
			}
			_, webErr := resolveV2Cookie(webFile)
			tokenConfigured, cookieConfigured := openErr == nil, webErr == nil
			body := fmt.Sprintf(`{"token_configured":%t,"cookie_configured":%t}`, tokenConfigured, cookieConfigured)
			return Render([]byte(body), func(w io.Writer) {
				fmt.Fprintf(w, "API token configured: %t\nWeb API cookie configured: %t\n", tokenConfigured, cookieConfigured)
			})
		},
	}
	cmd.Flags().StringVar(&webEnvFile, "web-env-file", "", "Override the dotenv file for the web API only (empty uses the default)")
	return cmd
}
