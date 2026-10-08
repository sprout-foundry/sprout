//go:build !js

// Agent command for sprout
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
	"github.com/sprout-foundry/sprout/pkg/personas"
	"github.com/sprout-foundry/sprout/pkg/security"
)

var (
	agentSkipPrompt            bool
	agentModel                 string
	agentProvider              string
	agentSessionID             string
	agentLastSession           bool
	agentPersona               string
	agentDryRun                bool
	maxIterations              int
	agentNoStreaming           bool
	agentShowReasoningTerminal bool
	agentReasoningMode         string // "hidden" (default), "fold", "full"
	agentSystemPromptFile      string
	agentSystemPrompt          string
	agentUnsafe                bool
	agentUnsafeShell           bool
	agentNoSubagents           bool
	agentSubagentModel         string
	agentSubagentProvider      string
	agentResourceDirectory     string
	agentWorkflowConfig        string
	// Path of the detached-run session record to finalize on exit. Only
	// `automate run --detach` sets it (via --automate-session-file); an
	// empty value means this process owns no session record.
	agentAutomateSessionFile string
	// Path of the session record to annotate with the continuation loop's
	// stop reason. Set by `automate run` in every mode (via
	// --automate-record-file) so attached runs — whose end state is
	// finalized by the launcher — still carry why the run stopped. The
	// child only writes the stop-reason fields; it never finalizes.
	agentAutomateRecordFile string
	agentNoConnectionCheck  bool
	agentTraceDatasetDir    string
	agentPromptStdin        bool
	agentRiskProfile        string
	// Workflow budget overrides — populated from CLI flags on `sprout
	// automate` and applied on top of the workflow JSON's budget block.
	// Only positive values apply; pass 0 (or omit) to inherit the workflow
	// JSON. To explicitly disable budget/heartbeat, edit the JSON.
	agentBudgetUSD        float64
	agentBudgetWarn       string
	agentHeartbeatSeconds int
	agentMockLLM          bool
	// agentNoDaemon forces this invocation to run in-process, never handing
	// the turn to a running daemon even when one is reachable and compatible.
	agentNoDaemon bool
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
		// CLI-G-2: route pre-decision security warnings through the
		// console.GlyphWarning path so they hit the terminal stderr
		// instead of ~/.sprout/workspace.log.
		console.GlyphWarning.Fprintln(os.Stderr, "Symlink warnings:")
		for _, warn := range symlinkWarnings {
			fmt.Fprintf(os.Stderr, "  %s\n", warn)
		}
	}

	// Run the full permission check
	security.RunStartupCheck(configDir)

	return nil
}

// resolveGlobalConfigDir returns the global config directory for layering a
// workspace config on top. SPROUT_CONFIG is not consulted: isolated-config
// mode repoints it at the workspace's own .sprout directory. An explicit
// SPROUT_CONFIG_DIR still wins, and otherwise $XDG_CONFIG_HOME/sprout or
// ~/.config/sprout — the same place the isolated config was seeded from.
func resolveGlobalConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("SPROUT_CONFIG_DIR")); dir != "" {
		return dir
	}
	dir, err := configuration.DefaultConfigDir()
	if err != nil {
		return ""
	}
	return dir
}

// shouldPreloadLocalModel reports whether createChatAgent should eagerly
// load the local model before the agent is ready to serve. Three cases skip
// the (expensive, GPU-loading) preload:
//
//  1. Not using the local provider at all — nothing to preload.
//  2. A silently auto-started background daemon (SPROUT_DAEMON_AUTOSTARTED=1,
//     set by daemon_autostart.go on the child's env): nothing routes real
//     agent traffic through that daemon yet, so eagerly loading here only
//     duplicates the foreground process's GPU/model work and contends with
//     it for the same GPU — which made the daemon's own health check
//     reliably miss its 10s StartTimeout, leaving it running unsupervised.
//  3. A daemon is already up, healthy, and about to actually serve this
//     query: tryDaemonOneShot (called later in the same invocation, once
//     flags/workflow are resolved) will route there, so preloading our own
//     copy first would be paid for nothing. Guarded to only skip when we're
//     confident tryDaemonOneShot will actually run and would route
//     successfully — see the inline checks below, which mirror
//     tryDaemonOneShot's own gating exactly so this can't skip a preload
//     that then has nothing to fall back on.
//
// In every skip case the model still loads normally on first actual local
// use (lazy init in LocalProvider.ensureLoaded) if daemon routing doesn't
// end up happening after all. An explicit `sprout agent -d` /
// `sprout service start` (daemonMode true, no SPROUT_DAEMON_AUTOSTARTED
// marker) always preloads — it doesn't route to another daemon at all
// (case 3 doesn't apply to it), and it's intentionally going to be used.
func shouldPreloadLocalModel() bool {
	if !isLocalProvider() {
		return false
	}
	if os.Getenv("SPROUT_DAEMON_AUTOSTARTED") == "1" {
		return false
	}
	if daemonMode {
		return true // we ARE the daemon; nothing else to defer to
	}
	// Mirror tryDaemonOneShot's own preconditions (cmd/agent_socket.go)
	// exactly: if any of these say it won't run or won't route, don't skip
	// the preload — we'd be relying on a fallback that isn't coming.
	if agentSkipDaemonRouting() {
		return true
	}
	return !isDaemonReachableForAgentRouting()
}

