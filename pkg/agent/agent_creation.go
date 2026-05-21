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
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
)

// sessionCleanupOnce ensures session cleanup runs only once per process,
// preventing repeated cleanup in daemon mode where multiple agents are created.
var sessionCleanupOnce sync.Once

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

// agentInitParams encapsulates the parameters needed to initialize an Agent
// after the provider and client have been resolved.
type agentInitParams struct {
	client          api.ClientInterface
	clientType      api.ClientType
	systemPrompt    string
	configManager   *configuration.Manager
	workspaceRoot   string
	debug           bool
	interruptCtx    context.Context
	interruptCancel context.CancelFunc
	// subagentDepth tracks the nesting depth of this agent.
	// 0 = primary agent (EA), 1 = orchestrator, 2 = coder/tester, etc.
	subagentDepth int
	// rootPersonaID tracks the persona of the top-level (depth 0) agent.
	// Propagated to subagents so depth limits can vary by root persona.
	rootPersonaID string
	// isProduction indicates this is a production agent, not a test agent.
	// Production agents have additional initialization steps (context limits,
	// todo clearing, session cleanup, tool registry initialization).
	isProduction bool
}

// initAgentFromResolvedProvider creates and initializes an Agent from resolved
// provider parameters. This consolidates the common agent initialization logic
// that was duplicated between test and production paths.
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

		// Clean up old sessions once per process. Uses sync.Once so daemon
		// mode (which creates agents per chat session) only runs cleanup on
		// the very first agent, not on every subsequent chat session.
		sessionCleanupOnce.Do(func() {
			if err := cleanupMemorySessions(); err != nil && agent.debug {
				_, _ = os.Stderr.Write([]byte(fmt.Sprintf("WARNING: Failed to clean up old sessions: %v\n", err)))
			}
		})

		// Sweep expired persistent context entries based on retention policy
		if agent.configManager != nil {
			cfg := agent.configManager.GetConfig()
			if cfg != nil && cfg.PersistentContext != nil && cfg.PersistentContext.RetentionDays > 0 {
				// Resolve storePath using the same logic as EmbeddingManager.initLocked()
				convoStoreDir := ""
				if cfg.EmbeddingIndex != nil {
					convoStoreDir = cfg.EmbeddingIndex.IndexDir
				}
				if convoStoreDir == "" {
					configDir := os.Getenv("SPROUT_CONFIG")
					if configDir == "" {
						configDir = os.Getenv("LEDIT_CONFIG")
					}
					if configDir == "" {
						home, _ := os.UserHomeDir() // Unlikely to fail; fallback below handles it gracefully
						configDir = filepath.Join(home, ".config", "sprout")
					}
					convoStoreDir = filepath.Join(configDir, "embeddings")
				}
				convoStorePath := filepath.Join(convoStoreDir, "conversation_turns.jsonl")
				swept, sweepErr := SweepExpiredEntries(cfg.PersistentContext.RetentionDays, convoStorePath)
				if sweepErr != nil && agent.debug {
					_, _ = os.Stderr.Write([]byte(fmt.Sprintf("WARNING: Failed to sweep expired context entries: %v\n", sweepErr)))
				} else if swept > 0 && agent.debug {
					_, _ = os.Stderr.Write([]byte(fmt.Sprintf("Swept %d expired context entries\n", swept)))
				}
			}
		}

		// Pre-initialize tool registry to avoid first-use overhead (safe: sync.Once)
		if agent.debug {
			agent.Logger().Info("Pre-initializing tool registry...")
		}
		InitializeToolRegistry()
		if agent.debug {
			agent.Logger().Info("Tool registry initialized")
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

	// Restore embedding index if previously enabled for this workspace
	agent.RestoreEmbeddingIndex()

	// Auto-activate Executive Assistant persona when started from home directory
	if params.isProduction {
		agent.autoActivateEAPersona()
	}

	return agent, nil
}

// NewAgent creates a new agent with auto-detected provider
func NewAgent() (*Agent, error) {
	return NewAgentWithModel("")
}

// NewAgentWithClient builds an agent around a pre-constructed provider
// client. The interactive provider-resolution path in newAgentWithConfigManager
// (API-key prompts, connection checks, recovery loops) is skipped — useful
// for WASM/SDK callers where the caller already knows which provider and
// model to use, and where API keys live elsewhere (e.g. attached server-side
// by the sprout-foundry platform proxy).
//
// The configManager must already be initialized; pass one from
// configuration.NewManagerSilent() or similar. The returned agent is a
// production agent (full lifecycle: context limits, session cleanup, tool
// registry, persona auto-activation).
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

	providerName := api.GetProviderName(clientType)
	systemPrompt, err := GetEmbeddedSystemPromptWithProvider(providerName)
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to load system prompt", err)
	}
	systemPrompt = resolveConfiguredSystemPrompt(configManager.GetConfig(), systemPrompt)

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

// NewAgentWithConfigDir creates a new agent using a per-client config directory.
// This enables per-client config isolation for the WebUI, where each X-Sprout-Client-ID
// can have its own isolated config directory so settings changes by one client don't affect another.
func NewAgentWithConfigDir(configDir, model string) (*Agent, error) {
	// Initialize configuration manager with a client-specific directory
	configManager, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		return nil, agenterrors.NewPermanentError(fmt.Sprintf("failed to initialize configuration from %s", configDir), err)
	}

	return newAgentWithConfigManager(configManager, model)
}

