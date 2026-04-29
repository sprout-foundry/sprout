package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
	"github.com/sprout-foundry/sprout/pkg/prompts"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
	"github.com/sprout-foundry/sprout/pkg/validation"
)

const (
	inputInjectionBufferSize = 10
	asyncOutputBufferSize    = 256
)

// PauseState tracks the state when a task is paused for clarification
type PauseState struct {
	IsPaused       bool          `json:"is_paused"`
	PausedAt       time.Time     `json:"paused_at"`
	OriginalTask   string        `json:"original_task"`
	Clarifications []string      `json:"clarifications"`
	MessagesBefore []api.Message `json:"messages_before"`
}

// initSubManagers ensures all sub-managers are initialized.
// This is called lazily to support tests that create bare Agent structs
// with &Agent{} and then call methods that depend on sub-managers.
func (a *Agent) initSubManagers() {
	if a.state == nil {
		a.state = NewAgentStateManager(false)
	}
	if a.output == nil {
		a.output = NewAgentOutputManager()
	}
	if a.security == nil {
		a.security = NewAgentSecurityManager()
	}
	if a.mcpSub == nil {
		a.mcpSub = NewAgentMCPManager()
	}
}

type Agent struct {
	// Core LLM coordination
	client           api.ClientInterface
	clientType       api.ClientType
	systemPrompt     string
	baseSystemPrompt string // Base prompt restored when persona is cleared
	maxIterations    int
	currentIteration int

	// Configuration
	configManager *configuration.Manager
	workspaceRoot string
	debug         bool

	// Input handling
	inputInjectionChan  chan string
	inputInjectionMutex sync.Mutex
	interruptCtx        context.Context
	interruptCancel     context.CancelFunc

	// Sub-managers — Agent coordinates through these interfaces
	state    StateManager    // Conversation history, checkpoints, tokens, cost, persona, etc.
	output   OutputManager   // Streaming, async output, event metadata, output routing
	security SecurityManager // Approvals, redaction, elevation, bypass
	mcpSub   MCPSubManager   // MCP server lifecycle and tool caching

	// Event system (bridges output and core)
	eventBus  *events.EventBus
	validator *validation.Validator

	// Tool execution support
	shellCommandHistory map[string]*ShellCommandResult
	changeTracker       *ChangeTracker
	preparedTools       sync.RWMutex
	lastToolNames       []string

	// UI integration
	ui UI

	// Stats callback
	statsUpdateCallback func(int, float64)

	// Debug logging
	debugLogFile  *os.File
	debugLogPath  string
	debugLogMutex sync.Mutex

	// Trace session for dataset collection
	traceSession interface{}
}

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

// Shutdown attempts to gracefully stop background work and child processes
// (e.g., MCP servers), and releases resources. It is safe to call multiple times.
func (a *Agent) Shutdown() {
	if a == nil {
		return
	}
	a.initSubManagers()

	// Save command history to configuration before shutdown.
	a.state.GetHistoryMutex().Lock()
	a.saveHistoryToConfig()
	a.state.GetHistoryMutex().Unlock()

	// Stop MCP servers (best-effort)
	if mgr := a.mcpSub.GetManager(); mgr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = mgr.StopAll(ctx)
		cancel()
	}

	// Cancel interrupt context
	if a.interruptCancel != nil {
		a.interruptCancel()
	}

	// Close async output worker
	if ch := a.output.GetAsyncOutput(); ch != nil {
		close(ch)
		a.output.SetAsyncOutput(nil)
	}

	// Close debug log file
	if a.debugLogFile != nil {
		_ = a.debugLogFile.Close()
		a.debugLogFile = nil
	}
}

// NewAgent creates a new agent with auto-detected provider
func NewAgent() (*Agent, error) {
	return NewAgentWithModel("")
}

// NewAgentWithModel creates a new agent with optional model override
func NewAgentWithModel(model string) (*Agent, error) {
	// Initialize configuration manager (silent mode for faster startup)
	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize configuration: %w", err)
	}

	return newAgentWithConfigManager(configManager, model)
}

// NewAgentWithConfigDir creates a new agent using a per-client config directory.
// This enables per-client config isolation for the WebUI, where each X-Ledit-Client-ID
// can have its own isolated config directory so settings changes by one client don't affect another.
func NewAgentWithConfigDir(configDir, model string) (*Agent, error) {
	// Initialize configuration manager with a client-specific directory
	configManager, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize configuration from %s: %w", configDir, err)
	}

	return newAgentWithConfigManager(configManager, model)
}