// agentSkipDaemonRouting reports whether this invocation will never attempt
// tryDaemonOneShot at all, independent of daemon health — i.e. shouldn't be
// used as a basis for skipping the local-model preload.
//
// tryDaemonOneShot's actual gate (agent_modes.go) is `workflowConfig ==
// nil`, where workflowConfig is loaded from the --workflow-config file at
// RunAgent time — after createChatAgent has already returned, so the loaded
// value itself isn't available yet here. agentWorkflowConfig (the raw flag)
// is populated by cobra before RunE runs, same timing createChatAgent
// already relies on for agentProvider/agentModel/etc, so "is the flag set"
// is used as a conservative stand-in: if it's set but loading later fails,
// the command errors out before ever reaching tryDaemonOneShot anyway, so
// treating "flag set" as "won't route" never skips a preload that had
// something to fall back on.
//
// These flags can't travel the wire protocol today; routing would silently
// drop them, so run in-process where they're honored. agentAutomateSessionFile
// is in the same class: the detached automate child owns finalizing its own
// session record, which a daemon-routed query would never do.
func agentSkipDaemonRouting() bool {
	return agentNoDaemon ||
		agentWorkflowConfig != "" ||
		agentAutomateSessionFile != "" ||
		agentSessionID != "" ||
		agentLastSession ||
		agentSystemPrompt != "" ||
		agentSystemPromptFile != "" ||
		agentDryRun ||
		agentUnsafe ||
		agentUnsafeShell ||
		agentNoSubagents ||
		agentSubagentModel != "" ||
		agentSubagentProvider != "" ||
		agentResourceDirectory != "" ||
		agentBudgetUSD != 0 ||
		agentTraceDatasetDir != "" ||
		agentMockLLM ||
		noProjectSkills
}

