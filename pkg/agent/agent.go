package agent

import (
	"context"
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

type Agent struct {
	client                api.ClientInterface
	messages              []api.Message
	systemPrompt          string
	baseSystemPrompt      string // Base prompt restored when persona is cleared
	maxIterations         int
	currentIteration      int
	totalCost             float64
	clientType            api.ClientType
	taskActions           []TaskAction                   // Track what was accomplished
	debug                 bool                           // Enable debug logging
	totalTokens           int                            // Track total tokens used across all requests
	promptTokens          int                            // Track total prompt tokens
	completionTokens      int                            // Track total completion tokens
	cachedTokens          int                            // Track tokens that were cached/reused
	cachedCostSavings     float64                        // Track cost savings from cached tokens
	previousSummary       string                         // Summary of previous actions for continuity
	sessionID             string                         // Unique session identifier
	optimizer             *ConversationOptimizer         // Conversation optimization
	configManager         *configuration.Manager         // Configuration management
	currentContextTokens  int                            // Current context size being sent to model
	maxContextTokens      int                            // Model's maximum context window
	contextWarningIssued  bool                           // Whether we've warned about approaching context limit
	shellCommandHistory   map[string]*ShellCommandResult // Track shell commands for deduplication
	changeTracker         *ChangeTracker                 // Track file changes for rollback support
	mcpManager            mcp.MCPManager                 // MCP server management
	mcpToolsCache         []api.Tool                     // Cached MCP tools to avoid reloading
	mcpInitialized        bool                           // Track whether MCP has been initialized
	mcpInitErr            error                          // Store initialization error
	mcpInitMu             sync.Mutex                     // Protect concurrent initialization
	circuitBreaker        *CircuitBreakerState           // Track repetitive actions
	conversationPruner    *ConversationPruner            // Automatic conversation pruning
	completionSummarizer  *CompletionContextSummarizer   // Completion context summarization
	toolCallGuidanceAdded bool                           // Prevent repeating tool call guidance
	activeSkills          []string                       // Currently activated skills (by ID)
	activePersona         string                         // Currently active persona ID (direct agent or subagent env)

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
	commandHistory  []string  // History of entered commands
	historyIndex    int       // Current position in history for navigation
	asyncOutputOnce sync.Once // Ensure async worker initializes once
	asyncBufferSize int       // Optional override for async output buffer (tests)

	// Pause/resume state management
	pauseState *PauseState // Current pause state and context
	pauseMutex sync.Mutex  // Mutex for pause state operations

	// Feature flags
	falseStopDetectionEnabled bool
	statsUpdateCallback       func(int, float64) // Callback for token/cost updates

	// UI integration
	ui UI // UI provider for dropdowns, etc.

	// Event system
	eventBus *events.EventBus // Event bus for real-time UI updates

	// Debug logging
	debugLogFile  *os.File   // File handle for debug logs
	debugLogPath  string     // Path to the debug log file
	debugLogMutex sync.Mutex // Mutex for safe writes to debug log

	// Unsafe mode - bypass most security checks
	unsafeMode bool // Allow operations without security prompting

}

// Shutdown attempts to gracefully stop background work and child processes
// (e.g., MCP servers), and releases resources. It is safe to call multiple times.
func (a *Agent) Shutdown() {
	if a == nil {
		return
	}

	// Save command history to configuration before shutdown
	a.saveHistoryToConfig()

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
	// Initialize configuration manager
	configManager, err := configuration.NewManager()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize configuration: %w", err)
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

		// Create agent with minimal initialization using test client
		agent := &Agent{
			client:                    client,
			messages:                  []api.Message{},
			systemPrompt:              systemPrompt,
			baseSystemPrompt:          systemPrompt,
			maxIterations:             1000,
			totalCost:                 0.0,
			clientType:                clientType,
			debug:                     os.Getenv("LEDIT_DEBUG") == "true" || os.Getenv("LEDIT_DEBUG") == "1" || os.Getenv("LEDIT_DEBUG") != "",
			optimizer:                 NewConversationOptimizer(true, false),
			configManager:             configManager,
			shellCommandHistory:       make(map[string]*ShellCommandResult),
			inputInjectionChan:        make(chan string, inputInjectionBufferSize),
			interruptCtx:              context.Background(),
			interruptCancel:           func() { /* no-op */ },
			falseStopDetectionEnabled: true,
			conversationPruner:        NewConversationPruner(false),
			activePersona:             "orchestrator",
		}

		// Load command history from configuration
		agent.loadHistoryFromConfig()

		// Initialize debug log file if debug enabled
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
		fmt.Fprintf(os.Stderr, "⚠️  Failed to resolve configured provider/model: %v\n", err)
		fmt.Fprintf(os.Stderr, "🔧 Selecting an available provider...\n")
		clientType, err = configManager.SelectNewProvider()
		if err != nil {
			return nil, fmt.Errorf("failed to select provider: %w", err)
		}
		finalModel = configManager.GetModelForProvider(clientType)
		if model != "" && !looksLikeProviderModelSpecifier(configManager, model) {
			finalModel = model
		}
	}

	// Ensure provider has API key
	if err := configManager.EnsureAPIKey(clientType); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Provider not configured. Selecting available provider...\n")
		// Try to select a different provider
		clientType, err = configManager.SelectNewProvider()
		if err != nil {
			return nil, fmt.Errorf("failed to select provider: %w", err)
		}
		if model != "" && !looksLikeProviderModelSpecifier(configManager, model) {
			finalModel = model
		} else {
			finalModel = configManager.GetModelForProvider(clientType)
		}
	}

	// Create the client
	client, err := factory.CreateProviderClient(clientType, finalModel)
	if err != nil {
		return nil, fmt.Errorf("failed to create API client: %w", err)
	}

	// Save the selection
	if err := configManager.SetProvider(clientType); err != nil {
		fmt.Printf("Warning: Failed to save provider selection: %v\n", err)
	}
	if finalModel != "" && finalModel != configManager.GetModelForProvider(clientType) {
		if err := configManager.SetModelForProvider(clientType, finalModel); err != nil {
			fmt.Printf("⚠️  Warning: Failed to save model selection: %v\n", err)
		}
	}

	// Check if debug mode is enabled
	debug := os.Getenv("LEDIT_DEBUG") == "true" || os.Getenv("LEDIT_DEBUG") == "1" || os.Getenv("LEDIT_DEBUG") != ""

	// Set debug mode on the client
	client.SetDebug(debug)

	// Check connection (allow tests to skip by setting LEDIT_SKIP_CONNECTION_CHECK)
	// Also skip for providers where a fast/reliable connectivity probe is not available (e.g., Z.AI Coding Plan).
	skipConnectionCheck := os.Getenv("LEDIT_SKIP_CONNECTION_CHECK") != "" || clientType == api.ZAIClientType

	if !skipConnectionCheck {
		if err := client.CheckConnection(); err != nil {
			return nil, fmt.Errorf("client connection check failed: %w", err)
		}
	} else if debug {
		fmt.Printf("⚠️  Skipping provider connection check for %s\n", api.GetProviderName(clientType))
	}

	// Use embedded system prompt with provider-specific enhancements
	providerName := api.GetProviderName(clientType)
	systemPrompt, err := GetEmbeddedSystemPromptWithProvider(providerName)
	if err != nil {
		return nil, fmt.Errorf("failed to load system prompt: %w", err)
	}

	// Clear old todos at session start
	tools.TodoWrite([]tools.TodoItem{})

	// Clean up old sessions (keep only most recent 20)
	cleanupMemorySessions()

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
		maxIterations:             1000,
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
		completionSummarizer:      NewCompletionContextSummarizer(debug),
		commandHistory:            []string{},
		historyIndex:              -1,
		activePersona:             "orchestrator",
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
		fmt.Printf("⚙️  Pre-initializing tool registry...\n")
	}
	InitializeToolRegistry()
	if debug {
		fmt.Printf("✓ Tool registry initialized\n")
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

// initDebugLogger creates a temporary file for debug logs and writes a session header
func (a *Agent) initDebugLogger() error {
	// Create temp file
	f, err := os.CreateTemp("", "ledit-debug-*.log")
	if err != nil {
		return err
	}
	a.debugLogFile = f
	a.debugLogPath = f.Name()

	// Write header
	header := fmt.Sprintf("==== Ledit Debug Log ====%sSession start: %s\nProvider: %s\nModel: %s\nPID: %d\n========================\n",
		"\n",
		time.Now().Format(time.RFC3339),
		a.GetProvider(), a.GetModel(), os.Getpid(),
	)
	a.debugLogMutex.Lock()
	defer a.debugLogMutex.Unlock()
	if _, err := a.debugLogFile.WriteString(header); err != nil {
		return err
	}
	return nil
}

// GetDebugLogPath returns the path to the current debug log file (if any)
func (a *Agent) GetDebugLogPath() string { return a.debugLogPath }

// GetUnsafeMode returns whether unsafe mode is enabled
func (a *Agent) GetUnsafeMode() bool { return a.unsafeMode }

// SetUnsafeMode sets the unsafe mode flag
func (a *Agent) SetUnsafeMode(unsafe bool) { a.unsafeMode = unsafe }

// getClientTypeFromName converts provider name to ClientType
// DEPRECATED: Use configManager.MapStringToClientType instead to handle custom providers
func getClientTypeFromName(name string) (api.ClientType, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "chutes":
		return api.ChutesClientType, nil
	case "openai":
		return api.OpenAIClientType, nil
	case "deepinfra":
		return api.DeepInfraClientType, nil
	case "deepseek":
		return api.DeepSeekClientType, nil
	case "openrouter":
		return api.OpenRouterClientType, nil
	case "zai":
		return api.ZAIClientType, nil
	case "ollama":
		return api.OllamaClientType, nil
	case "ollama-local":
		return api.OllamaLocalClientType, nil
	case "ollama-turbo":
		return api.OllamaTurboClientType, nil
	case "test":
		return api.TestClientType, nil
	// For providers not yet in ClientType constants
	case "anthropic", "gemini", "groq", "cerebras", "claude", "cohere", "mistral", "lmstudio":
		return api.ClientType(name), nil
	default:
		// Return error for unknown provider, but allow graceful fallback
		return "", fmt.Errorf("unknown provider: %s (known providers: chutes, openai, deepinfra, openrouter, zai, ollama, ollama-local, ollama-turbo, anthropic, gemini, groq, cerebras, lmstudio)", name)
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

// SetEventBus sets the event bus for real-time UI updates
func (a *Agent) SetEventBus(eventBus *events.EventBus) {
	a.eventBus = eventBus
}

// GetEventBus returns the current event bus
func (a *Agent) GetEventBus() *events.EventBus {
	return a.eventBus
}

// publishEvent publishes an event to the event bus if available
func (a *Agent) publishEvent(eventType string, data interface{}) {
	if a.eventBus != nil {
		a.eventBus.Publish(eventType, data)
	}
}

// SelectProvider allows interactive provider selection
func (a *Agent) SelectProvider() error {
	newProvider, err := a.configManager.SelectNewProvider()
	if err != nil {
		return err
	}

	// Update agent's client type
	a.clientType = newProvider

	// Recreate client with new provider
	model := a.configManager.GetModelForProvider(newProvider)
	client, err := factory.CreateProviderClient(newProvider, model)
	if err != nil {
		return fmt.Errorf("failed to create client for %s: %w", newProvider, err)
	}

	a.client = client
	a.client.SetDebug(a.debug)

	return nil
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

// SetSystemPromptFromFile loads a custom system prompt from a file
func (a *Agent) SetSystemPromptFromFile(filePath string) error {
	resolvedPath, err := resolvePromptPath(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve system prompt file: %w", err)
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			embeddedContent, embeddedErr := readEmbeddedPromptFile(filePath)
			if embeddedErr != nil {
				return fmt.Errorf("failed to read system prompt file: %w", err)
			}
			content = embeddedContent
		} else {
			return fmt.Errorf("failed to read system prompt file: %w", err)
		}
	}

	promptContent := strings.TrimSpace(string(content))
	if promptContent == "" {
		return fmt.Errorf("system prompt file is empty")
	}

	a.systemPrompt = a.ensureStopInformation(promptContent)
	return nil
}

func resolvePromptPath(filePath string) (string, error) {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return "", fmt.Errorf("path is empty")
	}

	// Preserve existing behavior first: relative paths resolve from cwd.
	if _, err := os.Stat(trimmed); err == nil {
		return trimmed, nil
	}

	if filepath.IsAbs(trimmed) {
		return trimmed, nil
	}

	// Fallback for repo-relative prompt paths like pkg/agent/prompts/... when cwd is nested.
	repoRoot, err := findRepoRootFromCWD()
	if err != nil {
		return trimmed, nil
	}
	candidate := filepath.Join(repoRoot, trimmed)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return trimmed, nil
}

func findRepoRootFromCWD() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from cwd")
		}
		dir = parent
	}
}

