package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
	"github.com/sprout-foundry/sprout/pkg/personas"
	"golang.org/x/term"
)

// sessionCleanupOnce ensures session cleanup runs only once per process.
var sessionCleanupOnce sync.Once

// backgroundOrphanCleanupOnce kills background processes left behind by a previous unclean exit.
var backgroundOrphanCleanupOnce sync.Once

func isDebugEnvEnabled() bool {
	value := strings.TrimSpace(configuration.GetEnvSimple("DEBUG"))
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// agentInitParams encapsulates the parameters needed to initialize an Agent after the provider and client have been resolved.
type agentInitParams struct {
	client          api.ClientInterface
	clientType      api.ClientType
	systemPrompt    string
	configManager   *configuration.Manager
	workspaceRoot   string
	debug           bool
	interruptCtx    context.Context
	interruptCancel context.CancelFunc
	// subagentDepth tracks the nesting depth of this agent. 0 = primary, 1 = orchestrator, 2 = coder/tester, etc.
	subagentDepth int
	// rootPersonaID tracks the persona of the top-level (depth 0) agent. Propagated to subagents so depth limits can vary by root persona.
	rootPersonaID string
	// isProduction indicates this is a production agent, not a test agent. Production agents have additional initialization steps.
	isProduction bool
}

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
		agent.effectiveContextCap = resolvedCap

		// Activation notice: emit a one-time stderr line when the user set a cap below the native window.
		if cfg != nil && cfg.MaxContextTokens != nil && *cfg.MaxContextTokens > 0 &&
			agent.effectiveContextCap > 0 &&
			agent.effectiveContextCap < nativeWindow {
			_, _ = fmt.Fprintf(os.Stderr,
				"⚡ Context cap active: %s (native: %s)\n"+
					"  All requests will use at most %s of context.\n"+
					"  /max-context clear to remove, /max-context <N> to change.\n",
				agent.formatTokenCount(agent.effectiveContextCap),
				agent.formatTokenCount(nativeWindow),
				agent.formatTokenCount(agent.effectiveContextCap),
			)
		}

		if profile.Mode == configuration.ContextModeLowContext {
			// Show a one-time notice only when LCM was auto-detected, not when explicitly configured.
			explicit := cfg != nil && cfg.ContextMode == configuration.ContextModeLowContext
			if !explicit {
				_, _ = fmt.Fprintf(os.Stderr,
					"⚠ %dK context detected — Low-Context Mode active\n"+
						"  8 tools, lite prompt, AGENTS.md kept\n"+
						"  Set context_mode: \"full\" in config to override, or /model to switch.\n",
					agent.state.GetMaxContextTokens()/1000)
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
	if agent.configManager != nil {cfg := agent.configManager.GetConfig()
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

	// Auto-activate Executive Assistant persona when started from home directory
	if params.isProduction {
		agent.autoActivateCoordinatorPersona()
	}

	// Wire tool function pointers so handlers in pkg/agent_tools can dispatch back into this agent's handler methods.
	wireAgentToolFuncs(agent, params.isProduction)

	return agent, nil
}

// NewAgent creates a new agent with auto-detected provider
func NewAgent() (*Agent, error) {
	return NewAgentWithModel("")
}

// resolveProfileAndSystemPrompt resolves the context profile and matching system prompt.
// Shared by both the SDK/WASM path (NewAgentWithClient) and the CLI path (newAgentWithConfigManagerInner).
// Floor errors (window < 8K) propagate to the caller.
func resolveProfileAndSystemPrompt(
	configManager *configuration.Manager,
	client api.ClientInterface,
	clientType api.ClientType,
	workspaceRoot string,
) (configuration.ContextProfile, string, error) {
	providerName := api.GetProviderName(clientType)

	// Read the model context window. Errors are non-fatal — falls back to default profile.
	contextWindow := 0
	if client != nil {
		if limit, err := client.GetModelContextLimit(); err == nil {
			contextWindow = limit
		}
	}

	// Resolve the profile. Floor errors propagate to the caller.
	var cfg *configuration.Config
	if configManager != nil {
		cfg = configManager.GetConfig()
	}
	profile, err := configuration.ResolveContextProfile(cfg, contextWindow)
	if err != nil {
		return profile, "", agenterrors.NewPermanentError("context profile resolution failed", err)
	}

	// (3) Load the prompt matched to the resolved profile. The path is
	// derived from profile.SystemPromptPath — the helper never hardcodes
	// "prompts/system_prompt.md" or "prompts/system_prompt.lite.md".
	systemPrompt, err := GetEmbeddedSystemPromptForProfile(profile, providerName, contextWindow, workspaceRoot)
	if err != nil {
		return profile, "", agenterrors.NewPermanentError("failed to load system prompt", err)
	}

	// (4) Apply the configured SystemPromptText override if any.
	systemPrompt = resolveConfiguredSystemPrompt(cfg, systemPrompt)

	return profile, systemPrompt, nil
}

// NewAgentWithClient builds an agent around a pre-constructed provider client.
// Skips the interactive provider-resolution path — useful for WASM/SDK callers where the caller already knows which provider and model to use.
// The configManager must already be initialized. The returned agent is a production agent.
func NewAgentWithClient(client api.ClientInterface, clientType api.ClientType, configManager *configuration.Manager) (*Agent, error) {
	if client == nil {
		return nil, agenterrors.NewPermanentError("client is required", nil)
	}
	if configManager == nil {
		return nil, agenterrors.NewPermanentError("configManager is required", nil)
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		workspaceRoot = "."
	}
	if absWorkspaceRoot, absErr := filepath.Abs(workspaceRoot); absErr == nil {
		workspaceRoot = absWorkspaceRoot
	}

	// Resolve the context profile and load the matching system prompt via the shared helper.
	// The profile is also re-resolved inside initAgentFromResolvedProvider; the two resolutions agree because they use the same inputs.
	_, systemPrompt, err := resolveProfileAndSystemPrompt(configManager, client, clientType, workspaceRoot)
	if err != nil {
		return nil, err
	}

	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	return initAgentFromResolvedProvider(agentInitParams{
		client:          client,
		clientType:      clientType,
		systemPrompt:    systemPrompt,
		configManager:   configManager,
		workspaceRoot:   workspaceRoot,
		debug:           isDebugEnvEnabled(),
		interruptCtx:    interruptCtx,
		interruptCancel: interruptCancel,
		isProduction:    true,
	})
}

// NewAgentWithModel creates a new agent with optional model override
func NewAgentWithModel(model string) (*Agent, error) {
	// Initialize configuration manager (silent mode for faster startup)
	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to initialize configuration", err)
	}

	return newAgentWithConfigManager(configManager, model)
}

// NewAgentWithConfigDir creates a new agent using a per-client config directory for WebUI isolation.
func NewAgentWithConfigDir(configDir, model string) (*Agent, error) {
	// Initialize configuration manager with a client-specific directory
	configManager, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		return nil, agenterrors.NewPermanentError(fmt.Sprintf("failed to initialize configuration from %s", configDir), err)
	}

	return newAgentWithConfigManager(configManager, model)
}

// NewAgentWithLayers creates a new agent using layered configuration (global + workspace).
func NewAgentWithLayers(globalDir, workspaceDir, model string) (*Agent, error) {
	configManager, err := configuration.NewManagerWithLayers(globalDir, workspaceDir)
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to initialize layered configuration", err)
	}

	return newAgentWithConfigManager(configManager, model)
}

