// Agent command for ledit
package cmd

import (
	"fmt"
	"os"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	agentSkipPrompt       bool
	agentModel            string
	agentProvider         string
	agentDryRun           bool
	maxIterations         int
	agentNoStreaming      bool
	agentSystemPromptFile string
	agentSystemPrompt     string
)

func createChatAgent() (*agent.Agent, error) {
	var chatAgent *agent.Agent
	var err error

	if agentProvider != "" && agentModel != "" {
		modelWithProvider := fmt.Sprintf("%s:%s", agentProvider, agentModel)
		chatAgent, err = agent.NewAgentWithModel(modelWithProvider)
	} else if agentModel != "" {
		chatAgent, err = agent.NewAgentWithModel(agentModel)
	} else {
		chatAgent, err = agent.NewAgent()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize agent: %w", err)
	}

	if agentSystemPrompt != "" {
		chatAgent.SetSystemPrompt(agentSystemPrompt)
	} else if agentSystemPromptFile != "" {
		if err := chatAgent.SetSystemPromptFromFile(agentSystemPromptFile); err != nil {
			return nil, fmt.Errorf("failed to load system prompt from file: %w", err)
		}
	}

	chatAgent.SetMaxIterations(maxIterations)

	return chatAgent, nil
}

func init() {
	agentCmd.Flags().BoolVar(&agentSkipPrompt, "skip-prompt", false, "Skip user prompts (enhanced by automated validation)")
	agentCmd.Flags().StringVarP(&agentModel, "model", "m", "", "Model name for agent system")
	agentCmd.Flags().StringVarP(&agentProvider, "provider", "p", "", "Provider to use (openai, chutes, openrouter, deepinfra, zai, ollama, ollama-local, ollama-turbo, lmstudio)")
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "Run tools in simulation mode (enhanced safety)")
	agentCmd.Flags().IntVar(&maxIterations, "max-iterations", 1000, "Maximum iterations before stopping (default: 1000)")
	agentCmd.Flags().BoolVar(&agentNoStreaming, "no-stream", false, "Disable streaming mode (useful for scripts and pipelines)")
	agentCmd.Flags().StringVar(&agentSystemPromptFile, "system-prompt", "", "File path containing custom system prompt")
	agentCmd.Flags().StringVar(&agentSystemPrompt, "system-prompt-str", "", "Direct system prompt string")
}

// agentCmd represents the agent command
var agentCmd = &cobra.Command{
	Use:   "agent [intent]",
	Short: "AI agent for code analysis and editing (default when running 'ledit' alone)",
	Long: `AI agent mode for intelligent code analysis and editing with modern CLI + Web UI.

Features:
• Clean CLI output with automatic web UI startup
• Real-time event streaming to web interface
• Error recovery and malformed tool call detection
• Context management and optimization
• Intelligent fallback and retry mechanisms

The agent runs in two modes:

1. **Interactive Mode**:
   - Clean CLI with real-time streaming
   - Automatic web UI startup on localhost:8800
   - Modern web interface for rich interaction
   - Event-driven communication between CLI and web UI

2. **Direct Mode**:
   - Clean CI-style output for automation
   - Optional web UI for monitoring progress
   - Tool execution with atomic operations
   - Context management and optimization

Examples:
  # Interactive mode (automatic when no arguments provided)
  ledit agent

  # Direct mode
  ledit agent "Add better error handling to the main function"
  ledit agent "How does the authentication system work?"

  # With specific provider and model
  ledit agent --provider openrouter --model "qwen/qwen3-coder-30b" "Fix the login bug"
  ledit agent -p deepinfra -m "deepseek-v3" "Analyze the codebase structure"

  # Disable web UI
  ledit agent --no-web-ui "Analyze this code"`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		chatAgent, err := createChatAgent()
		if err != nil {
			return err
		}

		_ = os.Setenv("LEDIT_FROM_AGENT", "1")
		if agentDryRun {
			_ = os.Setenv("LEDIT_DRY_RUN", "1")
		}

		// Check if we're in a CI environment or non-interactive mode
		isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""

		// Check if stdin is a terminal (not piped)
		stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))

		// We're interactive only if we have a terminal, no args (or UI flag), and not in CI
		isInteractive := (len(args) == 0 || enableUI) && !isCI && stdinIsTerminal

		// Use the new simplified enhanced mode
		return runSimpleEnhancedMode(chatAgent, isInteractive, args)
	},
}