func looksLikeProviderModelSpecifier(configManager *configuration.Manager, model string) bool {
	parts := strings.SplitN(strings.TrimSpace(model), ":", 2)
	if len(parts) != 2 {
		return false
	}
	if strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return false
	}
	if _, err := configManager.MapStringToClientType(parts[0]); err != nil {
		return false
	}
	return true
}

// ensureStopInformation preserves the original prompt content
func (a *Agent) ensureStopInformation(prompt string) string {
	return prompt
}

// PublishQueryProgress publishes query progress for real-time updates
func (a *Agent) PublishQueryProgress(message string, iteration int, tokensUsed int) {
	a.publishEvent(events.EventTypeQueryProgress, events.QueryProgressEvent(message, iteration, tokensUsed))
}

// PublishToolExecution publishes tool execution events for real-time updates
func (a *Agent) PublishToolExecution(toolName, action string, details map[string]interface{}) {
	a.publishEvent(events.EventTypeToolExecution, events.ToolExecutionEvent(toolName, action, details))
}

// AddToHistory adds a command to the history buffer
func (a *Agent) AddToHistory(command string) {
	// Don't add empty commands or duplicates of the last command
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}

	// Remove from history if it already exists (to avoid duplicates)
	for i, cmd := range a.commandHistory {
		if cmd == command {
			a.commandHistory = append(a.commandHistory[:i], a.commandHistory[i+1:]...)
			break
		}
	}

	// Add to history
	a.commandHistory = append(a.commandHistory, command)

	// Limit history size
	if len(a.commandHistory) > 100 {
		a.commandHistory = a.commandHistory[1:]
	}

	// Reset history index to end
	a.historyIndex = -1

	// Save history to configuration for persistence
	a.saveHistoryToConfig()
}

