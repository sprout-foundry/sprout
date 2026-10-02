//go:build !js

package cmd

// agent_command_run.go — the RunE body of the agent command, extracted
// from the agentCmd literal in agent_command.go so the command literal
// stays readable. runAgentCommand does the agent-command wiring: startup
// permission check, local-model preload decision, chat-agent creation,
// and mode dispatch. Split out of agent_command.go.
import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/trace"
)

func runAgentCommand(cmd *cobra.Command, args []string) (err error) {
	// Detached automate runs self-finalize: the launcher exited right
	// after spawning us, so nobody else will ever record how this run
	// ended. Registered before agent creation on purpose — a child that
	// dies during provider bootstrap is still a failing run and must
	// record exit 1, not stay "running" forever. Force-quit os.Exit
	// paths (second Ctrl+C, 5s shutdown timeout) skip defers entirely;
	// those records fall back to PID-liveness "exited".
	if agentAutomateSessionFile != "" {
		defer func() {
			finalizeAutomateSession(agentAutomateSessionFile, err)
		}()
	}

	// `sprout agent --help-all` (without -h) lists every flag, then exits.
	if maybeRenderAgentHelpAll(cmd) {
		return nil
	}

	// SP-056-3: validate --reasoning flag value.
	// Empty string means default (hidden); allowed explicit values:
	// "hidden", "fold", "full".
	switch agentReasoningMode {
	case "", "hidden", "fold", "full":
		// valid
	default:
		return usageErrorf(cmd, "invalid --reasoning value %q: must be 'hidden', 'fold', or 'full'", agentReasoningMode)
	}

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
		// SP-137 Phase 3: an auto-started daemon raises its oom_score_adj
		// so the kernel sacrifices the background helper before any
		// user-facing process. Explicit starts keep the default.
		maybePreferOOMVictim(daemonMode)
		// Defensive unset on command exit. RunAgent also defers its own
		// unset, but this handler may return early (provider errors,
		// session-load failures) before RunAgent runs.
		defer os.Unsetenv("SPROUT_DAEMON")
	}

	// SP-136 P2: lazily ensure a background daemon is running (async,
	// best-effort). Skipped when SPROUT_DAEMON=0/1 or in daemon mode.
	// The CLI proceeds in-process either way; later phases route work
	// through the daemon.
	stopDaemonKeepAlive := maybeAutoStartDaemon(cmd.Context(), daemonMode)
	defer stopDaemonKeepAlive()

	// Propagate --mock-llm flag to the agent package before agent creation.
	agent.UseMockLLM = agentMockLLM

	chatAgent, err := createChatAgent()
	if err != nil {
		return fmt.Errorf("failed to create chat agent: %w", err)
	}

	// In daemon mode, the agent may be nil if the provider isn't configured.
	// The web UI will handle provider setup. Skip all agent-specific setup.
	if chatAgent == nil && daemonMode {
		isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
		stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))
		isInteractive := !daemonMode && len(args) == 0 && !isCI && stdinIsTerminal
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

	// --unsafe implies --unsafe-shell, but --unsafe-shell can be set independently
	chatAgent.SetUnsafeShellMode(agentUnsafeShell || agentUnsafe)

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
			console.GlyphWarning.Printf("Unknown --risk-profile %q. Built-in: readonly, cautious, default, permissive, unrestricted. Define custom profiles in config.risk_profiles. Falling back to default for this session.", agentRiskProfile)
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
			console.GlyphWarning.Print("Could not persist subagent config: config manager unavailable")
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
		workingDir := chatAgent.GetWorkspaceRoot()
		if workingDir == "" {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to resolve current working directory for session restore: %w", err)
			}
			workingDir = wd
		}
		targetSessionID := strings.TrimSpace(agentSessionID)
		if agentLastSession {
			sessions, err := agent.ListSessionsWithTimestampsScoped(workingDir)
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

	// We're interactive only if we have a terminal, no args, not in CI,
	// and not running as a daemon (daemon serves the web UI only).
	isInteractive := !daemonMode && len(args) == 0 && !isCI && stdinIsTerminal

	// Use the new simplified enhanced mode
	return RunAgent(chatAgent, isInteractive, args)
}
