package agent

// agent_creation_provider.go — the provider-init path of agent creation:
// initAgentFromResolvedProvider (provider resolution, client construction,
// and agent wiring), maybeAutoActivateCoordinatorPersona (auto-activating
// the coordinator persona in interactive TTY sessions), and
// isInteractiveTerminal. Split out of agent_creation.go.
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// initAgentFromResolvedProvider creates and initializes an Agent from resolved provider parameters.
func initAgentFromResolvedProvider(params agentInitParams) (*Agent, error) {
	// Create sub-managers
	stateMgr := NewAgentStateManager(params.debug)
	outputMgr := NewAgentOutputManager()
	securityMgr := NewAgentSecurityManager()
	mcpMgr := NewAgentMCPManager()

	// Construct the agent struct
	agent := &Agent{
		client:              params.client,
		systemPrompt:        params.systemPrompt,
		baseSystemPrompt:    params.systemPrompt,
		maxIterations:       0,
		clientType:          params.clientType,
		debug:               params.debug,
		configManager:       params.configManager,
		shellCommandHistory: make(map[string]*ShellCommandResult),
		inputInjectionChan:  make(chan string, inputInjectionBufferSize),
		interruptCtx:        params.interruptCtx,
		interruptCancel:     params.interruptCancel,
		workspaceRoot:       params.workspaceRoot,
		state:               stateMgr,
		output:              outputMgr,
		security:            securityMgr,
		mcpSub:              mcpMgr,
		todoMgr:             tools.NewTodoManager(),
		subagentDepth:       params.subagentDepth,
		rootPersonaID:       params.rootPersonaID,
		shellCwd:            &shellCwdTracker{},
	}

	// Set up output router
	router := NewOutputRouter(agent, nil)
	agent.output.SetOutputRouter(router)

	// Configure the optimizer with the LLM client
	agent.state.GetOptimizer().SetLLMClient(agent.client, agent.GetProvider(), func(line string) {
		agent.PrintLineAsync(line)
	})

	// Initialize debug log file if debug enabled
	if agent.debug {
		if err := agent.initDebugLogger(); err != nil {
			// Non-fatal: fall back to stdout debug
			_, _ = os.Stderr.Write([]byte(fmt.Sprintf("WARNING: Failed to initialize debug logger: %v\n", err)))
		}
	}

	// Production-only initialization steps
	if params.isProduction {
		// The system prompts advertise /tmp/sprout as the scratch directory;
		// create it up front so the model's first shell write doesn't fail
		// with ENOENT. Sandbox-isolated /tmp fails silently by design.
		filesystem.EnsureScratchDir()

		// Initialize context limits based on model
		agent.state.SetMaxContextTokens(agent.getModelContextLimit())
		agent.state.SetCurrentContextTokens(0)
		agent.state.SetContextWarningIssued(false)

		// Resolve the context profile once at agent creation. Auto-detects LCM from model context window.
		var cfg *configuration.Config
		if agent.configManager != nil {
			cfg = agent.configManager.GetConfig()
		}
		profile, err := configuration.ResolveContextProfile(
			cfg,
			agent.state.GetMaxContextTokens(),
		)
		if err != nil {
			return nil, err
		}
		agent.contextProfile = profile

		// Resolve the effective context cap once at agent creation. Caps below EffectiveContextCapMinimum (1024) return an error.
		nativeWindow := agent.getNativeModelContextLimit()
		resolvedCap, capErr := configuration.ResolveEffectiveContextCap(cfg, nativeWindow)
		if capErr != nil {
			return nil, agenterrors.NewConfig("resolving effective context cap", capErr)
		}
		agent.setContextCapState(resolvedCap, nativeWindow, cfg)

		// Activation notice: emit a one-time stderr line when the user set a cap below the native window.
		effectiveCap := agent.effectiveCapSnapshot()
		if cfg != nil && cfg.MaxContextTokens != nil && *cfg.MaxContextTokens > 0 &&
			effectiveCap > 0 &&
			effectiveCap < nativeWindow {
			_, _ = fmt.Fprintf(os.Stderr,
				"⚡ Context cap active: %s (native: %s)\n"+
					"  All requests will use at most %s of context.\n"+
					"  /max-context clear to remove, /max-context <N> to change.\n",
				agent.formatTokenCount(effectiveCap),
				agent.formatTokenCount(nativeWindow),
				agent.formatTokenCount(effectiveCap),
			)
		}

		if profile.Mode == configuration.ContextModeLowContext {
			// Show a one-time notice only when LCM was auto-detected, not when explicitly configured.
			explicit := cfg != nil && cfg.ContextMode == configuration.ContextModeLowContext
			if !explicit {
				_, _ = fmt.Fprintf(os.Stderr,
					"⚠ %dK context detected — Low-Context Mode active\n"+
						"  %d tools, lite prompt, AGENTS.md kept\n"+
						"  Set context_mode: \"full\" in config to override, or /model to switch.\n",
					agent.state.GetMaxContextTokens()/1000, len(profile.ToolAllowlist))
			} else if params.debug {
				_, _ = fmt.Fprintf(os.Stderr,
					"[low-context] explicit config: tools=%d prompt=%s trigger=%.2f\n",
					len(profile.ToolAllowlist), profile.SystemPromptPath,
					profile.CompactionTriggerFraction)
			}
		}

		// Clean up old sessions once per process.
		sessionCleanupOnce.Do(func() {
			if err := cleanupMemorySessions(); err != nil && agent.debug {
				_, _ = os.Stderr.Write([]byte(fmt.Sprintf("WARNING: Failed to clean up old sessions: %v\n", err)))
			}
		})

		// Clean up orphaned background processes from previous unclean exits.
		backgroundOrphanCleanupOnce.Do(func() {
			cleanupOrphanedBackgroundProcesses(agent.debug)
		})

		// Auto-register a CLI password prompter when stdin is a TTY.
		if isInteractiveTerminal() {
			agent.passwordPrompter = NewCLIPasswordPrompter()
			if agent.debug {
				agent.Logger().Info("Registered CLI password prompter (TTY detected)")
			}
		}

		// Sweep expired persistent context entries based on retention policy.
		if agent.configManager != nil {
			cfg := agent.configManager.GetConfig()
			if cfg != nil && cfg.PersistentContext != nil && cfg.PersistentContext.RetentionDays > 0 {
				// Resolve storePath using the same logic as EmbeddingManager.initLocked().
				convoStoreDir := ""
				if cfg.EmbeddingIndex != nil {
					convoStoreDir = cfg.EmbeddingIndex.IndexDir
				}
				if convoStoreDir == "" {
					dataDir, err := envutil.DataDir()
					if err == nil {
						convoStoreDir = filepath.Join(dataDir, "embeddings")
					} else {
						home, _ := os.UserHomeDir()
						convoStoreDir = filepath.Join(home, ".local", "share", "sprout", "embeddings")
					}
				}
				convoStorePath := filepath.Join(convoStoreDir, "conversation_turns.hnsw")
				swept, sweepErr := SweepExpiredEntries(cfg.PersistentContext.RetentionDays, convoStorePath)
				if sweepErr != nil && agent.debug {
					_, _ = os.Stderr.Write([]byte(fmt.Sprintf("WARNING: Failed to sweep expired context entries: %v\n", sweepErr)))
				} else if swept > 0 && agent.debug {
					_, _ = os.Stderr.Write([]byte(fmt.Sprintf("Swept %d expired context entries\n", swept)))
				}
			}
		}

		// Register computer_use desktop-control tools when enabled in config.
		if agent.configManager != nil {
			if cuErr := RegisterComputerUseTools(agent.configManager.GetConfig()); cuErr != nil && agent.debug {
				agent.Logger().Info("computer_use tools not registered: %v", cuErr)
			}
		}
	}

	// Load command history from configuration
	agent.loadHistoryFromConfig()

	// Set persona from environment if specified
	if persona := strings.TrimSpace(configuration.GetEnvSimple("PERSONA")); persona != "" {
		agent.state.SetActivePersona(strings.ReplaceAll(strings.ToLower(persona), "-", "_"))
	}

	// Initialize change tracker
	agent.changeTracker = NewChangeTracker(agent, "")
	agent.changeTracker.Enable() // Start enabled by default

	// Wire the package-level logger so package-level functions can use structured logging with session context.
	SetPackageLogger(agent.Logger())

	// Restore embedding index if previously enabled for this workspace
	agent.RestoreEmbeddingIndex()

	// Coordinator persona is opt-in ('/persona coordinator', or
	// 'coordinator_auto_activate' in config). It no longer auto-fires
	// for $HOME workspaces: a capable orchestrator handles multi-project
	// triage directly, and the swap surprised more users than it helped.
	if params.isProduction {
		agent.maybeAutoActivateCoordinatorPersona()
	}

	// Wire tool function pointers so handlers in pkg/agent_tools can dispatch back into this agent's handler methods.
	wireAgentToolFuncs(agent, params.isProduction)

	return agent, nil
}

