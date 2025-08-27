// Agent command implementation
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/providers"
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
	Short: "AI agent mode - interactive or direct execution of development tasks",
	Long: `Simplified Agent mode with streamlined workflow for code updates, questions, and commands.

The agent can run in two modes:

1. **Interactive Mode** (with --ui flag):
   - Run "ledit agent --ui" to start interactive TUI mode
   - Type requests in the bottom input box and press Enter to execute
   - Watch real-time progress and logs
   - Perfect for iterative development workflows

2. **Direct Mode** (with command line arguments):
   - Run "ledit agent \"your request\"" for one-shot execution
   - Ideal for scripting and automation

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
  # Interactive mode
  ledit agent --ui
  
  # Direct mode
  ledit agent "Add better error handling to the main function"
  ledit agent "How does the authentication system work?"
  ledit agent "run build command"
  ledit agent "Fix the bug where users can't login"`,
	Args: cobra.MaximumNArgs(1), // Allow 0 or 1 args for interactive mode
	RunE: func(cmd *cobra.Command, args []string) error {
		// If no arguments provided, check if UI is available for interactive mode
		if len(args) == 0 {
			if uiPkg.IsUIActive() {
				// Start interactive TUI mode
				return startInteractiveTUI()
			} else {
				return fmt.Errorf("no intent provided. Use: ledit agent \"<your request>\" or enable UI mode with --ui flag")
			}
		}
		userIntent := strings.Join(args, " ")
		// Mark this invocation as coming from agent for downstream logic (e.g., automated review policy)
		_ = os.Setenv("LEDIT_FROM_AGENT", "1")
		// Propagate dry-run via env var for simplicity; config loader reads os.Getenv in future enhancement
		if agentDryRun {
			_ = os.Setenv("LEDIT_DRY_RUN", "1")
		}
		// If UI is enabled, start TUI in background and route output
		if uiPkg.IsUIActive() {
			uiPkg.SetDefaultSink(uiPkg.TuiSink{})
			go func() { _ = tuiPkg.Run() }()
		}

		// Default to simplified agent
		err := agent.RunSimplifiedAgent(userIntent, agentSkipPrompt, agentModel)

		// If there's an error, use graceful exit with token usage information
		if err != nil {
			// Try to get token usage information from config
			var tokenUsage interface{}
			var modelName string

			if cfg, cfgErr := config.LoadOrInitConfig(agentSkipPrompt); cfgErr == nil && cfg != nil {
				if cfg.LastTokenUsage != nil {
					tokenUsage = cfg.LastTokenUsage
				}
				modelName = cfg.EditingModel
			}

			// Print graceful exit message
			gracefulExitMsg := prompts.NewGracefulExitWithTokenUsage(
				"AI agent processing your request",
				err,
				tokenUsage,
				modelName,
			)
			fmt.Fprint(os.Stderr, gracefulExitMsg)
			os.Exit(1)
		}

		// Handle token usage summary based on context
		if cfg, cfgErr := config.LoadOrInitConfig(agentSkipPrompt); cfgErr == nil && cfg != nil && cfg.LastTokenUsage != nil {
			// Use provider interface for cost calculation
			if provider, err := providers.GetProvider(cfg.EditingModel); err == nil {
				cost := provider.CalculateCost(providers.TokenUsage{
					PromptTokens:     cfg.LastTokenUsage.PromptTokens,
					CompletionTokens: cfg.LastTokenUsage.CompletionTokens,
					TotalTokens:      cfg.LastTokenUsage.TotalTokens,
				})

				// Only show summary in console mode - UI shows this in the header
				uiPkg.PrintfContext(false, "Token Usage: %d prompt + %d completion = %d total (Cost: $%.4f)\n",
					cfg.LastTokenUsage.PromptTokens,
					cfg.LastTokenUsage.CompletionTokens,
					cfg.LastTokenUsage.TotalTokens,
					cost)
			}
		}
		return nil
	},
}

// startInteractiveTUI starts the TUI in interactive agent mode
func startInteractiveTUI() error {
	// Set TUI as output sink
	uiPkg.SetDefaultSink(uiPkg.TuiSink{})

	// Start TUI with interactive agent mode
	if err := tuiPkg.RunInteractiveAgent(); err != nil {
		return fmt.Errorf("failed to start interactive TUI: %w", err)
	}
	return nil
}
