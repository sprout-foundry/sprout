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
	agentDryRun      bool
	agentDirectApply bool
	agentSimplified  bool
	agentUseV2       bool
)

func init() {
	agentCmd.Flags().BoolVar(&agentSkipPrompt, "skip-prompt", false, "Skip user prompt for applying changes")
	// Add a flag to allow users to specify and override the LLM model for agent operations
	agentCmd.Flags().StringVarP(&agentModel, "model", "m", "", "Model name to use with the LLM")
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "Run tools in simulation mode (no writes/shell side-effects)")
	agentCmd.Flags().BoolVar(&agentDirectApply, "direct-apply", false, "Let the orchestration model directly apply changes via tools (experimental)")
	agentCmd.Flags().BoolVar(&agentSimplified, "simplified", true, "Use simplified agent workflow with todos and direct execution (default: true)")
}

// agentCmd represents the agent command
var agentCmd = &cobra.Command{
	Use:   "agent [intent]",
	Short: "AI agent mode - analyzes intent and autonomously decides what actions to take",
	Long: `Simplified Agent mode with streamlined workflow for code updates, questions, and commands.

The agent uses a simplified approach:
• For code updates: Creates todos, executes them via the code command with auto-review, validates builds
• For questions: Responds directly without complex planning
• For commands: Executes commands directly without todo overhead

Workflow:
1. Analyze your intent (code update, question, or command)
2. For code updates: Create prioritized todos and execute them sequentially
3. Each todo is executed via the code command with skip-prompt for auto review
4. Build validation runs after each todo to ensure changes work
5. Questions and commands are handled directly without todos

Examples:
  ledit agent "Add better error handling to the main function"
  ledit agent "How does the authentication system work?"
  ledit agent "run build command"
  ledit agent "Fix the bug where users can't login"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		userIntent := strings.Join(args, " ")
		// Mark this invocation as coming from agent for downstream logic (e.g., automated review policy)
		_ = os.Setenv("LEDIT_FROM_AGENT", "1")
		// Propagate dry-run via env var for simplicity; config loader reads os.Getenv in future enhancement
		if agentDryRun {
			_ = os.Setenv("LEDIT_DRY_RUN", "1")
		}
		// If UI is enabled, start TUI in background and route output
		if uiPkg.Enabled() {
			uiPkg.SetDefaultSink(uiPkg.TuiSink{})
			go func() { _ = tuiPkg.Run() }()
		}

		// Default to simplified agent
		return agent.RunSimplifiedAgent(userIntent, agentSkipPrompt, agentModel)
	},
}