// maybeAutoActivateCoordinatorPersona activates the Coordinator persona when
// the user has explicitly opted in via 'coordinator_auto_activate' in config
// and the workspace is the user's home directory. Coordinator used to
// auto-activate for $HOME workspaces unconditionally; that default was
// removed — the orchestrator handles multi-project triage directly, and the
// silent persona swap surprised more users than it helped.
func (a *Agent) maybeAutoActivateCoordinatorPersona() {
	// Don't override an already-set persona
	if a.state.GetActivePersona() != "" {
		return
	}

	// Opt-in only.
	if cfg := a.GetConfig(); cfg == nil || !cfg.CoordinatorAutoActivate {
		return
	}

	// Only activate when workspace is home directory
	if !isHomeDirPath(a.GetWorkspaceRoot()) {
		return
	}

	// Check if the coordinator persona is available
	personaID := personas.IDCoordinator
	available := a.GetAvailablePersonaIDs()
	found := false
	for _, id := range available {
		if id == personaID {
			found = true
			break
		}
	}
	if !found {
		return
	}

	if err := a.ApplyPersona(personaID); err != nil {
		console.GlyphWarning.Fprintf(os.Stderr, "Failed to auto-activate coordinator persona: %v", err)
		return
	}
	// Surface the activation so users can see why behavior changed.
	console.GlyphInfo.Fprintf(os.Stderr, "Activated coordinator persona because workspace is $HOME (disable with 'disable_coordinator_auto_activate' in config)")
}

// isInteractiveTerminal returns true if stdin is a TTY, indicating the
// agent is running in an interactive CLI session (not piped, daemon, CI).
func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