func createChatAgent() (*agent.Agent, error) {
	// Proactive CLI onboarding: if no provider is configured and we're in
	// an interactive terminal, guide the user through setup before trying
	// to create an agent. Onboarding persists the provider+model to config
	// so the subsequent NewAgent() call picks up the fresh configuration.
	maybeRunOnboarding()

	// Setup was skipped or didn't finish: start in editor-only mode — the
	// web UI without an agent — rather than failing on the missing provider.
	if onboardingDeclined && !daemonMode {
		daemonMode = true
		fmt.Println()
		console.GlyphInfo.Printf("Starting in editor-only mode: browse and edit your files in the web UI.")
		fmt.Println("Add an AI provider any time in the web UI's settings, or run 'sprout keys set <provider>' and start sprout again.")
		return nil, nil
	}

	// If using the local provider, pre-load the model in-process — with
	// the user's actual persisted/flag-selected model, not the RAM-tier
	// default. This preload runs before the real agent (and its own
	// config-driven model resolution below) exists, so without resolving
	// the intended model here too, it would always auto-select — loading
	// the wrong model whenever that differs from the persisted choice,
	// and paying for a second full reload moments later when the real
	// agent corrects it. A throwaway config read here is a few
	// milliseconds; the reload it avoids is 8+ seconds of GPU work.
	if shouldPreloadLocalModel() {
		preloadModel := ""
		if cfgManager, cfgErr := configuration.NewManagerSilent(); cfgErr == nil {
			if _, resolvedModel, resolveErr := cfgManager.ResolveProviderModel(agentProvider, agentModel); resolveErr == nil {
				preloadModel = resolvedModel
			}
		}
		// This is the one place local-model loading is otherwise
		// completely silent: it runs before the REPL/spinner
		// infrastructure exists, so without an explicit message the
		// terminal just sits frozen for the load's 8+ seconds with no
		// indication anything is happening. Every other load path (a
		// mid-session /model switch) at least runs under the "Thinking"
		// spinner already.
		if preloadModel != "" {
			console.GlyphInfo.Printf("Loading local model (%s)...", preloadModel)
		} else {
			console.GlyphInfo.Print("Loading local model...")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		if err := localmodel.EnsureServerForProviderWithCheckAndModel(ctx, "sprout-local", preloadModel); err != nil {
			console.GlyphWarning.Printf("Local AI: %v", err)
		}
		cancel()
	}

	var chatAgent *agent.Agent
	var err error

	// Use layered config when workspace config was auto-detected, so the agent
	// inherits API keys from global config while using workspace overrides.
	if autoDetectedWorkspaceDir != "" {
		globalDir := resolveGlobalConfigDir()
		if globalDir != "" {
			chatAgent, err = agent.NewAgentWithLayers(globalDir, autoDetectedWorkspaceDir, providerModelSpec(agentProvider, agentModel))
		}
	}
	if chatAgent == nil {
		if spec := providerModelSpec(agentProvider, agentModel); spec != "" {
			chatAgent, err = agent.NewAgentWithModel(spec)
		} else {
			chatAgent, err = agent.NewAgent()
		}
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
			// No usable provider and stdin isn't a terminal. Don't print the raw
			// wrapped error here — Execute()'s central renderer shows the cause
			// plus a concise provider-setup block (renderProviderSetupHint).
			return nil, err
		}
		return nil, fmt.Errorf("failed to initialize agent: %w", err)
	}

	// Run startup permission check
	if err := runStartupPermissionCheck(); err != nil {
		// CLI-G-2: same channel as the symlink warning above — these
		// are pre-decision signals and should hit the terminal.
		console.GlyphWarning.Fprintf(os.Stderr, "%v", err)
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

	if maxIterations > 0 {
		chatAgent.SetMaxIterations(maxIterations)
	}

	// Wire training data collection hooks (opt-in session recording).
	wireTrainingHooks(chatAgent, chatAgent.GetConfigManager())

	return chatAgent, nil
}

func init() {
	agentCmd.Flags().BoolVarP(&agentSkipPrompt, "yes", "y", false, "Run without interactive prompts: auto-approve confirmations and skip pickers")
	boolFlagAlias(agentCmd.Flags(), &agentSkipPrompt, "skip-prompt", "yes", aliasSilent)
	agentCmd.Flags().BoolVar(&agentNoConnectionCheck, "no-connection-check", false, "Skip provider connection check at startup (saves 1-3 seconds)")
	agentCmd.Flags().StringVarP(&agentModel, "model", "m", "", "Model name for agent system")
	agentCmd.Flags().StringVarP(&agentProvider, "provider", "p", "", providerFlagUsage)
	agentCmd.Flags().StringVar(&agentSessionID, "session-id", "", "Resume a specific session ID in the current working directory scope")
	agentCmd.Flags().BoolVar(&agentLastSession, "last-session", false, "Resume the most recent session from the current working directory scope")
	agentCmd.Flags().StringVar(&agentPersona, "persona", "", "Persona to activate at startup (e.g., general, coder, refactor, debugger, tester, reviewer, researcher, web_scraper)")
	agentCmd.Flags().StringVar(&agentRiskProfile, "risk-profile", "", "Shell-command risk cascade profile: readonly | cautious | default | permissive | unrestricted. Overrides config.risk_profile for this session. Persona-defined rules still win.")
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "Run tools in simulation mode (enhanced safety)")
	agentCmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Maximum iterations per prompt before stopping (default: 0 = unlimited)")
	agentCmd.Flags().BoolVar(&agentNoStreaming, "no-stream", false, "Disable streaming mode (useful for scripts and pipelines) (or set SPROUT_NO_STREAM=1)")
	agentCmd.Flags().BoolVar(&agentShowReasoningTerminal, "show-reasoning-terminal", false, "Render reasoning stream chunks in terminal output (default: hidden; WebUI still receives reasoning)")
	agentCmd.Flags().StringVar(&agentReasoningMode, "reasoning", "", "Reasoning display mode: 'hidden' (default), 'fold' (collapsed token count), 'full' (stream raw text)")
	agentCmd.Flags().StringVar(&agentSystemPromptFile, "system-prompt", "", "File path containing custom system prompt")
	agentCmd.Flags().StringVar(&agentSystemPrompt, "system-prompt-str", "", "Direct system prompt string")
	agentCmd.Flags().BoolVar(&agentUnsafe, "unsafe", false, "UNSAFE MODE: Bypass most security checks (still blocks critical system operations)")
	agentCmd.Flags().BoolVar(&agentUnsafeShell, "unsafe-shell", false, "UNSAFE SHELL MODE: Bypass CAUTION-tier shell prompts only (DANGEROUS operations still block; file security still applies)")
	agentCmd.Flags().BoolVar(&agentNoSubagents, "no-subagents", false, "Disable subagent tools (run_subagent, run_parallel_subagents)")
	agentCmd.Flags().StringVar(&agentSubagentModel, "subagent-model", "", "Model for subagent tools (saved to config)")
	agentCmd.Flags().StringVar(&agentSubagentProvider, "subagent-provider", "", "Provider for subagent tools (saved to config)")
	agentCmd.Flags().StringVar(&agentResourceDirectory, "resource-directory", "", "Optional directory (relative to current working directory) to store captured web/vision resources")
	agentCmd.Flags().StringVar(&agentWorkflowConfig, "workflow-config", "", "JSON file that defines agent workflow steps for non-interactive runs")
	agentCmd.Flags().StringVar(&agentAutomateSessionFile, "automate-session-file", "", "Session record JSON path to finalize when this run exits (set by 'automate run --detach'; empty = no finalization)")
	_ = agentCmd.Flags().MarkHidden("automate-session-file")
	agentCmd.Flags().StringVar(&agentAutomateRecordFile, "automate-record-file", "", "Session record JSON path to annotate with the continuation stop reason (set by 'automate run'; empty = none)")
	_ = agentCmd.Flags().MarkHidden("automate-record-file")
	agentCmd.Flags().Float64Var(&agentBudgetUSD, "budget-usd", 0, "Hard cap on workflow USD spend (overrides workflow JSON budget.usd; 0 = no cap)")
	agentCmd.Flags().StringVar(&agentBudgetWarn, "budget-warn", "", "Comma-separated warning thresholds as fractions of the budget, e.g. '0.5,0.8'")
	agentCmd.Flags().IntVar(&agentHeartbeatSeconds, "heartbeat", 0, "Print [budget] progress every N seconds during the run (overrides progress.heartbeat_seconds)")
	agentCmd.Flags().StringVar(&agentTraceDatasetDir, "trace-dataset-dir", "", "Enable dataset trace mode and write to directory (also settable via SPROUT_TRACE_DATASET_DIR env var)")
	agentCmd.Flags().BoolVar(&agentPromptStdin, "prompt-stdin", false, "Read the prompt from stdin (avoids OS ARG_MAX limits for large prompts)")
	agentCmd.Flags().BoolVar(&agentMockLLM, "mock-llm", false, "Use a stub LLM provider that returns canned responses (for testing)")
	agentCmd.Flags().BoolVar(&agentNoDaemon, "no-daemon", false, "Run this turn in-process instead of handing it to a running daemon (also set by SPROUT_DAEMON_AGENT=0)")
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
		// Exclude orchestrator from subagent options (it's the primary chat persona)
		if id == personas.IDOrchestrator {
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
	Long: `Run the coding agent. With no intent it starts an interactive session
(terminal prompt plus the web UI on localhost:56000); with an intent it runs
that one task and exits.

Examples:
  sprout agent                                   # interactive session
  sprout agent "How does the auth flow work?"    # one task, then exit
  sprout agent -p openrouter -m qwen/qwen3-coder-30b "Fix the login bug"
  sprout agent --persona web_scraper "Collect the pricing table from the docs"
  sprout agent --last-session                    # resume the latest session here
  sprout agent --session-id <id>                 # resume a specific session
  sprout agent --no-web-ui --json -o result.json "Summarize open TODOs"
  sprout agent --workflow-config workflow.json   # scripted multi-step run

Run 'sprout agent --help-all' for advanced flags (budgets, risk profile,
subagent model, custom system prompts).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentCommand,
}