// GetHistoryCommand returns the command at the given index from history
func (a *Agent) GetHistoryCommand(index int) string {
	if index < 0 || index >= len(a.commandHistory) {
		return ""
	}
	return a.commandHistory[index]
}

// NavigateHistory navigates through command history
// direction: 1 for up (older), -1 for down (newer)
// currentIndex: current position in the input line
func (a *Agent) NavigateHistory(direction int, currentIndex int) (string, int) {
	if len(a.commandHistory) == 0 {
		return "", currentIndex
	}

	switch direction {
	case 1: // Up arrow - go to older commands
		if a.historyIndex == -1 {
			// Starting from current input, go to last command
			a.historyIndex = len(a.commandHistory) - 1
		} else if a.historyIndex > 0 {
			// Go to older command
			a.historyIndex--
		}
	case -1: // Down arrow - go to newer commands
		if a.historyIndex == -1 {
			// Already at newest, return empty
			return "", currentIndex
		} else if a.historyIndex < len(a.commandHistory)-1 {
			// Go to newer command
			a.historyIndex++
		} else {
			// At the newest command, reset to current input
			a.historyIndex = -1
			return "", currentIndex
		}
	}

	if a.historyIndex == -1 {
		return "", currentIndex
	}

	return a.commandHistory[a.historyIndex], currentIndex
}

// ResetHistoryIndex resets the history navigation index
func (a *Agent) ResetHistoryIndex() {
	a.historyIndex = -1
}

// GetHistorySize returns the number of commands in history
func (a *Agent) GetHistorySize() int {
	return len(a.commandHistory)
}

// GetHistory returns the command history
func (a *Agent) GetHistory() []string {
	return a.commandHistory
}

// loadHistoryFromConfig loads command history from the configuration
func (a *Agent) loadHistoryFromConfig() {
	if a.configManager == nil {
		return
	}

	config := a.configManager.GetConfig()
	if config != nil && len(config.CommandHistory) > 0 {
		a.commandHistory = config.CommandHistory
		a.historyIndex = config.HistoryIndex
		// Reset history index to -1 for new session navigation
		a.historyIndex = -1
	}
}

// saveHistoryToConfig saves command history to the configuration
func (a *Agent) saveHistoryToConfig() {
	if a.configManager == nil {
		return
	}

	config := a.configManager.GetConfig()
	if config != nil {
		config.CommandHistory = a.commandHistory
		config.HistoryIndex = a.historyIndex
		// Save configuration
		if err := config.Save(); err != nil && a.debug {
			a.debugLog("Failed to save command history to config: %v\n", err)
		}
	}
}