// NewAgentWithLayersInWorkspace creates a new agent using layered configuration with an explicit workspace root.
func NewAgentWithLayersInWorkspace(globalDir, workspaceDir, workspaceRoot, model string) (*Agent, error) {
	configManager, err := configuration.NewManagerWithLayers(globalDir, workspaceDir)
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to initialize layered configuration", err)
	}

	return newAgentWithConfigManagerAndWorkspace(configManager, workspaceRoot, model)
}

// newAgentWithConfigManagerAndWorkspace is like newAgentWithConfigManager but accepts an explicit workspace root.
func newAgentWithConfigManagerAndWorkspace(configManager *configuration.Manager, workspaceRoot, model string) (*Agent, error) {
	if workspaceRoot == "" {
		var err error
		workspaceRoot, err = os.Getwd()
		if err != nil {
			workspaceRoot = "."
		}
	}
	if absWorkspaceRoot, absErr := filepath.Abs(workspaceRoot); absErr == nil {
		workspaceRoot = absWorkspaceRoot
	}

	return newAgentWithConfigManagerInner(configManager, workspaceRoot, model)
}

// newAgentWithConfigManager is the internal implementation that creates an agent
// with a pre-configured configuration manager.
func newAgentWithConfigManager(configManager *configuration.Manager, model string) (*Agent, error) {
	workspaceRoot, err := os.Getwd()
	if err != nil {
		workspaceRoot = "."
	}
	if absWorkspaceRoot, absErr := filepath.Abs(workspaceRoot); absErr == nil {
		workspaceRoot = absWorkspaceRoot
	}

	return newAgentWithConfigManagerInner(configManager, workspaceRoot, model)
}

