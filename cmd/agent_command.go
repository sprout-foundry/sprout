//go:build !js

// Agent command for sprout
package cmd

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/trace"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	agentSkipPrompt            bool
	agentModel                 string
	agentProvider              string
	agentSessionID             string
	agentLastSession           bool
	agentPersona               string
	agentEAMode                string
	agentDryRun                bool
	maxIterations              int
	agentNoStreaming           bool
	agentShowReasoningTerminal bool
	agentSystemPromptFile      string
	agentSystemPrompt          string
	agentUnsafe                bool
	agentNoSubagents           bool
	agentSubagentModel         string
	agentSubagentProvider      string
	agentResourceDirectory     string
	agentWorkflowConfig        string
	agentNoConnectionCheck     bool
	agentTraceDatasetDir       string
	agentPromptStdin           bool
	agentRiskProfile           string
)

// runStartupPermissionCheck performs a security check on config file permissions
// and logs warnings if any files have insecure permissions.
func runStartupPermissionCheck() error {
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	// Check for symlinks pointing outside the config directory
	symlinkWarnings := security.CheckAllSymlinks(configDir)
	if len(symlinkWarnings) > 0 {
		log.Printf("[security] Symlink warnings:")
		for _, warn := range symlinkWarnings {
			log.Printf("  %s", warn)
		}
	}

	// Run the full permission check
	security.RunStartupCheck(configDir)

	return nil
}

