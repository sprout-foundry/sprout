package cmd

import (
	"encoding/json"
	"os"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
	"github.com/spf13/cobra"
)

var insightsCmd = &cobra.Command{
	Use:   "insights",
	Short: "Show inferred project goals and insights",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := config.LoadOrInitConfig(false)
		logger := utils.GetLogger(cfg.SkipPrompt)

		_ = os.MkdirAll(".ledit", os.ModePerm)
		// Ensure workspace file exists and is up to date
		_ = workspace.GetWorkspaceContext("", cfg)

		ws, err := workspace.LoadWorkspaceFile()
		if err != nil {
			logger.Logf("Failed to load workspace file: %v\n", err)
			return
		}

		// Print Goals and Insights as pretty JSON
		out := map[string]any{
			"project_goals":    ws.ProjectGoals,
			"project_insights": ws.ProjectInsights,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		ui.Out().Printf("%s\n", string(b))
	},
}

func init() {
	rootCmd.AddCommand(insightsCmd)
}