// NewAgentWithLayers creates a new agent using layered configuration.
// globalDir contains global config (~/.config/sprout/), workspaceDir contains workspace config.
// This is the preferred method for WebUI usage where workspace config is supported.
func NewAgentWithLayers(globalDir, workspaceDir, model string) (*Agent, error) {
	configManager, err := configuration.NewManagerWithLayers(globalDir, workspaceDir)
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to initialize layered configuration", err)
	}

	return newAgentWithConfigManager(configManager, model)
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

	var clientType api.ClientType
	var finalModel string

	// If running under `go test`, prefer the test/mock client to avoid network/API key
	// dependencies unless explicitly overridden by SPROUT_ALLOW_REAL_PROVIDER (or legacy LEDIT_ALLOW_REAL_PROVIDER).
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

	// Non-interactive fast-fail: check provider availability before entering
	// the retry loop. In non-interactive mode (daemon, piped input, CI),
	// we cannot prompt for provider selection or API keys, so fail early with
	// a clear message if no provider is usable.
	//
	// NOTE: This early-exit path is not directly testable under `go test`
	// because isRunningUnderTest() returns true for all test binaries
	// (which inject -test.* flags into os.Args). End-to-end validation is
	// covered by webui integration tests and manual daemon testing.
	//
	// EXCEPTION: SSH daemons set BROWSER=none and allow startup even
	// without a provider so that the web UI can handle provider setup.
	// This supports SSH workspace setup where the daemon starts on a fresh
	// remote machine before provider is configured.
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
	}

	// NOTE: The early check above ensures that in non-interactive mode the
	// provider resolves and has an API key before reaching the retry loop
	// below. The retry loop's recoverProviderStartup calls include their
	// own non-interactive guards and serve as defense-in-depth, but are
	// unreachable via the non-interactive path when this early check succeeds.
	clientType, finalModel, err = configManager.ResolveProviderModel("", model)
	if err != nil {
		_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[WARN] Failed to resolve configured provider/model: %v\n", err)))
		// SSH daemon exception: allow startup even without provider
		if isSSHDaemon() {
			// Continue with whatever clientType was resolved (may be EditorClientType)
		} else if isNonInteractive() {
			return nil, agenterrors.NewProviderError("no provider configured. Running in non-interactive mode. "+noninteractive.HelpHint, err, "", "")
		} else {
			// Interactive mode: offer to select a provider
			_, _ = os.Stderr.Write([]byte("[tool] Selecting an available provider...\n"))
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
				_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[SSH] Auto-selected provider %s (has API key)\n", autoProvider)))
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
			_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[WARN] Provider %s is not configured: %v\n", api.GetProviderName(clientType), err)))
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

		// Check connection (allow tests to skip by setting SPROUT_SKIP_CONNECTION_CHECK or legacy LEDIT_SKIP_CONNECTION_CHECK)
		// Also skip for providers where a fast/reliable connectivity probe is not available (e.g., Z.AI Coding Plan).
		skipConnectionCheck := configuration.GetEnvSimple("SKIP_CONNECTION_CHECK") != "" || clientType == api.ZAIClientType
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
			_, _ = os.Stdout.Write([]byte(fmt.Sprintf("\n[WARN] Skipping provider connection check for %s\n", api.GetProviderName(clientType))))
		}

		break
	}

	// Save the selection
	if err := configManager.SetProvider(clientType); err != nil {
		_, _ = os.Stdout.Write([]byte(fmt.Sprintf("Warning: Failed to save provider selection: %v\n", err)))
	}
	if finalModel != "" && finalModel != configManager.GetModelForProvider(clientType) && clientType != api.TestClientType {
		if err := configManager.SetModelForProvider(clientType, finalModel); err != nil {
			_, _ = os.Stdout.Write([]byte(fmt.Sprintf("\n[WARN] Warning: Failed to save model selection: %v\n", err)))
		}
	}

	// Check if debug mode is enabled
	debug := isDebugEnvEnabled()

	// Use embedded system prompt with provider-specific enhancements
	providerName := api.GetProviderName(clientType)
	systemPrompt, err := GetEmbeddedSystemPromptWithProvider(providerName)
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to load system prompt", err)
	}
	systemPrompt = resolveConfiguredSystemPrompt(configManager.GetConfig(), systemPrompt)

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

// autoActivateEAPersona checks if the Executive Assistant persona should be
// auto-activated based on the workspace root being the user's home directory.
// This is a no-op if a persona is already set or if the EA persona is unavailable.
func (a *Agent) autoActivateEAPersona() {
	// Don't override an already-set persona
	if a.state.GetActivePersona() != "" {
		return
	}

	// Only activate when workspace is home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	// Resolve symlinks on both paths to avoid mismatch
	// (e.g., macOS /Users → /private/Users, or custom symlinked home)
	resolvedWorkspace, wsErr := filepath.EvalSymlinks(a.workspaceRoot)
	resolvedHome, homeErr := filepath.EvalSymlinks(homeDir)
	if wsErr != nil || homeErr != nil {
		// Fall back to direct comparison if symlink resolution fails
		resolvedWorkspace = a.workspaceRoot
		resolvedHome = homeDir
	}
	if resolvedWorkspace != resolvedHome {
		return
	}

	// Check if EA persona is available
	personaID := "executive_assistant"
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
		_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[WARN] Failed to auto-activate EA persona: %v\n", err)))
	}
}
