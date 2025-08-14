package cmd

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/tui"
    "github.com/alantheprice/ledit/pkg/ui"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Launch the interactive TUI",
	Long:  "Starts the Ledit terminal UI for a visually rich experience.",
	RunE: func(cmd *cobra.Command, args []string) error {
        ui.SetDefaultSink(ui.TuiSink{})
		if err := tui.Run(); err != nil {
			return fmt.Errorf("failed to start UI: %w", err)
		}
		return nil
	},
}

func init() {
}
