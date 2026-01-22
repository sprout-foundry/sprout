package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/mcp"
	"golang.org/x/term"
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
	mcpInitMu             sync.Mutex                    // Protect concurrent initialization
	circuitBreaker        *CircuitBreakerState           // Track repetitive actions
	conversationPruner    *ConversationPruner            // Automatic conversation pruning
	completionSummarizer  *CompletionContextSummarizer   // Completion context summarization
	toolCallGuidanceAdded bool                           // Prevent repeating tool call guidance

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

		// Initialize change tracker
		agent.changeTracker = NewChangeTracker(agent, "")
		agent.changeTracker.Enable() // Start enabled by default

		return agent, nil
	}

	if model != "" {
		// Check if model includes provider prefix (e.g., "openai:gpt-4")
		parts := strings.SplitN(model, ":", 2)
		if len(parts) == 2 {
			// Provider explicitly specified
			providerName := parts[0]
			finalModel = parts[1]

			// Get ClientType for provider using config manager to handle custom providers
			clientType, err = configManager.MapStringToClientType(providerName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "⚠️  Unknown provider '%s' specified. Falling back to available provider...\n", providerName)
				// Try to select a different provider
				clientType, err = configManager.SelectNewProvider()
				if err != nil {
					return nil, fmt.Errorf("failed to select provider after unknown provider '%s': %w", providerName, err)
				}
				finalModel = configManager.GetModelForProvider(clientType)
				fmt.Fprintf(os.Stderr, "✅ Using provider: %s with model: %s\n", api.GetProviderName(clientType), finalModel)
			} else {
				// Ensure provider has API key
				if err := configManager.EnsureAPIKey(clientType); err != nil {
					fmt.Fprintf(os.Stderr, "⚠️  Provider '%s' not configured. Falling back to available provider...\n", providerName)
					// Try to select a different provider
					clientType, err = configManager.SelectNewProvider()
					if err != nil {
						return nil, fmt.Errorf("failed to select provider after API key issue with '%s': %w", providerName, err)
					}
					finalModel = configManager.GetModelForProvider(clientType)
					fmt.Fprintf(os.Stderr, "✅ Using provider: %s with model: %s\n", api.GetProviderName(clientType), finalModel)
				}
			}
		} else {
			// No provider specified, use current provider with specified model
			clientType, err = configManager.GetProvider()
			if err != nil {
				// No provider set, select one
				fmt.Fprintf(os.Stderr, "🔧 No provider configured. Selecting available provider...\n")
				clientType, err = configManager.SelectNewProvider()
				if err != nil {
					return nil, fmt.Errorf("failed to select provider: %w", err)
				}
			}
			finalModel = model
		}
	} else {
		// Use configured provider and model
		clientType, err = configManager.GetProvider()
		if err != nil {
			// No provider set, select one
			fmt.Fprintf(os.Stderr, "🔧 No provider configured. Selecting available provider...\n")
			clientType, err = configManager.SelectNewProvider()
			if err != nil {
				return nil, fmt.Errorf("failed to select provider: %w", err)
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
		}

		finalModel = configManager.GetModelForProvider(clientType)
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
	tools.ClearTodos()

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

// SetStreamingEnabled enables or disables streaming responses
func (a *Agent) SetStreamingEnabled(enabled bool) {
	a.streamingEnabled = enabled
	if enabled && a.outputMutex == nil {
		a.outputMutex = &sync.Mutex{}
	}
}

// SetStreamingCallback sets a custom callback for streaming output
func (a *Agent) SetStreamingCallback(callback func(string)) {
	a.streamingCallback = callback
}

// SetDebug enables or disables debug mode
func (a *Agent) SetDebug(debug bool) {
	a.debug = debug
	if a.client != nil {
		a.client.SetDebug(debug)
	}
}

// debugLog prints debug messages if debug mode is enabled

// SetInterruptHandler sets the interrupt handler for UI mode
func (a *Agent) SetInterruptHandler(ch chan struct{}) {
	// Store the channel for external interrupt handling
	// Note: This is kept for backward compatibility
	// Interrupts are now primarily handled via context cancellation
}

// TriggerInterrupt manually triggers an interrupt for testing purposes
func (a *Agent) TriggerInterrupt() {
	if a.interruptCancel != nil {
		a.interruptCancel()
	}
}

// CheckForInterrupt checks if an interrupt was requested
func (a *Agent) CheckForInterrupt() bool {
	select {
	case <-a.interruptCtx.Done():
		// Context cancelled, interrupt requested
		return true
	default:
		return false
	}
}

// HandleInterrupt processes the interrupt request with pause/resume functionality
func (a *Agent) HandleInterrupt() string {
	if !a.CheckForInterrupt() {
		return ""
	}

	a.pauseMutex.Lock()
	defer a.pauseMutex.Unlock()

	// Initialize pause state if needed
	if a.pauseState == nil {
		a.pauseState = &PauseState{}
	}

	// Set pause state
	a.pauseState.IsPaused = true
	a.pauseState.PausedAt = time.Now()

	// Store current messages for context restoration
	a.pauseState.MessagesBefore = make([]api.Message, len(a.messages))
	copy(a.pauseState.MessagesBefore, a.messages)

	// Enhanced pause/resume interface
	fmt.Println("\n⏸️  Task interrupted! Choose an option:")
	fmt.Println("1. Add clarification and continue")
	fmt.Println("2. Stop current task")
	fmt.Println("3. Resume without changes")
	fmt.Println("4. Continue (default)")
	fmt.Print("Enter choice (1-4, or just press Enter for #4): ")

	// Use a simple input reader that doesn't conflict with UnifiedInputManager
	choice := a.readSimpleInput("4") // Default to continue

	switch strings.TrimSpace(choice) {
	case "1":
		fmt.Print("Enter clarification: ")
		clarification := a.readSimpleInput("")

		if strings.TrimSpace(clarification) != "" {
			// Add clarification to pause state
			a.pauseState.Clarifications = append(a.pauseState.Clarifications, clarification)

			// Add clarification as a system message for context
			clarificationMsg := fmt.Sprintf("USER CLARIFICATION DURING PAUSE: %s", clarification)
			a.messages = append(a.messages, api.Message{
				Role:    "user",
				Content: clarificationMsg,
			})

			// Reset interrupt and continue
			a.ClearInterrupt()
			a.pauseState.IsPaused = false
			return "CONTINUE_WITH_CLARIFICATION"
		}
		// Fall through to continue if no clarification

	case "2":
		a.pauseState.IsPaused = false
		return "STOP"

	case "3":
		// Just resume without changes
		a.ClearInterrupt()
		a.pauseState.IsPaused = false
		return "CONTINUE"

	case "4", "":
		// Default case - continue
		fmt.Println("Continuing...")
		a.ClearInterrupt()
		a.pauseState.IsPaused = false
		return "CONTINUE"

	default:
		fmt.Printf("Invalid choice '%s', continuing...\n", choice)
		a.ClearInterrupt()
		a.pauseState.IsPaused = false
		return "CONTINUE"
	}

	// This should never be reached, but add for safety
	return "CONTINUE"
}

// readSimpleInput reads a single line of input without interfering with UnifiedInputManager
func (a *Agent) readSimpleInput(defaultValue string) string {
	// Check if we're in a terminal to avoid issues with nested terminal mode
	if term.IsTerminal(int(os.Stdin.Fd())) {
		// For simple input during agent processing, we should avoid nested terminal raw mode
		// Just read a single line with buffered I/O instead of full terminal handling
		fmt.Print("> ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			if a.debug {
				fmt.Printf("Debug: readSimpleInput error: %v, using default: %s\n", err, defaultValue)
			}
			return defaultValue
		}
		result := strings.TrimSpace(input)
		if result == "" {
			result = defaultValue
		}
		return result
	}

	// Non-terminal fallback - use InputReader
	reader := console.NewInputReader("> ")
	input, err := reader.ReadLine()
	if err != nil {
		if a.debug {
			fmt.Printf("Debug: readSimpleInput error: %v, using default: %s\n", err, defaultValue)
		}
		return defaultValue
	}

	result := strings.TrimSpace(input)
	if result == "" {
		result = defaultValue
	}

	return result
}

// ClearInterrupt resets the interrupt state
func (a *Agent) ClearInterrupt() {
	// Create new interrupt context
	if a.interruptCancel != nil {
		a.interruptCancel()
	}
	interruptCtx, interruptCancel := context.WithCancel(context.Background())
	a.interruptCtx = interruptCtx
	a.interruptCancel = interruptCancel
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

// initializeMCP initializes MCP configuration and starts servers if needed
func (a *Agent) initializeMCP() error {
	config := a.configManager.GetConfig()
	if !config.MCP.Enabled {
		if a.debug {
			fmt.Println("🔧 MCP is disabled in configuration")
		}
		return nil
	}

	ctx := context.Background()

	// Add configured servers
	for name, serverConfig := range config.MCP.Servers {
		mcpServer := mcp.MCPServerConfig{
			Name:        serverConfig.Name,
			Command:     serverConfig.Command,
			Args:        serverConfig.Args,
			AutoStart:   serverConfig.AutoStart,
			MaxRestarts: serverConfig.MaxRestarts,
			Timeout:     serverConfig.Timeout,
			Env:         serverConfig.Env,
		}

		if err := a.mcpManager.AddServer(mcpServer); err != nil {
			if a.debug {
				fmt.Printf("⚠️  Warning: Failed to add MCP server %s: %v\n", name, err)
			}
			continue
		}
	}

	// Auto-start servers if configured
	if config.MCP.AutoStart {
		if err := a.mcpManager.StartAll(ctx); err != nil {
			return fmt.Errorf("failed to start MCP servers: %w", err)
		}

		if a.debug {
			tools, _ := a.mcpManager.GetAllTools(ctx)
			fmt.Printf("✅ MCP initialized with %d tools available\n", len(tools))
		}
	}

	// Auto-discover GitHub server if token is available
	if config.MCP.AutoDiscover {
		if githubToken := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); githubToken != "" {
			if _, exists := config.MCP.Servers["github"]; !exists {
				// Try npx version first
				githubServer := mcp.MCPServerConfig{
					Name:        "github",
					Command:     "npx",
					Args:        []string{"-y", "@modelcontextprotocol/server-github"},
					AutoStart:   true,
					MaxRestarts: 3,
					Timeout:     30 * time.Second,
					Env: map[string]string{
						"GITHUB_PERSONAL_ACCESS_TOKEN": githubToken,
					},
				}

				if err := a.mcpManager.AddServer(githubServer); err == nil {
					if config.MCP.AutoStart {
						if err := a.mcpManager.StartAll(ctx); err != nil {
							if a.debug {
								fmt.Printf("⚠️  Failed to start GitHub MCP server (npx): %v\n", err)
							}
						} else if a.debug {
							fmt.Println("✅ GitHub MCP server auto-discovered and started (npx)")
						}
					}
				}
			}
		}
	}

	return nil
}

// RefreshMCPTools refreshes the MCP tools cache
func (a *Agent) RefreshMCPTools() error {
	a.mcpToolsCache = nil // Clear cache to force reload
	tools := a.getMCPTools()
	if a.debug {
		fmt.Printf("🔧 Refreshed MCP tools: %d available\n", len(tools))
	}
	return nil
}

// getMCPTools retrieves all available MCP tools and converts them to agent tool format (with caching)
func (a *Agent) getMCPTools() []api.Tool {
	if a.mcpManager == nil {
		if a.debug {
			a.debugLog("⚠️  Warning: MCP manager is nil\n")
		}
		return nil
	}

	// Initialize MCP on first use (lazy loading for better startup performance)
	a.mcpInitMu.Lock()
	defer a.mcpInitMu.Unlock()

	if !a.mcpInitialized {
		if a.debug {
			a.debugLog("⚙️  Initializing MCP (first use)...\n")
		}
		if err := a.initializeMCP(); err != nil {
			// Non-fatal - MCP is optional
			a.mcpInitErr = err
			if a.debug {
				a.debugLog("⚠️  MCP initialization failed: %v\n", err)
			}
			// Don't set mcpInitialized to allow retry
			a.mcpInitialized = false
		} else {
			// Success - mark as initialized
			a.mcpInitialized = true
			a.mcpInitErr = nil
			if a.debug {
				a.debugLog("✅ MCP initialized\n")
			}
		}
	}

	// Return nil if not initialized
	if !a.mcpInitialized {
		return nil
	}

	// Return cached tools if available
	if a.mcpToolsCache != nil {
		if a.debug {
			a.debugLog("🔧 Using cached MCP tools: %d\n", len(a.mcpToolsCache))
		}
		return a.mcpToolsCache
	}

	ctx := context.Background()
	mcpTools, err := a.mcpManager.GetAllTools(ctx)
	if err != nil {
		if a.debug {
			a.debugLog("⚠️  Warning: Failed to get MCP tools: %v\n", err)
		}
		return nil
	}

	if a.debug {
		a.debugLog("🔧 Loading %d MCP tools from manager (first time)\n", len(mcpTools))
	}

	var agentTools []api.Tool
	for _, mcpTool := range mcpTools {
		// Create wrapper and convert to agent tool format
		wrapper := mcp.NewMCPToolWrapper(mcpTool, a.mcpManager)
		agentTool := wrapper.ToAgentTool()

		// Convert to api.Tool format
		apiTool := api.Tool{
			Type:     agentTool.Type,
			Function: agentTool.Function,
		}
		agentTools = append(agentTools, apiTool)
	}

	// Cache the tools
	a.mcpToolsCache = agentTools

	return agentTools
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

// isValidMCPTool checks if the tool name is a valid MCP tool
func (a *Agent) isValidMCPTool(toolName string) bool {
	if !strings.HasPrefix(toolName, "mcp_") {
		return false
	}

	// Get MCP tools
	mcpTools := a.getMCPTools()
	for _, tool := range mcpTools {
		if tool.Function.Name == toolName {
			return true
		}
	}

	return false
}

// executeMCPTool executes an MCP tool
func (a *Agent) executeMCPTool(toolName string, args map[string]interface{}) (string, error) {
	// Remove mcp_ prefix and parse server:tool format
	toolName = strings.TrimPrefix(toolName, "mcp_")
	parts := strings.SplitN(toolName, "_", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid MCP tool name format: %s", toolName)
	}

	serverName := parts[0]
	actualToolName := parts[1]

	ctx := context.Background()
	result, err := a.mcpManager.CallTool(ctx, serverName, actualToolName, args)
	if err != nil {
		return "", err
	}

	// Convert result to string
	return formatMCPResult(result), nil
}

// formatMCPResult formats an MCP result for display
func formatMCPResult(result *mcp.MCPToolCallResult) string {
	if result == nil {
		return "No result"
	}

	var output strings.Builder
	for _, content := range result.Content {
		switch content.Type {
		case "text":
			output.WriteString(content.Text)
			output.WriteString("\n")
		case "resource":
			output.WriteString(fmt.Sprintf("[Resource: %s]\n", content.Data))
		default:
			output.WriteString(fmt.Sprintf("[%s: %s]\n", content.Type, content.Text))
		}
	}

	return strings.TrimSpace(output.String())
}

// handleMCPToolsCommand handles the mcp_tools meta command
func (a *Agent) handleMCPToolsCommand(args map[string]interface{}) (string, error) {
	action, ok := args["action"].(string)
	if !ok {
		return "", fmt.Errorf("action parameter required")
	}

	ctx := context.Background()

	switch action {
	case "list":
		tools := a.getMCPTools()
		if len(tools) == 0 {
			return "No MCP tools available", nil
		}

		var output strings.Builder
		output.WriteString(fmt.Sprintf("Available MCP tools (%d):\n", len(tools)))
		for _, tool := range tools {
			output.WriteString(fmt.Sprintf("- %s: %s\n", tool.Function.Name, tool.Function.Description))
		}
		return output.String(), nil

	case "refresh":
		a.mcpToolsCache = nil
		tools := a.getMCPTools()
		return fmt.Sprintf("Refreshed MCP tools. %d tools available.", len(tools)), nil

	case "status":
		servers := a.mcpManager.ListServers()
		var output strings.Builder
		output.WriteString("MCP Server Status:\n")
		for _, server := range servers {
			status := "stopped"
			if server.IsRunning() {
				status = "running"
			}
			output.WriteString(fmt.Sprintf("- %s: %s\n", server.GetName(), status))
		}
		return output.String(), nil

	case "start":
		// For now, start all servers
		if err := a.mcpManager.StartAll(ctx); err != nil {
			return "", fmt.Errorf("failed to start servers: %w", err)
		}
		return "Started all MCP servers", nil

	case "stop":
		// For now, stop all servers
		if err := a.mcpManager.StopAll(ctx); err != nil {
			return "", fmt.Errorf("failed to stop servers: %w", err)
		}
		return "Stopped all MCP servers", nil

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

// InjectInputContext injects a new user input using context-based interrupt system
func (a *Agent) InjectInputContext(input string) error {
	a.inputInjectionMutex.Lock()
	defer a.inputInjectionMutex.Unlock()

	// Send the new input to the injection channel
	select {
	case a.inputInjectionChan <- input:
		return nil
	default:
		return fmt.Errorf("input injection channel full")
	}
}

// GetInputInjectionContext returns the input injection channel for the new system
func (a *Agent) GetInputInjectionContext() <-chan string {
	return a.inputInjectionChan
}

// ClearInputInjectionContext clears any pending input injections
func (a *Agent) ClearInputInjectionContext() {
	a.inputInjectionMutex.Lock()
	defer a.inputInjectionMutex.Unlock()

	// Drain the channel
	for {
		select {
		case <-a.inputInjectionChan:
			// Remove item
		default:
			// Channel empty
			return
		}
	}
}

// SetOutputMutex sets the output mutex for synchronized output
func (a *Agent) SetOutputMutex(mutex *sync.Mutex) {
	a.outputMutex = mutex
}

// IsInterrupted returns true if an interrupt has been requested
func (a *Agent) IsInterrupted() bool {
	return a.CheckForInterrupt()
}

// EnableStreaming enables response streaming with a callback
func (a *Agent) EnableStreaming(callback func(string)) {
	a.streamingEnabled = true
	a.streamingCallback = callback
}

// DisableStreaming disables response streaming
func (a *Agent) DisableStreaming() {
	a.streamingEnabled = false
	a.streamingCallback = nil
	a.flushCallback = nil
}

// SetFlushCallback sets a callback to flush buffered output
func (a *Agent) SetFlushCallback(callback func()) {
	a.flushCallback = callback
}

// GetTotalTokens returns the total tokens used across all requests
func (a *Agent) GetTotalTokens() int {
	return a.totalTokens
}

// GetCurrentIteration returns the current iteration number
func (a *Agent) GetCurrentIteration() int {
	return a.currentIteration
}

// GetCurrentContextTokens returns the current context token count
func (a *Agent) GetCurrentContextTokens() int {
	// Return the current request context tokens, not cumulative
	return a.currentContextTokens
}

// GetMaxContextTokens returns the maximum context tokens for the current model
func (a *Agent) GetMaxContextTokens() int {
	// Get context limit from the model
	return a.getModelContextLimit()
}

// GetConfigManager returns the configuration manager
func (a *Agent) GetConfigManager() *configuration.Manager {
	return a.configManager
}

// SetMaxIterations sets the maximum number of iterations for the agent
func (a *Agent) SetMaxIterations(max int) {
	a.maxIterations = max
}

// GetLastTPS returns the most recent TPS value from the provider
func (a *Agent) GetLastTPS() float64 {
	if a.client != nil {
		return a.client.GetLastTPS()
	}
	return 0.0
}

// GetPromptTokens returns the total prompt tokens used
func (a *Agent) GetPromptTokens() int {
	return a.promptTokens
}

// TrackMetricsFromResponse updates agent metrics from API response usage data
func (a *Agent) TrackMetricsFromResponse(promptTokens, completionTokens, totalTokens int, estimatedCost float64, cachedTokens int) {
	a.totalTokens += totalTokens
	a.promptTokens += promptTokens
	a.completionTokens += completionTokens
	a.totalCost += estimatedCost
	a.cachedTokens += cachedTokens

	// Calculate cost savings from cached tokens
	// Assuming cached tokens save approximately 90% of the cost (since they're reused)
	if cachedTokens > 0 {
		// Rough estimate: cached token value = tokens * average cost per token
		avgCostPerToken := 0.0
		if totalTokens > 0 && estimatedCost > 0 {
			avgCostPerToken = estimatedCost / float64(totalTokens)
		}
		a.cachedCostSavings += float64(cachedTokens) * avgCostPerToken * 0.9
	}

	// Trigger stats update callback if registered
	if a.statsUpdateCallback != nil {
		a.statsUpdateCallback(a.totalTokens, a.totalCost)
	}
}

// GetCompletionTokens returns the total completion tokens used
func (a *Agent) GetCompletionTokens() int {
	return a.completionTokens
}

// GetCachedTokens returns the total cached/reused tokens
func (a *Agent) GetCachedTokens() int {
	return a.cachedTokens
}

// GetCachedCostSavings returns the cost savings from cached tokens
func (a *Agent) GetCachedCostSavings() float64 {
	return a.cachedCostSavings
}

// GetContextWarningIssued returns whether a context warning has been issued
func (a *Agent) GetContextWarningIssued() bool {
	return a.contextWarningIssued
}

// GetMaxIterations returns the maximum iterations allowed
func (a *Agent) GetMaxIterations() int {
	return a.maxIterations
}

// IsStreamingEnabled returns whether streaming is enabled
func (a *Agent) IsStreamingEnabled() bool {
	return a.streamingEnabled
}

// IsDebugMode returns whether debug mode is enabled
func (a *Agent) IsDebugMode() bool {
	return a.debug
}

// GetCurrentTPS returns the current TPS value (alias for GetLastTPS)
func (a *Agent) GetCurrentTPS() float64 {
	return a.GetLastTPS()
}

// GetAverageTPS returns the average TPS across all requests
func (a *Agent) GetAverageTPS() float64 {
	if a.client != nil {
		return a.client.GetAverageTPS()
	}
	return 0.0
}

// GetTPSStats returns comprehensive TPS statistics
func (a *Agent) GetTPSStats() map[string]float64 {
	if a.client != nil {
		return a.client.GetTPSStats()
	}
	return map[string]float64{}
}

// SetSystemPrompt sets the system prompt for the agent
func (a *Agent) SetSystemPrompt(prompt string) {
	a.systemPrompt = a.ensureStopInformation(prompt)
}

// GetSystemPrompt returns the current system prompt
func (a *Agent) GetSystemPrompt() string {
	return a.systemPrompt
}

// SetSystemPromptFromFile loads a custom system prompt from a file
func (a *Agent) SetSystemPromptFromFile(filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read system prompt file: %w", err)
	}

	promptContent := strings.TrimSpace(string(content))
	if promptContent == "" {
		return fmt.Errorf("system prompt file is empty")
	}

	a.systemPrompt = a.ensureStopInformation(promptContent)
	return nil
}

// ensureStopInformation preserves the original prompt content
func (a *Agent) ensureStopInformation(prompt string) string {
	return prompt
}

// PublishStreamChunk publishes a streaming chunk for real-time updates
func (a *Agent) PublishStreamChunk(chunk string) {
	a.publishEvent(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk))
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