// NewAgentWithLayers creates a new agent using layered configuration.
// globalDir contains global config (~/.config/sprout/), workspaceDir contains workspace config.
// This is the preferred method for WebUI usage where workspace config is supported.
func NewAgentWithLayers(globalDir, workspaceDir, model string) (*Agent, error) {
	configManager, err := configuration.NewManagerWithLayers(globalDir, workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize layered configuration: %w", err)
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
			return nil, fmt.Errorf("failed to create API client for tests: %w", err)
		}

		// Load system prompt for test agent
		providerName := api.GetProviderName(clientType)
		systemPrompt, err := GetEmbeddedSystemPromptWithProvider(providerName)
		if err != nil {
			return nil, fmt.Errorf("failed to load system prompt: %w", err)
		}
		systemPrompt = resolveConfiguredSystemPrompt(configManager.GetConfig(), systemPrompt)

	stateMgr := NewAgentStateManager(isDebugEnvEnabled())
	outputMgr := NewAgentOutputManager()
	securityMgr := NewAgentSecurityManager()
	mcpMgr := NewAgentMCPManager()

	agent := &Agent{
		client:              client,
		systemPrompt:        systemPrompt,
		baseSystemPrompt:    systemPrompt,
		maxIterations:       0,
		clientType:          clientType,
		debug:               isDebugEnvEnabled(),
		configManager:       configManager,
		shellCommandHistory: make(map[string]*ShellCommandResult),
		inputInjectionChan:  make(chan string, inputInjectionBufferSize),
		interruptCtx:        context.Background(),
		interruptCancel:     func() { /* no-op */ },
		workspaceRoot:       workspaceRoot,
		state:               stateMgr,
		output:              outputMgr,
		security:            securityMgr,
		mcpSub:              mcpMgr,
	}

	router := NewOutputRouter(agent, nil)
	agent.output.SetOutputRouter(router)

	agent.state.GetOptimizer().SetLLMClient(agent.client, agent.GetProvider(), func(line string) {
		agent.PrintLineAsync(line)
	})

		// Load command history from configuration
		agent.loadHistoryFromConfig()
		// Initialize debug log file if debug enabled
		if agent.debug {
			if err := agent.initDebugLogger(); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: Failed to initialize debug logger: %v\n", err)
			}
		}

	if persona := strings.TrimSpace(configuration.GetEnvSimple("PERSONA")); persona != "" {
		agent.state.SetActivePersona(strings.ReplaceAll(strings.ToLower(persona), "-", "_"))
	}

		// Initialize change tracker
		agent.changeTracker = NewChangeTracker(agent, "")
		agent.changeTracker.Enable() // Start enabled by default

		return agent, nil
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
			return nil, fmt.Errorf("no provider configured. Running in non-interactive mode. "+noninteractive.HelpHint+": %w", resolveErr)
		}
		// Check if editor mode is active
		if resolvedType == api.EditorClientType {
			return nil, fmt.Errorf("editor mode is active — no AI provider configured. " +
				"Set up a provider with: sprout agent --provider <provider> " +
				"or configure via Settings in the webui (sprout agent -d)")
		}
		// Provider resolved — ensure API key exists without prompting.
		if keyErr := configManager.EnsureAPIKey(resolvedType); keyErr != nil {
			return nil, fmt.Errorf("no provider configured. Running in non-interactive mode. "+noninteractive.HelpHint+": %w", keyErr)
		}
	}

	// NOTE: The early check above ensures that in non-interactive mode the
	// provider resolves and has an API key before reaching the retry loop
	// below. The retry loop's recoverProviderStartup calls include their
	// own non-interactive guards and serve as defense-in-depth, but are
	// unreachable via the non-interactive path when this early check succeeds.
	clientType, finalModel, err = configManager.ResolveProviderModel("", model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] Failed to resolve configured provider/model: %v\n", err)
		// SSH daemon exception: allow startup even without provider
		if isSSHDaemon() {
			// Continue with whatever clientType was resolved (may be EditorClientType)
		} else if isNonInteractive() {
			return nil, fmt.Errorf("no provider configured. Running in non-interactive mode. " + noninteractive.HelpHint)
		} else {
			// Interactive mode: offer to select a provider
			fmt.Fprintf(os.Stderr, "[tool] Selecting an available provider...\n")
			clientType, err = configManager.SelectNewProvider()
			if err != nil {
				return nil, fmt.Errorf("failed to select provider: %w", err)
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
				fmt.Fprintf(os.Stderr, "[SSH] Auto-selected provider %s (has API key)\n", autoProvider)
				clientType = autoProvider
				finalModel = autoModel
			} else {
				return nil, fmt.Errorf("editor mode is active — no AI provider configured. " +
					"Set up a provider with: sprout agent --provider <provider> " +
					"or configure via Settings in the webui (sprout agent -d)")
			}
		} else {
			return nil, fmt.Errorf("editor mode is active — no AI provider configured. " +
				"Set up a provider with: sprout agent --provider <provider> " +
				"or configure via Settings in the webui (sprout agent -d)")
		}
	}

	// Ensure provider can be initialized; allow recovery in interactive mode.
	var client api.ClientInterface
	for {
		if err := configManager.EnsureAPIKey(clientType); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] Provider %s is not configured: %v\n", api.GetProviderName(clientType), err)
			nextClientType, nextModel, recoverErr := recoverProviderStartup(configManager, clientType, model, err)
			if recoverErr != nil {
				return nil, fmt.Errorf("provider recovery failed after ensuring API key: %w", recoverErr)
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
				return nil, fmt.Errorf("provider recovery failed after creating client: %w", recoverErr)
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
					return nil, fmt.Errorf("provider recovery failed after connection check: %w", recoverErr)
				}
				clientType = nextClientType
				finalModel = nextModel
				continue
			}
		} else if debug {
			fmt.Printf("\n[WARN] Skipping provider connection check for %s\n", api.GetProviderName(clientType))
		}

		break
	}

	// Save the selection
	if err := configManager.SetProvider(clientType); err != nil {
		fmt.Printf("Warning: Failed to save provider selection: %v\n", err)
	}
	if finalModel != "" && finalModel != configManager.GetModelForProvider(clientType) && clientType != api.TestClientType {
		if err := configManager.SetModelForProvider(clientType, finalModel); err != nil {
			fmt.Printf("\n[WARN] Warning: Failed to save model selection: %v\n", err)
		}
	}

	// Check if debug mode is enabled
	debug := isDebugEnvEnabled()

	// Use embedded system prompt with provider-specific enhancements
	providerName := api.GetProviderName(clientType)
	systemPrompt, err := GetEmbeddedSystemPromptWithProvider(providerName)
	if err != nil {
		return nil, fmt.Errorf("failed to load system prompt: %w", err)
	}
	systemPrompt = resolveConfiguredSystemPrompt(configManager.GetConfig(), systemPrompt)

	// Clear old todos at session start
	tools.TodoWrite([]tools.TodoItem{})

	// Clean up old sessions (keep only most recent 20 for this working directory scope).
	if err := cleanupMemorySessions(); err != nil && debug {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to clean up old sessions: %v\n", err)
	}

	// Create interrupt context for the agent
	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	stateMgr := NewAgentStateManager(debug)
	outputMgr := NewAgentOutputManager()
	securityMgr := NewAgentSecurityManager()
	mcpMgr := NewAgentMCPManager()

	agent := &Agent{
		client:              client,
		systemPrompt:        systemPrompt,
		baseSystemPrompt:    systemPrompt,
		maxIterations:       0,
		clientType:          clientType,
		debug:               debug,
		configManager:       configManager,
		shellCommandHistory: make(map[string]*ShellCommandResult),
		inputInjectionChan:  make(chan string, inputInjectionBufferSize),
		interruptCtx:        interruptCtx,
		interruptCancel:     interruptCancel,
		workspaceRoot:       workspaceRoot,
		state:               stateMgr,
		output:              outputMgr,
		security:            securityMgr,
		mcpSub:              mcpMgr,
	}

	router := NewOutputRouter(agent, nil)
	agent.output.SetOutputRouter(router)

	agent.state.GetOptimizer().SetLLMClient(agent.client, agent.GetProvider(), func(line string) {
		agent.PrintLineAsync(line)
	})

	// Initialize debug log file if debug enabled
	if debug {
		if err := agent.initDebugLogger(); err != nil {
			// Non-fatal: fall back to stdout debug
			fmt.Fprintf(os.Stderr, "WARNING: Failed to initialize debug logger: %v\n", err)
		}
	}

	// Initialize context limits based on model
	agent.state.SetMaxContextTokens(agent.getModelContextLimit())
	agent.state.SetCurrentContextTokens(0)
	agent.state.SetContextWarningIssued(false)

	// Initialize change tracker
	agent.changeTracker = NewChangeTracker(agent, "")
	agent.changeTracker.Enable() // Start enabled by default

	// Pre-initialize tool registry to avoid first-use overhead
	if debug {
		fmt.Printf("\n[cfg] Pre-initializing tool registry...\n")
	}
	InitializeToolRegistry()
	if debug {
		fmt.Printf("[ok] Tool registry initialized\n")
	}

	// Load command history from configuration
	agent.loadHistoryFromConfig()

	if persona := strings.TrimSpace(configuration.GetEnvSimple("PERSONA")); persona != "" {
		agent.state.SetActivePersona(strings.ReplaceAll(strings.ToLower(persona), "-", "_"))
	}

	return agent, nil
}

