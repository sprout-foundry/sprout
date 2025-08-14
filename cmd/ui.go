package cmd

import (
	"fmt"
	"time"

	"github.com/alantheprice/ledit/pkg/tui"
	uiPkg "github.com/alantheprice/ledit/pkg/ui"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Launch the interactive TUI",
	Long:  "Starts the Ledit terminal UI for a visually rich experience.",
	RunE: func(cmd *cobra.Command, args []string) error {
		uiPkg.SetDefaultSink(uiPkg.TuiSink{})
		if demo, _ := cmd.Flags().GetBool("demo"); demo {
			go runUIDemo()
		}
		if err := tui.Run(); err != nil {
			return fmt.Errorf("failed to start UI: %w", err)
		}
		return nil
	},
}

func init() {
	uiCmd.Flags().Bool("demo", false, "Run a self-test demo that exercises the UI")
}

// runUIDemo publishes fake events to exercise the UI
func runUIDemo() {
	uiPkg.PublishModel("demo:gpt-4o")
	rows := []uiPkg.ProgressRow{
		{Name: "Architect", Status: "working", Step: "design_architecture", Tokens: 1200, Cost: 0.024},
		{Name: "Backend", Status: "idle", Step: "", Tokens: 0, Cost: 0},
		{Name: "Frontend", Status: "idle", Step: "", Tokens: 0, Cost: 0},
	}
	for i := 0; i <= 3; i++ {
		uiPkg.Publish(uiPkg.ProgressSnapshotEvent{Completed: i, Total: 3, Rows: rows, Time: time.Now(), TotalTokens: 1200 * (i + 1), TotalCost: 0.024 * float64(i+1), BaseModel: "demo:gpt-4o"})
		uiPkg.Logf("Demo log line %d", i+1)
		time.Sleep(300 * time.Millisecond)
	}
	uiPkg.PublishStreamStarted()
	for i := 0; i < 10; i++ {
		uiPkg.Logf("Streaming chunk %d...", i+1)
		time.Sleep(150 * time.Millisecond)
	}
	uiPkg.PublishStreamEnded()
	// demo prompt
	go func() {
		time.Sleep(500 * time.Millisecond)
		_, _ = uiPkg.PromptYesNo("Proceed with demo?", true)
		uiPkg.Log("Demo prompt answered.")
	}()
}
