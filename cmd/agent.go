// Agent command implementation
package cmd

import (
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	tuiPkg "github.com/alantheprice/ledit/pkg/tui"
	uiPkg "github.com/alantheprice/ledit/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	agentSkipPrompt  bool
	agentModel       string // Declare agentModel variable
	agentVersion     string // v1 (default) or v2 (tool-driven)
	agentDryRun      bool
	agentDirectApply bool
)

func init() {
	agentCmd.Flags().BoolVar(&agentSkipPrompt, "skip-prompt", false, "Skip user prompt for applying changes")
	// Add a flag to allow users to specify and override the LLM model for agent operations
	agentCmd.Flags().StringVarP(&agentModel, "model", "m", "", "Model name to use with the LLM")
	agentCmd.Flags().StringVar(&agentVersion, "agent", "v2", "Agent version: v2 (tool-driven)")
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "Run tools in simulation mode (no writes/shell side-effects)")
	agentCmd.Flags().BoolVar(&agentDirectApply, "direct-apply", false, "Let the orchestration model directly apply changes via tools (experimental)")
}

// agentCmd represents the agent command
var agentCmd = &cobra.Command{
	Use:   "agent [intent]",
	Short: "AI agent mode - analyzes intent and autonomously decides what actions to take",
	Long: `Agent mode allows the LLM to analyze your intent and autonomously decide what actions to take.
The agent uses adaptive decision-making to evaluate progress and respond to changing conditions.

Features:
• Progressive evaluation after each major step
• Intelligent error handling and recovery
• Adaptive plan revision based on learnings
• Context summarization to maintain efficiency
• Smart action selection (continue, revise, validate, complete)

The agent will:
1. Analyze your intent and assess complexity
2. Create a detailed execution plan
3. Execute operations with progress monitoring
4. Evaluate outcomes and decide next actions
5. Handle errors intelligently with context-aware recovery
6. Validate changes and ensure quality

Examples:
  ledit agent "Add better error handling to the main function"
  ledit agent "Refactor the user authentication system"
  ledit agent "Fix the bug where users can't login"
  ledit agent "Add unit tests for the payment processing"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		userIntent := strings.Join(args, " ")
		// Propagate dry-run via env var for simplicity; config loader reads os.Getenv in future enhancement
		if agentDryRun {
			_ = os.Setenv("LEDIT_DRY_RUN", "1")
		}
		// If UI is enabled, start TUI in background and route output
		if uiPkg.Enabled() {
			uiPkg.SetDefaultSink(uiPkg.TuiSink{})
			go func() { _ = tuiPkg.Run() }()
		}
		// Default to v2; v1 has been removed/deprecated
		return agent.RunAgentModeV2(userIntent, agentSkipPrompt, agentModel, agentDirectApply)
	},
}
