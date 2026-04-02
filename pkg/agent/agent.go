package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/mcp"
	"github.com/alantheprice/ledit/pkg/validation"
)

const (
	inputInjectionBufferSize = 10
	asyncOutputBufferSize    = 256
)

var errProviderStartupClosed = errors.New("provider startup cancelled by user")

// PauseState tracks the state when a task is paused for clarification
type PauseState struct {
	IsPaused       bool          `json:"is_paused"`
	PausedAt       time.Time     `json:"paused_at"`
	OriginalTask   string        `json:"original_task"`
	Clarifications []string      `json:"clarifications"`
	MessagesBefore []api.Message `json:"messages_before"`
}

type Agent struct {
	client                  api.ClientInterface
	messages                []api.Message
	systemPrompt            string
	baseSystemPrompt        string // Base prompt restored when persona is cleared
	maxIterations           int
	currentIteration        int
	totalCost               float64
	clientType              api.ClientType
	taskActions             []TaskAction                   // Track what was accomplished
	debug                   bool                           // Enable debug logging
	totalTokens             int                            // Track total tokens used across all requests
	promptTokens            int                            // Track total prompt tokens
	completionTokens        int                            // Track total completion tokens
	estimatedTokenResponses int                            // Number of responses where token usage was estimated
	cachedTokens            int                            // Track tokens that were cached/reused
	cachedCostSavings       float64                        // Track cost savings from cached tokens
	previousSummary         string                         // Summary of previous actions for continuity
	sessionID               string                         // Unique session identifier
	turnCheckpoints         []TurnCheckpoint               // Completed-turn summaries used when context gets tight
	checkpointMu            sync.RWMutex                   // Protects background checkpoint compaction
	optimizer               *ConversationOptimizer         // Conversation optimization
	configManager           *configuration.Manager         // Configuration management
	currentContextTokens    int                            // Current context size being sent to model
	maxContextTokens        int                            // Model's maximum context window
	contextWarningIssued    bool                           // Whether we've warned about approaching context limit
	shellCommandHistory     map[string]*ShellCommandResult // Track shell commands for deduplication
	changeTracker           *ChangeTracker                 // Track file changes for rollback support
	mcpManager              mcp.MCPManager                 // MCP server management
	mcpToolsCache           []api.Tool                     // Cached MCP tools to avoid reloading
	mcpInitialized          bool                           // Track whether MCP has been initialized
	mcpInitErr              error                          // Store initialization error
	mcpInitMu               sync.Mutex                     // Protect concurrent initialization
	circuitBreaker          *CircuitBreakerState           // Track repetitive actions
	conversationPruner      *ConversationPruner            // Automatic conversation pruning
	toolCallGuidanceAdded   bool                           // Prevent repeating tool call guidance
	activeSkills            []string                       // Currently activated skills (by ID)
	activePersona           string                         // Currently active persona ID (direct agent or subagent env)
	workspaceRoot           string                         // Explicit workspace root for this agent instance

	// Session-scoped provider/model overrides (webui sessions)
	// When set, these take precedence over config values and don't persist
	sessionProvider api.ClientType // Session-scoped provider override
	sessionModel    string         // Session-scoped model override

	// Input injection handling
	inputInjectionChan  chan string        // Channel for injecting new user input
	inputInjectionMutex sync.Mutex         // Mutex for input injection operations
	interruptCtx        context.Context    // Context for interrupt handling
	interruptCancel     context.CancelFunc // Cancel function for interrupt context
	outputMutex         *sync.Mutex        // Mutex for synchronized output
	streamingEnabled    bool               // Whether streaming is enabled
	streamingCallback   func(string)       // Custom streaming callback
	reasoningCallback   func(string)       // Custom reasoning/thinking callback
	streamingBuffer     strings.Builder    // Buffer for streaming content
	reasoningBuffer     strings.Builder    // Buffer for reasoning content
	flushCallback       func()             // Callback to flush buffered output
	asyncOutput         chan string        // Buffered channel for async PrintLine calls

	// Command history for interactive mode
	historyMu       sync.Mutex // Protects commandHistory and historyIndex
	commandHistory  []string   // History of entered commands
	historyIndex    int        // Current position in history for navigation
	asyncOutputOnce sync.Once  // Ensure async worker initializes once
	asyncBufferSize int        // Optional override for async output buffer (tests)

	// Pause/resume state management
	pauseState *PauseState // Current pause state and context
	pauseMutex sync.Mutex  // Mutex for pause state operations

	// Trace session for dataset collection
	traceSession interface{} // Using interface{} to avoid circular dependency

	// Feature flags
	falseStopDetectionEnabled bool
	statsUpdateCallback       func(int, float64) // Callback for token/cost updates
	lastRunTerminationReason  string
	enablePreWriteValidation  bool // Enable syntax validation before writes

	// UI integration
	ui UI // UI provider for dropdowns, etc.

	// Event system
	eventBus *events.EventBus // Event bus for real-time UI updates

	outputRouter *OutputRouter // Single routing layer for all output (terminal + webui)

	// Security approval system (webui fallback when stdin unavailable)
	securityApprovalMgr *SecurityApprovalManager

	// Validation system
	validator *validation.Validator // Syntax validation and async diagnostics

	// Debug logging
	debugLogFile  *os.File   // File handle for debug logs
	debugLogPath  string     // Path to the debug log file
	debugLogMutex sync.Mutex // Mutex for safe writes to debug log
	preparedTools sync.RWMutex
	lastToolNames []string

	// One-shot context note injected after provider/model switches that require syntax normalization.
	pendingSwitchContextRefresh string
	// One-shot user-facing status notice for slash commands after strict-syntax switch normalization.
	pendingStrictSwitchNotice string

	// Unsafe mode - bypass most security checks
	unsafeMode bool // Allow operations without security prompting

	// Filesystem security bypass approval - once user approves access outside CWD,
	// all subsequent requests in the session are allowed without re-prompting
	securityBypassApproved bool
	securityBypassMu       sync.RWMutex

	// Subagent output batching - buffer events to reduce event bus traffic
	subagentBatchBuffer     []string            // Buffered subagent output lines
	subagentBatchCount      int                 // Number of lines in buffer
	subagentBatchMutex      sync.Mutex          // Protect batch buffer
	subagentBatchSize       int                 // Flush threshold (default 50)
	subagentBatchMilestones map[string]struct{} // Milestone phases that force immediate flush
	eventMetadataMu         sync.RWMutex
	eventMetadata           map[string]interface{}
}

