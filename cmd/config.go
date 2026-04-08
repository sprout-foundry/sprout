package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or manage configuration",
	Long:  `Display and manage ledit configuration. Output is always credential-redacted.`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration (credentials are redacted)",
	Long: `Display the current ledit configuration as JSON.
All credential values (API keys, tokens, secrets in MCP env vars, etc.)
are redacted before output.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		config, err := configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		redacted := configuration.RedactConfig(config)

		data, err := json.MarshalIndent(redacted, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		fmt.Println(string(data))
		return nil
	},
}

func init() {
	configCmd.AddCommand(configShowCmd)
	rootCmd.AddCommand(configCmd)
}