// GetDebugLogPath returns the path to the current debug log file (if any)
func (a *Agent) GetDebugLogPath() string { return a.debugLogPath }

// GetUnsafeMode returns whether unsafe mode is enabled
func (a *Agent) GetUnsafeMode() bool { return a.security.GetUnsafeMode() }

// SetUnsafeMode sets the unsafe mode flag
func (a *Agent) SetUnsafeMode(unsafe bool) { a.security.SetUnsafeMode(unsafe) }

// IsSecurityBypassApproved returns whether the user has approved filesystem access outside CWD
func (a *Agent) IsSecurityBypassApproved() bool {
	return a.security.IsSecurityBypassApproved()
}

// SetSecurityBypassApproved marks that the user has approved filesystem access outside CWD for this session
func (a *Agent) SetSecurityBypassApproved() {
	a.security.SetSecurityBypassApproved()
}

// CheckFileContentSecurity runs security concern detection on file content after a write.
// In WebUI mode, it uses the event-bus-based ApprovalManager to show a dialog.
// In CLI mode, it falls back to the interactive logger prompt.
// Ignored concerns are tracked per-file so they are not re-prompted.
func (a *Agent) CheckFileContentSecurity(filePath string, content string) {
	promptManager := security.GetGlobalApprovalManager()
	eventBus := a.GetEventBus()

	if promptManager == nil && eventBus == nil {
		return
	}

	concerns, snippets := security.DetectSecurityConcernsWithContext(content, filePath)
	if len(concerns) == 0 {
		return
	}

	logger := utils.GetLogger(false)

	for _, concern := range concerns {
		if a.security.IsConcernIgnored(filePath, concern) {
			continue
		}

		snippet := ""
		if snippets != nil {
			snippet = snippets[concern]
		}
		prompt := prompts.PotentialSecurityConcernsFound(filePath, concern, snippet)

		var userResponse bool

		if eventBus != nil && promptManager != nil && a.security.HasActiveWebUIClients() {
			extras := map[string]string{
				"file_path": filePath,
				"concern":   concern,
			}
			userResponse = promptManager.RequestPrompt(eventBus, prompt, true, extras)
			logger.Logf("Security concern '%s' in %s user response: %v", concern, filePath, userResponse)
		} else {
			userResponse = logger.AskForConfirmation(prompt, true, false)
		}

		if userResponse {
			logger.Logf("Security concern '%s' in %s noted as an issue.", concern, filePath)
		} else {
			logger.Logf("Security concern '%s' in %s noted as unimportant.", concern, filePath)
			a.security.SetConcernIgnored(filePath, concern)
		}
	}
}