func isDebugEnvEnabled() bool {
	value := strings.TrimSpace(os.Getenv("LEDIT_DEBUG"))
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

	// Save command history to configuration before shutdown.
	// Lock historyMu to avoid racing with concurrent AddToHistory calls.
	a.historyMu.Lock()
	a.saveHistoryToConfig()
	a.historyMu.Unlock()

	// Stop MCP servers (best-effort)
	if a.mcpManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.mcpManager.StopAll(ctx)
		cancel()
	}

	// Cancel interrupt context
	if a.interruptCancel != nil {
		a.interruptCancel()
	}

	// Close async output worker
	if a.asyncOutput != nil {
		// Close channel to stop background worker started in ensureAsyncOutputWorker
		close(a.asyncOutput)
		a.asyncOutput = nil
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
	// dependencies unless explicitly overridden by LEDIT_ALLOW_REAL_PROVIDER.
	if isRunningUnderTest() && os.Getenv("LEDIT_ALLOW_REAL_PROVIDER") == "" {
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

		// Create agent with minimal initialization using test client
		agent := &Agent{
			client:                    client,
			messages:                  []api.Message{},
			systemPrompt:              systemPrompt,
			baseSystemPrompt:          systemPrompt,
			maxIterations:             0, // 0 means unlimited
			totalCost:                 0.0,
			clientType:                clientType,
			debug:                     isDebugEnvEnabled(),
			optimizer:                 NewConversationOptimizer(true, false),
			configManager:             configManager,
			shellCommandHistory:       make(map[string]*ShellCommandResult),
			inputInjectionChan:        make(chan string, inputInjectionBufferSize),
			interruptCtx:              context.Background(),
			interruptCancel:           func() { /* no-op */ },
			falseStopDetectionEnabled: true,
			conversationPruner:        NewConversationPruner(false),
			activePersona:             "orchestrator",
			workspaceRoot:             workspaceRoot,
			securityApprovalMgr:       NewSecurityApprovalManager(),
			outputRouter:              NewOutputRouter(nil, nil),
		}

		agent.optimizer.SetLLMClient(agent.client, agent.GetProvider(), func(line string) {
			agent.PrintLineAsync(line)
		})

		// Wire output router with the agent reference now that agent exists
		if agent.outputRouter != nil {
			agent.outputRouter.agent = agent
		}

		// Load command history from configuration
		agent.loadHistoryFromConfig() // Initialize debug log file if debug enabled
		if agent.debug {
			if err := agent.initDebugLogger(); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: Failed to initialize debug logger: %v\n", err)
			}
		}

		// Initialize MCP manager
		agent.mcpManager = mcp.NewMCPManager(nil)

		if persona := strings.TrimSpace(os.Getenv("LEDIT_PERSONA")); persona != "" {
			agent.activePersona = strings.ReplaceAll(strings.ToLower(persona), "-", "_")
		}

		// Initialize change tracker
		agent.changeTracker = NewChangeTracker(agent, "")
		agent.changeTracker.Enable() // Start enabled by default

		return agent, nil
	}

	clientType, finalModel, err = configManager.ResolveProviderModel("", model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] Failed to resolve configured provider/model: %v\n", err)
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

	// Ensure provider can be initialized; allow recovery in interactive mode.
	var client api.ClientInterface
	for {
		if err := configManager.EnsureAPIKey(clientType); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] Provider %s is not configured: %v\n", api.GetProviderName(clientType), err)
			nextClientType, nextModel, recoverErr := recoverProviderStartup(configManager, clientType, model, err)
			if recoverErr != nil {
				return nil, recoverErr
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
				return nil, recoverErr
			}
			clientType = nextClientType
			finalModel = nextModel
			continue
		}

		// Set debug mode on the client
		debug := isDebugEnvEnabled()
		client.SetDebug(debug)

		// Check connection (allow tests to skip by setting LEDIT_SKIP_CONNECTION_CHECK)
		// Also skip for providers where a fast/reliable connectivity probe is not available (e.g., Z.AI Coding Plan).
		skipConnectionCheck := os.Getenv("LEDIT_SKIP_CONNECTION_CHECK") != "" || clientType == api.ZAIClientType
		if !skipConnectionCheck {
			if err := client.CheckConnection(); err != nil {
				nextClientType, nextModel, recoverErr := recoverProviderStartup(configManager, clientType, model, err)
				if recoverErr != nil {
					return nil, recoverErr
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
	if finalModel != "" && finalModel != configManager.GetModelForProvider(clientType) {
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

	// Conversation optimization is always enabled
	optimizationEnabled := true

	// Create interrupt context for the agent
	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	// Create the agent
	agent := &Agent{
		client:                    client,
		messages:                  []api.Message{},
		systemPrompt:              systemPrompt,
		baseSystemPrompt:          systemPrompt,
		maxIterations:             0, // 0 means unlimited
		totalCost:                 0.0,
		clientType:                clientType,
		debug:                     debug,
		optimizer:                 NewConversationOptimizer(optimizationEnabled, debug),
		configManager:             configManager,
		shellCommandHistory:       make(map[string]*ShellCommandResult),
		inputInjectionChan:        make(chan string, inputInjectionBufferSize),
		interruptCtx:              interruptCtx,
		interruptCancel:           interruptCancel,
		falseStopDetectionEnabled: true,
		conversationPruner:        NewConversationPruner(debug),
		commandHistory:            []string{},
		historyIndex:              -1,
		activePersona:             "orchestrator",
		workspaceRoot:             workspaceRoot,
		securityApprovalMgr:       NewSecurityApprovalManager(),
		outputRouter:              NewOutputRouter(nil, nil),
	}

	agent.optimizer.SetLLMClient(agent.client, agent.GetProvider(), func(line string) {
		agent.PrintLineAsync(line)
	})

	// Wire output router with the agent reference now that agent exists
	if agent.outputRouter != nil {
		agent.outputRouter.agent = agent
	}

	// Initialize debug log file if debug enabled
	if debug {
		if err := agent.initDebugLogger(); err != nil {
			// Non-fatal: fall back to stdout debug
			fmt.Fprintf(os.Stderr, "WARNING: Failed to initialize debug logger: %v\n", err)
		}
	}

	// Initialize context limits based on model
	agent.maxContextTokens = agent.getModelContextLimit()
	agent.currentContextTokens = 0
	agent.contextWarningIssued = false

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

	// Initialize MCP manager (but don't start servers yet - lazy load)
	agent.mcpManager = mcp.NewMCPManager(nil)
	// MCP servers will be initialized on first use to improve startup performance

	// Initialize circuit breaker
	agent.circuitBreaker = &CircuitBreakerState{
		Actions: make(map[string]*CircuitBreakerAction),
	}

	// Load command history from configuration
	agent.loadHistoryFromConfig()

	if persona := strings.TrimSpace(os.Getenv("LEDIT_PERSONA")); persona != "" {
		agent.activePersona = strings.ReplaceAll(strings.ToLower(persona), "-", "_")
	}

	return agent, nil
}

// GetDebugLogPath returns the path to the current debug log file (if any)
func (a *Agent) GetDebugLogPath() string { return a.debugLogPath }

// GetUnsafeMode returns whether unsafe mode is enabled
func (a *Agent) GetUnsafeMode() bool { return a.unsafeMode }

// SetUnsafeMode sets the unsafe mode flag
func (a *Agent) SetUnsafeMode(unsafe bool) { a.unsafeMode = unsafe }

// IsSecurityBypassApproved returns whether the user has approved filesystem access outside CWD
func (a *Agent) IsSecurityBypassApproved() bool {
	a.securityBypassMu.RLock()
	defer a.securityBypassMu.RUnlock()
	return a.securityBypassApproved
}

// SetSecurityBypassApproved marks that the user has approved filesystem access outside CWD for this session
func (a *Agent) SetSecurityBypassApproved() {
	a.securityBypassMu.Lock()
	defer a.securityBypassMu.Unlock()
	a.securityBypassApproved = true
}

// SetInterruptHandler sets the interrupt handler for UI mode
func (a *Agent) SetInterruptHandler(ch chan struct{}) {
	// Store the channel for external interrupt handling
	// Note: This is kept for backward compatibility
	// Interrupts are now primarily handled via context cancellation
}

// GetMessages returns the current conversation messages
func (a *Agent) GetMessages() []api.Message {
	return a.messages
}

// SetMessages sets the conversation messages (for restore)
func (a *Agent) SetMessages(messages []api.Message) {
	a.messages = messages
}

// AddMessage adds a single message to the conversation history
func (a *Agent) AddMessage(message api.Message) {
	a.messages = append(a.messages, message)
}

// GetTotalCost returns the total cost of the conversation
func (a *Agent) GetTotalCost() float64 {
	return a.totalCost
}

// GetTaskActions returns completed task actions
func (a *Agent) GetTaskActions() []TaskAction {
	return a.taskActions
}

// IsInteractiveMode returns true if running in interactive mode
func (a *Agent) IsInteractiveMode() bool {
	return os.Getenv("LEDIT_INTERACTIVE") == "1" ||
		os.Getenv("LEDIT_FROM_AGENT") != "1"
}

// GenerateResponse generates a simple response using the current model without tool calls
func (a *Agent) GenerateResponse(messages []api.Message) (string, error) {
	resp, err := a.client.SendChatRequest(messages, nil, "") // No tools, no reasoning
	if err != nil {
		return "", fmt.Errorf("failed to generate response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response generated")
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
func (a *Agent) OutputRouter() *OutputRouter { return a.outputRouter }

// PrintTerminalOnly writes text to the terminal without publishing to the event bus.
// Use this for output already published via a more specific event type.
func (a *Agent) PrintTerminalOnly(text string) {
	if a == nil || a.outputRouter == nil {
		// Fallback: just print
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Print(text)
		return
	}
	a.outputRouter.RouteTerminalOnly(text)
}

// GetSecurityApprovalMgr returns the security approval manager
func (a *Agent) GetSecurityApprovalMgr() *SecurityApprovalManager {
	return a.securityApprovalMgr
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

// SetTraceSessionsets the trace session for dataset collection
func (a *Agent) SetTraceSession(traceSession interface{}) {
	a.traceSession = traceSession
}