func createChatAgent() (*agent.Agent, error) {
	var chatAgent *agent.Agent
	var err error

	if agentProvider != "" && agentModel != "" {
		modelWithProvider := fmt.Sprintf("%s:%s", agentProvider, agentModel)
		chatAgent, err = agent.NewAgentWithModel(modelWithProvider)
	} else if agentProvider != "" {
		chatAgent, err = agent.NewAgentWithModel(agentProvider)
	} else if agentModel != "" {
		chatAgent, err = agent.NewAgentWithModel(agentModel)
	} else {
		chatAgent, err = agent.NewAgent()
	}

	if err != nil {
		// In daemon mode, if the provider isn't configured or the model isn't
		// available, gracefully proceed without an agent. The web UI will
		// allow the user to configure a provider interactively.
		if daemonMode && (errors.Is(err, agent.ErrProviderNotConfigured) || errors.Is(err, agent.ErrModelNotAvailable)) {
			console.GlyphWarning.Fprintf(os.Stderr, "Provider not configured: %v. Starting web UI for interactive setup.", err)
			return nil, nil
		}
		if noninteractive.IsNonInteractiveHint(err) {
			// The agent startup failed specifically because no provider is
			// configured and stdin is not a terminal. Print the guidance
			// prominently and exit with a non-zero status.
			_, _ = os.Stderr.Write([]byte(fmt.Sprintf("\n%s\n\n", err)))
			return nil, err
		}
		return nil, fmt.Errorf("failed to initialize agent: %w", err)
	}

	// Run startup permission check
	if err := runStartupPermissionCheck(); err != nil {
		log.Printf("[security] %v", err)
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

	if agentEAMode != "" {
		if agentEAMode != "interactive" && agentEAMode != "queue" {
			return nil, fmt.Errorf("invalid --ea-mode value %q: must be 'interactive' or 'queue'", agentEAMode)
		}
		cm := chatAgent.GetConfigManager()
		if cm != nil {
			if err := cm.UpdateConfig(func(c *configuration.Config) error {
				c.EAMode = agentEAMode
				return nil
			}); err != nil {
				return nil, fmt.Errorf("failed to save EA mode config: %w", err)
			}
		}
	}

	if maxIterations > 0 {
		chatAgent.SetMaxIterations(maxIterations)
	}

	return chatAgent, nil
}

func init() {
	agentCmd.Flags().BoolVar(&agentSkipPrompt, "skip-prompt", false, "Skip user prompts (enhanced by automated validation)")
	agentCmd.Flags().BoolVar(&agentNoConnectionCheck, "no-connection-check", false, "Skip provider connection check at startup (saves 1-3 seconds)")
	agentCmd.Flags().StringVarP(&agentModel, "model", "m", "", "Model name for agent system")
	agentCmd.Flags().StringVarP(&agentProvider, "provider", "p", "", "Provider to use (openai, chutes, openrouter, deepinfra, deepseek, zai, mistral, ollama, ollama-local, ollama-turbo, lmstudio, or custom providers)")
	agentCmd.Flags().StringVar(&agentSessionID, "session-id", "", "Resume a specific session ID in the current working directory scope")
	agentCmd.Flags().BoolVar(&agentLastSession, "last-session", false, "Resume the most recent session from the current working directory scope")
	agentCmd.Flags().StringVar(&agentPersona, "persona", "", "Persona to activate at startup (e.g., general, coder, refactor, debugger, tester, code_reviewer, researcher, web_scraper)")
	agentCmd.Flags().StringVar(&agentRiskProfile, "risk-profile", "", "Shell-command risk cascade profile: readonly | cautious | default | permissive | unrestricted. Overrides config.risk_profile for this session. Persona-defined rules still win.")
	agentCmd.Flags().StringVar(&agentEAMode, "ea-mode", "", "Executive Assistant startup mode: 'interactive' (default) or 'queue' (autonomous task processing)")
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "Run tools in simulation mode (enhanced safety)")
	agentCmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Maximum iterations per prompt before stopping (default: 0 = unlimited)")
	agentCmd.Flags().BoolVar(&agentNoStreaming, "no-stream", false, "Disable streaming mode (useful for scripts and pipelines) (or set SPROUT_NO_STREAM=1)")
	agentCmd.Flags().BoolVar(&agentShowReasoningTerminal, "show-reasoning-terminal", false, "Render reasoning stream chunks in terminal output (default: hidden; WebUI still receives reasoning)")
	agentCmd.Flags().StringVar(&agentSystemPromptFile, "system-prompt", "", "File path containing custom system prompt")
	agentCmd.Flags().StringVar(&agentSystemPrompt, "system-prompt-str", "", "Direct system prompt string")
	agentCmd.Flags().BoolVar(&agentUnsafe, "unsafe", false, "UNSAFE MODE: Bypass most security checks (still blocks critical system operations)")
	agentCmd.Flags().BoolVar(&agentNoSubagents, "no-subagents", false, "Disable subagent tools (run_subagent, run_parallel_subagents)")
	agentCmd.Flags().StringVar(&agentSubagentModel, "subagent-model", "", "Model for subagent tools (persists to config; set per-session)")
	agentCmd.Flags().StringVar(&agentSubagentProvider, "subagent-provider", "", "Provider for subagent tools (persists to config; set per-session)")
	agentCmd.Flags().StringVar(&agentResourceDirectory, "resource-directory", "", "Optional directory (relative to current working directory) to store captured web/vision resources")
	agentCmd.Flags().StringVar(&agentWorkflowConfig, "workflow-config", "", "JSON file that defines agent workflow steps for non-interactive runs")
	agentCmd.Flags().StringVar(&agentTraceDatasetDir, "trace-dataset-dir", "", "Enable dataset trace mode and write to directory (also settable via SPROUT_TRACE_DATASET_DIR env var)")
	agentCmd.Flags().BoolVar(&agentPromptStdin, "prompt-stdin", false, "Read the prompt from stdin (avoids OS ARG_MAX limits for large prompts)")
	_ = agentCmd.RegisterFlagCompletionFunc("persona", completePersonaFlag)

	// Initialize environment-based defaults
	cobra.OnInitialize(func() {
			// Check for SPROUT_NO_STREAM environment variable
	if configuration.GetEnvSimple("NO_STREAM") == "1" || configuration.GetEnvSimple("NO_STREAM") == "true" {
		agentNoStreaming = true
	}
	// Check for SPROUT_SHOW_REASONING_TERMINAL environment variable
	if configuration.GetEnvSimple("SHOW_REASONING_TERMINAL") == "1" || strings.EqualFold(configuration.GetEnvSimple("SHOW_REASONING_TERMINAL"), "true") {
		agentShowReasoningTerminal = true
	}
	// Check for SPROUT_NO_SUBAGENTS environment variable
	if configuration.GetEnvSimple("NO_SUBAGENTS") == "1" || configuration.GetEnvSimple("NO_SUBAGENTS") == "true" {
		agentNoSubagents = true
	}
	// Check for SPROUT_NO_CONNECTION_CHECK environment variable
	if configuration.GetEnvSimple("NO_CONNECTION_CHECK") == "1" || configuration.GetEnvSimple("NO_CONNECTION_CHECK") == "true" {
		agentNoConnectionCheck = true
	}})
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
		// Exclude orchestrator from subagent options (it's the primary chat persona)
		if id == "orchestrator" {
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
	Short: "Agent for code analysis and editing (default when running 'sprout' alone)",
	Long: `Agent mode for intelligent code analysis and editing with modern CLI + Web UI.

Features:
• Clean CLI output with automatic web UI startup
• Real-time event streaming to web interface
• Error recovery and malformed tool call detection
• Context management and optimization
• Intelligent fallback and retry mechanisms

The agent runs in two modes:

1. **Interactive Mode**:
   - Clean CLI with real-time streaming
   - Automatic web UI startup on localhost:56000
   - Modern web interface for rich interaction
   - Event-driven communication between CLI and web UI

2. **Direct Mode**:
   - Clean CI-style output for automation
   - Optional web UI for monitoring progress
   - Tool execution with atomic operations
   - Context management and optimization

Examples:
  # Interactive mode (automatic when no arguments provided)
  sprout agent

  # Direct mode
  sprout agent "Add better error handling to the main function"
  sprout agent "How does the authentication system work?"

  # With specific provider and model
  sprout agent --provider openrouter --model "qwen/qwen3-coder-30b" "Fix the login bug"
  sprout agent -p deepinfra -m "deepseek-v3" "Analyze the codebase structure"
  sprout agent -p deepseek -m "deepseek-chat" "Write Python code for data analysis"

  # Start with a persona
  sprout agent --persona web-scraper "Collect pricing table data from docs pages"

  # With custom provider (configured via 'sprout custom add')
  sprout agent --provider my-custom-slow --model "custom-model-v1" "Review this code"

  # Skip connection check for faster startup (saves 1-3 seconds)
  sprout agent --no-connection-check "Quick analysis"
  SPROUT_NO_CONNECTION_CHECK=1 sprout agent "Another quick analysis"

  # Set subagent model/provider (persists to config)
  sprout agent --subagent-model "claude-haiku-4-20250514" "Fix the tests"
  sprout agent --subagent-provider deepinfra --subagent-model "deepseek-v3" "Refactor auth"

  # Non-interactive run with an agent workflow
  sprout agent --workflow-config examples/agent_workflow.json

  # Resume a previous session in this directory scope
  sprout agent --session-id session_1234567890

  # Resume the most recent session from this directory
  sprout agent --last-session

  # Disable web UI
  sprout agent --no-web-ui "Analyze this code"`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Propagate --no-project-skills to env so config loading skips discovery
		if noProjectSkills {
			os.Setenv("SPROUT_NO_PROJECT_SKILLS", "1")
		}

		// Propagate daemon mode before agent creation so that
		// isSSHDaemon() returns true during provider resolution.
		// Without this, NewAgent() fails in non-interactive mode
		// before RunAgent has a chance to set the env var.
		if daemonMode {
			os.Setenv("SPROUT_DAEMON", "1")
		}

		chatAgent, err := createChatAgent()
		if err != nil {
			return fmt.Errorf("failed to create chat agent: %w", err)
		}

		// In daemon mode, the agent may be nil if the provider isn't configured.
		// The web UI will handle provider setup. Skip all agent-specific setup.
		if chatAgent == nil && daemonMode {
			isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
			stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))
			isInteractive := len(args) == 0 && !isCI && stdinIsTerminal
			return RunAgent(nil, isInteractive, args)
		}

		// Initialize trace session if requested
		traceDir := getTraceDatasetDir(agentTraceDatasetDir)
		if traceDir != "" {
			provider := chatAgent.GetProvider()
			model := chatAgent.GetModel()
			traceSession, err := trace.NewTraceSession(traceDir, provider, model)
			if err != nil {
				return fmt.Errorf("failed to initialize trace session: %w", err)
			}
			chatAgent.SetTraceSession(traceSession)
			_, _ = os.Stdout.Write([]byte(fmt.Sprintf("Dataset tracing enabled: %s\n", traceSession.GetRunID())))
		}

		// Set unsafe mode if flag is provided
		chatAgent.SetUnsafeMode(agentUnsafe)

		// Apply --risk-profile flag (SP-058). Accepts either a
		// built-in profile name OR a user-defined name from
		// config.risk_profiles. Empty string preserves the config
		// setting; unrecognized names (no built-in AND no override)
		// are warned about but still set — the resolver will fall
		// back to the Default profile when it can't find rules.
		if agentRiskProfile != "" {
			cfg := chatAgent.GetConfig()
			_, hasUserOverride := func() (configuration.AutoApproveRules, bool) {
				if cfg == nil || cfg.RiskProfiles == nil {
					return configuration.AutoApproveRules{}, false
				}
				v, ok := cfg.RiskProfiles[agentRiskProfile]
				return v, ok
			}()
			if configuration.IsValidRiskProfile(agentRiskProfile) || hasUserOverride {
				chatAgent.SetRiskProfileOverride(configuration.RiskProfile(agentRiskProfile))
			} else {
				fmt.Fprintf(os.Stderr, "Warning: unknown --risk-profile %q. Built-in: readonly, cautious, default, permissive, unrestricted. Define custom profiles in config.risk_profiles. Falling back to default for this session.\n", agentRiskProfile)
			}
		}

		// Disable subagents if flag is set
		if agentNoSubagents {
			_ = configuration.SetEnv("NO_SUBAGENTS", "1")
		}

		// Persist subagent model/provider CLI flags to config
		if agentSubagentModel != "" || agentSubagentProvider != "" {
			cm := chatAgent.GetConfigManager()
			if cm != nil {
				if err := cm.UpdateConfig(func(c *configuration.Config) error {
					if agentSubagentModel != "" {
						c.SetSubagentModel(agentSubagentModel)
					}
					if agentSubagentProvider != "" {
						c.SetSubagentProvider(agentSubagentProvider)
					}
					return nil
				}); err != nil {
					return fmt.Errorf("failed to save subagent config: %w", err)
				}
			} else {
				_, _ = os.Stderr.Write([]byte("Warning: could not persist subagent config: config manager unavailable\n"))
			}
		}

		if agentDryRun {
			_ = configuration.SetEnv("DRY_RUN", "1")
		}
		if agentNoConnectionCheck {
			_ = configuration.SetEnv("SKIP_CONNECTION_CHECK", "1")
		}
		if strings.TrimSpace(agentResourceDirectory) != "" {
			_ = configuration.SetEnv("RESOURCE_DIRECTORY", strings.TrimSpace(agentResourceDirectory))
		}
		if agentLastSession && strings.TrimSpace(agentSessionID) != "" {
			return errors.New("flag --session-id and --last-session are mutually exclusive")
		}
		if agentLastSession || strings.TrimSpace(agentSessionID) != "" {
			workingDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to resolve current working directory for session restore: %w", err)
			}
			targetSessionID := strings.TrimSpace(agentSessionID)
			if agentLastSession {
				sessions, err := agent.ListSessionsWithTimestamps()
				if err != nil {
					return fmt.Errorf("failed to list sessions: %w", err)
				}
				for _, session := range sessions {
					if strings.TrimSpace(session.WorkingDirectory) == workingDir {
						targetSessionID = strings.TrimSpace(session.SessionID)
						break
					}
				}
				if targetSessionID == "" {
					return fmt.Errorf("no prior session found for current directory: %s", workingDir)
				}
			}
			state, err := chatAgent.LoadStateScoped(targetSessionID, workingDir)
			if err != nil {
				return fmt.Errorf("failed to load session %q: %w", targetSessionID, err)
			}
			chatAgent.ApplyState(state)
			chatAgent.SetSessionID(state.SessionID)
		}

		// Check if we're in a CI environment or non-interactive mode
		isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""

		// Check if stdin is a terminal (not piped)
		stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))

		// When --prompt-stdin is set, read the full prompt from stdin to avoid
		// OS ARG_MAX limits when passing large prompts as CLI arguments.
		if agentPromptStdin {
			promptData, readErr := io.ReadAll(os.Stdin)
			if readErr != nil {
				return fmt.Errorf("failed to read prompt from stdin: %w", readErr)
			}
			promptText := strings.TrimSpace(string(promptData))
			if promptText == "" {
				return errors.New("--prompt-stdin specified but stdin was empty")
			}
			args = []string{promptText}
			stdinIsTerminal = false
		}

		// We're interactive only if we have a terminal, no args, and not in CI
		isInteractive := len(args) == 0 && !isCI && stdinIsTerminal

		// Use the new simplified enhanced mode
		return RunAgent(chatAgent, isInteractive, args)
	},
}