// newAgentWithConfigManagerInner is the core implementation that accepts an explicit workspace root.
func newAgentWithConfigManagerInner(configManager *configuration.Manager, workspaceRoot, model string) (*Agent, error) {
	var err error

	var clientType api.ClientType
	var finalModel string

	// --mock-llm flag override: use a deterministic mock provider. Excluded from WASM build.
	if handled, agent, err := tryMockLLMAgent(model, configManager, workspaceRoot); handled {
		return agent, err
	}

	// If running under `go test`, prefer the test/mock client to avoid network/API key
	// dependencies unless explicitly overridden by SPROUT_ALLOW_REAL_PROVIDER (or legacy SPROUT_ALLOW_REAL_PROVIDER).
	if isRunningUnderTest() && configuration.GetEnvSimple("ALLOW_REAL_PROVIDER") == "" {
		clientType = api.TestClientType
		finalModel = model
		// Create the test client immediately to avoid API key checks
		client, err := factory.CreateProviderClient(clientType, finalModel)
		if err != nil {
			return nil, agenterrors.NewProviderError("failed to create API client for tests", err, "", "")
		}

		// Load system prompt for test agent
		providerName := api.GetProviderName(clientType)
		systemPrompt, err := GetEmbeddedSystemPromptWithProvider(providerName)
		if err != nil {
			return nil, agenterrors.NewPermanentError("failed to load system prompt", err)
		}
		systemPrompt = resolveConfiguredSystemPrompt(configManager.GetConfig(), systemPrompt)

		// Initialize agent using the helper
		return initAgentFromResolvedProvider(agentInitParams{
			client:          client,
			clientType:      clientType,
			systemPrompt:    systemPrompt,
			configManager:   configManager,
			workspaceRoot:   workspaceRoot,
			debug:           isDebugEnvEnabled(),
			interruptCtx:    context.Background(),
			interruptCancel: func() { /* no-op */ },
			isProduction:    false,
		})
	}

	// Non-interactive fast-fail: check provider availability before entering the retry loop.
	// SSH daemons allow startup even without a provider so the web UI can handle provider setup.
	if isNonInteractive() && !isRunningUnderTest() && !isSSHDaemon() {
		resolvedType, _, resolveErr := configManager.ResolveProviderModel("", model)
		if resolveErr != nil {
			return nil, agenterrors.NewProviderError("no provider configured. Running in non-interactive mode. "+noninteractive.HelpHint, resolveErr, "", "")
		}
		// Check if editor mode is active
		if resolvedType == api.EditorClientType {
			return nil, agenterrors.NewProviderError("editor mode is active — no AI provider configured. "+
				"Set up a provider with: sprout agent --provider <provider> "+
				"or configure via Settings in the webui (sprout agent -d)", nil, "", "")
		}
		// Provider resolved — ensure API key exists without prompting.
		if keyErr := configManager.EnsureAPIKey(resolvedType); keyErr != nil {
			return nil, agenterrors.NewProviderError("no provider configured. Running in non-interactive mode. "+noninteractive.HelpHint, keyErr, "", "")
		}

		// Warn that non-interactive runs use a permissive security posture.
		console.GlyphWarning.Fprintf(os.Stderr,
			"Non-interactive mode: security is permissive (Medium/High operations auto-approved; only Critical ops block). "+
				"Run inside a container or sandbox for isolation.\n")
	}

	// The early check ensures the provider resolves before the retry loop. The retry loop's recoverProviderStartup calls serve as defense-in-depth.
	clientType, finalModel, err = configManager.ResolveProviderModel("", model)
	if err != nil {
		console.GlyphWarning.Fprintf(os.Stderr, "Failed to resolve configured provider/model: %v", err)
		// SSH daemon exception: allow startup even without provider
		if isSSHDaemon() {
			// Continue with whatever clientType was resolved (may be EditorClientType)
		} else if isNonInteractive() {
			return nil, agenterrors.NewProviderError("no provider configured. Running in non-interactive mode. "+noninteractive.HelpHint, err, "", "")
		} else {
			// Interactive mode: offer to select a provider
			console.GlyphAction.Fprintf(os.Stderr, "Selecting an available provider...")
			clientType, err = configManager.SelectNewProvider()
			if err != nil {
				return nil, agenterrors.NewProviderError("failed to select provider", err, "", "")
			}
			finalModel = configManager.GetModelForProvider(clientType)
			if model != "" && !looksLikeProviderModelSpecifier(configManager, model) {
				finalModel = model
			}
		}
	}

	// Check if editor mode is active — no AI provider configured
	if clientType == api.EditorClientType {
		// SSH daemon exception: try to find a provider with API key automatically
		if isSSHDaemon() {
			if autoProvider, autoModel := findProviderWithAPIKey(configManager); autoProvider != "" {
				console.GlyphInfo.Fprintf(os.Stderr, "SSH: Auto-selected provider %s (has API key)", autoProvider)
				clientType = autoProvider
				finalModel = autoModel
			} else {
				return nil, agenterrors.NewProviderError("editor mode is active — no AI provider configured. "+
					"Set up a provider with: sprout agent --provider <provider> "+
					"or configure via Settings in the webui (sprout agent -d)", nil, "", "")
			}
		} else {
			return nil, agenterrors.NewProviderError("editor mode is active — no AI provider configured. "+
				"Set up a provider with: sprout agent --provider <provider> "+
				"or configure via Settings in the webui (sprout agent -d)", nil, "", "")
		}
	}

	// Ensure provider can be initialized; allow recovery in interactive mode.
	var client api.ClientInterface
	for {
		if err := configManager.EnsureAPIKey(clientType); err != nil {
			console.GlyphWarning.Fprintf(os.Stderr, "Provider %s is not configured: %v", api.GetProviderName(clientType), err)
			nextClientType, nextModel, recoverErr := recoverProviderStartup(configManager, clientType, model, err)
			if recoverErr != nil {
				return nil, agenterrors.NewProviderError("provider recovery failed after ensuring API key", recoverErr, "", "")
			}
			clientType = nextClientType
			finalModel = nextModel
			continue
		}

		// Create the client
		client, err = factory.CreateProviderClient(clientType, finalModel)
		if err != nil {
			nextClientType, nextModel, recoverErr := recoverProviderStartup(configManager, clientType, model, err)
			if recoverErr != nil {
				return nil, agenterrors.NewProviderError("provider recovery failed after creating client", recoverErr, "", "")
			}
			clientType = nextClientType
			finalModel = nextModel
			continue
		}

		// Set debug mode on the client
		debug := isDebugEnvEnabled()
		client.SetDebug(debug)

		// Check connection. Skip for providers where a fast/reliable connectivity probe is not available (Z.AI, GLM Coding).
		skipConnectionCheck := configuration.GetEnvSimple("SKIP_CONNECTION_CHECK") != "" ||
			clientType == api.ZAIClientType ||
			clientType == api.ZAICodingClientType
		if !skipConnectionCheck {
			if err := client.CheckConnection(); err != nil {
				nextClientType, nextModel, recoverErr := recoverProviderStartup(configManager, clientType, model, err)
				if recoverErr != nil {
					return nil, agenterrors.NewProviderError("provider recovery failed after connection check", recoverErr, "", "")
				}
				clientType = nextClientType
				finalModel = nextModel
				continue
			}
		} else if debug {
			fmt.Println()
			console.GlyphWarning.Printf("Skipping provider connection check for %s", api.GetProviderName(clientType))
		}

		break
	}

	// Save the selection
	if err := configManager.SetProvider(clientType); err != nil {
		_, _ = os.Stdout.Write([]byte(fmt.Sprintf("Warning: Failed to save provider selection: %v\n", err)))
	}
	if finalModel != "" && finalModel != configManager.GetModelForProvider(clientType) && clientType != api.TestClientType {
		if err := configManager.SetModelForProvider(clientType, finalModel); err != nil {
			fmt.Println()
			console.GlyphWarning.Printf("Failed to save model selection: %v", err)
		}
	}

	// Check if debug mode is enabled
	debug := isDebugEnvEnabled()

	// Resolve the context profile and load the matching system prompt via the shared helper.
	_, systemPrompt, err := resolveProfileAndSystemPrompt(configManager, client, clientType, workspaceRoot)
	if err != nil {
		return nil, err
	}

	// Create interrupt context for the agent
	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	// Initialize agent using the helper
	return initAgentFromResolvedProvider(agentInitParams{
		client:          client,
		clientType:      clientType,
		systemPrompt:    systemPrompt,
		configManager:   configManager,
		workspaceRoot:   workspaceRoot,
		debug:           debug,
		interruptCtx:    interruptCtx,
		interruptCancel: interruptCancel,
		isProduction:    true,
	})
}

// isHomeDirPath reports whether dir resolves to the user's home directory.
func isHomeDirPath(dir string) bool {
	if dir == "" {
		return false
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	resolvedDir, dirErr := filepath.EvalSymlinks(dir)
	resolvedHome, homeErr := filepath.EvalSymlinks(homeDir)
	if dirErr != nil || homeErr != nil {
		resolvedDir = dir
		resolvedHome = homeDir
	}
	return resolvedDir == resolvedHome
}

// autoActivateCoordinatorPersona auto-activates the Coordinator persona if the workspace is the user's home directory.
func (a *Agent) autoActivateCoordinatorPersona() {
	// Don't override an already-set persona
	if a.state.GetActivePersona() != "" {
		return
	}

	// Honor the user's opt-out flag
	if cfg := a.GetConfig(); cfg != nil && cfg.DisableCoordinatorAutoActivate {
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
