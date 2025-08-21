package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/alantheprice/ledit/pkg/orchestration"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/spf13/cobra"
)

var jsonOut bool

var processValidateCmd = &cobra.Command{
	Use:   "process-validate [process-file]",
	Short: "Validate a multi-agent process file without executing it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		loader := orchestration.NewProcessLoader()
		// LoadProcessFile performs schema validation and sets defaults
		pf, err := loader.LoadProcessFile(path)
		if err != nil {
			return fmt.Errorf("invalid process file: %w", err)
		}
		if jsonOut {
			out := map[string]any{
				"status":              "valid",
				"version":             pf.Version,
				"goal":                pf.Goal,
				"agents":              len(pf.Agents),
				"steps":               len(pf.Steps),
				"settings":            pf.Settings,
				"validation_required": pf.Validation != nil && pf.Validation.Required,
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			_ = enc.Encode(out)
			return nil
		}
		ui.Out().Print("Process file is valid.\n")
		ui.Out().Printf("Version: %s\n", pf.Version)
		ui.Out().Printf("Goal: %s\n", pf.Goal)
		ui.Out().Printf("Agents: %d\n", len(pf.Agents))
		ui.Out().Printf("Steps: %d\n", len(pf.Steps))
		if pf.Settings != nil {
			ui.Out().Printf("Parallel: %t, MaxRetries: %d, StepTimeout: %d, StopOnFailure: %t\n",
				pf.Settings.ParallelExecution, pf.Settings.MaxRetries, pf.Settings.StepTimeout, pf.Settings.StopOnFailure)
		}
		if pf.Validation != nil {
			ui.Out().Printf("Validation - required: %t\n", pf.Validation.Required)
		}
		return nil
	},
}

func init() {
	processValidateCmd.Flags().BoolVar(&jsonOut, "json", false, "Output validation summary as JSON")
	rootCmd.AddCommand(processValidateCmd)
}