// SetInterruptHandler sets the interrupt handler for UI mode
func (a *Agent) SetInterruptHandler(ch chan struct{}) {
	// Store the channel for external interrupt handling
	// Note: This is kept for backward compatibility
	// Interrupts are now primarily handled via context cancellation
}

// GetMessages returns the current conversation messages
func (a *Agent) GetMessages() []api.Message {
	if a.state == nil {
		return nil
	}
	return a.state.GetMessages()
}

// SetMessages sets the conversation messages (for restore)
func (a *Agent) SetMessages(messages []api.Message) {
	if a.state != nil {
		a.state.SetMessages(messages)
	}
}

// AddMessage adds a single message to the conversation history
func (a *Agent) AddMessage(message api.Message) {
	if a.state != nil {
		a.state.AddMessage(message)
	}
}

// GetTotalCost returns the total cost of the conversation
func (a *Agent) GetTotalCost() float64 {
	return a.state.GetTotalCost()
}

// GetTaskActions returns completed task actions
func (a *Agent) GetTaskActions() []TaskAction {
	return a.state.GetTaskActions()
}

// IsInteractiveMode returns true if running in interactive mode
func (a *Agent) IsInteractiveMode() bool {
	return configuration.GetEnvSimple("INTERACTIVE") == "1" ||
		configuration.GetEnvSimple("FROM_AGENT") != "1"
}

