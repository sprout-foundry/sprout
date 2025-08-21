package cmd

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/spf13/cobra"
)

var (
	forceRefresh bool
)

var promptsCmd = &cobra.Command{
	Use:   "prompts",
	Short: "Manage Ledit prompt templates",
}

var promptsRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh user prompt templates from embedded defaults",
	RunE: func(cmd *cobra.Command, args []string) error {
		pm := prompts.GetPromptManager()
		if err := pm.Refresh(forceRefresh); err != nil {
			return fmt.Errorf("failed to refresh prompts: %w", err)
		}
		return nil
	},
}

func init() {
	promptsCmd.AddCommand(promptsRefreshCmd)
	promptsRefreshCmd.Flags().BoolVar(&forceRefresh, "force", false, "Overwrite all prompts with embedded defaults")
}
