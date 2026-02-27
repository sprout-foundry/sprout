// Agent command for ledit
package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	agentSkipPrompt       bool
	agentModel            string
	agentProvider         string
	agentPersona          string
	agentDryRun           bool
	maxIterations         int
	agentNoStreaming      bool
	agentSystemPromptFile string
	agentSystemPrompt     string
	agentUnsafe           bool
	agentNoSubagents      bool
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
	chatAgent.SetBaseSystemPrompt(chatAgent.GetSystemPrompt())

	if agentPersona != "" {
		if err := chatAgent.ApplyPersona(agentPersona); err != nil {
			return nil, fmt.Errorf("failed to apply persona %q: %w", agentPersona, err)
		}
	}

	chatAgent.SetMaxIterations(maxIterations)

	return chatAgent, nil
}

func init() {
	agentCmd.Flags().BoolVar(&agentSkipPrompt, "skip-prompt", false, "Skip user prompts (enhanced by automated validation)")
	agentCmd.Flags().StringVarP(&agentModel, "model", "m", "", "Model name for agent system")
	agentCmd.Flags().StringVarP(&agentProvider, "provider", "p", "", "Provider to use (openai, chutes, openrouter, deepinfra, deepseek, zai, mistral, ollama, ollama-local, ollama-turbo, lmstudio, or custom providers)")
	agentCmd.Flags().StringVar(&agentPersona, "persona", "", "Persona to activate at startup (e.g., general, coder, debugger, tester, code_reviewer, researcher, web_scraper)")
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "Run tools in simulation mode (enhanced safety)")
	agentCmd.Flags().IntVar(&maxIterations, "max-iterations", 1000, "Maximum iterations before stopping (default: 1000)")
	agentCmd.Flags().BoolVar(&agentNoStreaming, "no-stream", false, "Disable streaming mode (useful for scripts and pipelines) (or set LEDIT_NO_STREAM=1)")
	agentCmd.Flags().StringVar(&agentSystemPromptFile, "system-prompt", "", "File path containing custom system prompt")
	agentCmd.Flags().StringVar(&agentSystemPrompt, "system-prompt-str", "", "Direct system prompt string")
	agentCmd.Flags().BoolVar(&agentUnsafe, "unsafe", false, "UNSAFE MODE: Bypass most security checks (still blocks critical system operations)")
	agentCmd.Flags().BoolVar(&agentNoSubagents, "no-subagents", false, "Disable subagent tools (run_subagent, run_parallel_subagents)")
	_ = agentCmd.RegisterFlagCompletionFunc("persona", completePersonaFlag)

	// Initialize environment-based defaults
	cobra.OnInitialize(func() {
		// Check for LEDIT_NO_STREAM environment variable
		if os.Getenv("LEDIT_NO_STREAM") == "1" || os.Getenv("LEDIT_NO_STREAM") == "true" {
			agentNoStreaming = true
		}
		// Check for LEDIT_NO_SUBAGENTS environment variable
		if os.Getenv("LEDIT_NO_SUBAGENTS") == "1" || os.Getenv("LEDIT_NO_SUBAGENTS") == "true" {
			agentNoSubagents = true
		}
	})
}

func completePersonaFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := configuration.Load()
	if err != nil || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return availablePersonaCompletions(cfg, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func availablePersonaCompletions(cfg *configuration.Config, toComplete string) []string {
	if cfg == nil || cfg.SubagentTypes == nil {
		return nil
	}

	prefix := strings.ToLower(strings.TrimSpace(toComplete))
	options := make([]string, 0, len(cfg.SubagentTypes))
	for id, persona := range cfg.SubagentTypes {
		if !persona.Enabled {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(id), prefix) {
			continue
		}
		options = append(options, id)
	}
	sort.Strings(options)
	return options
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
   - Automatic web UI startup on localhost:54321
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
  ledit agent -p deepseek -m "deepseek-chat" "Write Python code for data analysis"

  # Start with a persona
  ledit agent --persona web-scraper "Collect pricing table data from docs pages"

  # With custom provider (configured via 'ledit custom-model add')
  ledit agent --provider my-custom-llm --model "custom-model-v1" "Review this code"

  # Disable web UI
  ledit agent --no-web-ui "Analyze this code"`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		chatAgent, err := createChatAgent()
		if err != nil {
			return err
		}

		// Set unsafe mode if flag is provided
		chatAgent.SetUnsafeMode(agentUnsafe)

		// Disable subagents if flag is set
		if agentNoSubagents {
			os.Setenv("LEDIT_NO_SUBAGENTS", "1")
		}

		if agentDryRun {
			_ = os.Setenv("LEDIT_DRY_RUN", "1")
		}

		// Check if we're in a CI environment or non-interactive mode
		isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""

		// Check if stdin is a terminal (not piped)
		stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))

		// We're interactive only if we have a terminal, no args, and not in CI
		isInteractive := len(args) == 0 && !isCI && stdinIsTerminal

		// Use the new simplified enhanced mode
		return RunAgent(chatAgent, isInteractive, args)
	},
}