// GenerateResponse generates a simple response using the current model without tool calls
func (a *Agent) GenerateResponse(messages []api.Message) (string, error) {
	resp, err := a.client.SendChatRequest(messages, nil, "", false) // No tools, no reasoning, no disableThinking
	if err != nil {
		return "", fmt.Errorf("failed to generate response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response generated for %d messages", len(messages))
	}

	return resp.Choices[0].Message.Content, nil
}

// SetStatsUpdateCallback sets a callback for token/cost updates
func (a *Agent) SetStatsUpdateCallback(callback func(int, float64)) {
	a.statsUpdateCallback = callback
}

// GetConfig returns the configuration
func (a *Agent) GetConfig() *configuration.Config {
	if a.configManager == nil {
		return nil
	}
	return a.configManager.GetConfig()
}

// SetWorkspaceRoot records the logical workspace root for this agent instance.
func (a *Agent) SetWorkspaceRoot(workspaceRoot string) {
	a.workspaceRoot = strings.TrimSpace(workspaceRoot)
}

// GetWorkspaceRoot returns the logical workspace root for this agent instance.
func (a *Agent) GetWorkspaceRoot() string {
	return strings.TrimSpace(a.workspaceRoot)
}

// SetConfigOverrides stores session-scoped config overrides on the agent.
// These are applied in-memory and persisted with the session state.
func (a *Agent) SetConfigOverrides(overrides map[string]interface{}) {
	a.state.SetConfigOverrides(overrides)
}

// GetConfigOverrides returns the session-scoped config overrides.
func (a *Agent) GetConfigOverrides() map[string]interface{} {
	return a.state.GetConfigOverrides()
}

// currentWorkspaceRoot resolves the agent workspace, falling back to the process cwd.
func (a *Agent) currentWorkspaceRoot() string {
	if root := strings.TrimSpace(a.workspaceRoot); root != "" {
		return root
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// OutputRouter returns the current output router (nil if not initialized)
func (a *Agent) OutputRouter() *OutputRouter { return a.output.GetOutputRouter() }

// PrintTerminalOnly writes text to the terminal without publishing to the event bus.
// Use this for output already published via a more specific event type.
func (a *Agent) PrintTerminalOnly(text string) {
	if a == nil {
		return
	}
	if a.output == nil {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Print(text)
		return
	}
	router := a.output.GetOutputRouter()
	if router == nil {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Print(text)
		return
	}
	router.RouteTerminalOnly(text)
}

// GetSecurityApprovalMgr returns the security approval manager
func (a *Agent) GetSecurityApprovalMgr() *security.ApprovalManager {
	return a.security.GetSecurityApprovalMgr()
}

// SetHasActiveWebUIClients sets a callback that returns whether any WebUI
// clients are currently connected. The security prompting logic uses this
// to decide between WebUI event-bus routing and CLI-based prompting.
func (a *Agent) SetHasActiveWebUIClients(fn func() bool) {
	a.security.SetHasActiveWebUIClients(fn)
}

// HasActiveWebUIClients calls the registered callback (or returns false if
// none is set) to check whether WebUI clients are connected.
func (a *Agent) HasActiveWebUIClients() bool {
	return a.security.HasActiveWebUIClients()
}

// SetSystemPrompt sets the system prompt for the agent
func (a *Agent) SetSystemPrompt(prompt string) {
	a.systemPrompt = a.ensureStopInformation(prompt)
}

// SetBaseSystemPrompt updates the baseline prompt used when persona overrides are cleared.
func (a *Agent) SetBaseSystemPrompt(prompt string) {
	a.baseSystemPrompt = a.ensureStopInformation(prompt)
	if strings.TrimSpace(a.baseSystemPrompt) == "" {
		a.baseSystemPrompt = a.systemPrompt
	}
}

// GetSystemPrompt returns the current system prompt
func (a *Agent) GetSystemPrompt() string {
	return a.systemPrompt
}

// GetValidator returns the syntax validator (nil until SetEventBus is called).
func (a *Agent) GetValidator() *validation.Validator {
	return a.validator
}

// SetTraceSessionsets the trace session for dataset collection
func (a *Agent) SetTraceSession(traceSession interface{}) {
	a.state.SetTraceSession(traceSession)
}
