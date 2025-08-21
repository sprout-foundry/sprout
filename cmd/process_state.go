package cmd

import (
	"fmt"
	"os"

	"github.com/alantheprice/ledit/pkg/orchestration"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/spf13/cobra"
)

var statePathFlag string

var processStatusCmd = &cobra.Command{
	Use:   "process-status",
	Short: "Show a summary of the current orchestration state",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := statePathFlag
		if path == "" {
			path = ".ledit/orchestration_state.json"
		}
		plan, err := orchestration.LoadState(path)
		if err != nil {
			return fmt.Errorf("failed to load state from %s: %w", path, err)
		}
		orchestration.PrintStateSummary(plan)
		return nil
	},
}

var processClearStateCmd = &cobra.Command{
	Use:   "process-clear-state",
	Short: "Delete the saved orchestration state file",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := statePathFlag
		if path == "" {
			path = ".ledit/orchestration_state.json"
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
		logger := utils.GetLogger(true) // Use true for skipPrompt since this is internal
		logger.Logf("Removed state: %s", path)
		return nil
	},
}

func init() {
	processStatusCmd.Flags().StringVar(&statePathFlag, "state", "", "Path to orchestration state file")
	processClearStateCmd.Flags().StringVar(&statePathFlag, "state", "", "Path to orchestration state file")
	rootCmd.AddCommand(processStatusCmd)
	rootCmd.AddCommand(processClearStateCmd)
}
