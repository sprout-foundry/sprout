package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/mcp"
	"golang.org/x/term"
)

type Agent struct {
	client               api.ClientInterface
	messages             []api.Message
	systemPrompt         string
	maxIterations        int
	currentIteration     int
	totalCost            float64
	clientType           api.ClientType
	taskActions          []TaskAction                   // Track what was accomplished
	debug                bool                           // Enable debug logging
	totalTokens          int                            // Track total tokens used across all requests
	promptTokens         int                            // Track total prompt tokens
	completionTokens     int                            // Track total completion tokens
	cachedTokens         int                            // Track tokens that were cached/reused
	cachedCostSavings    float64                        // Track cost savings from cached tokens
	previousSummary      string                         // Summary of previous actions for continuity
	sessionID            string                         // Unique session identifier
	optimizer            *ConversationOptimizer         // Conversation optimization
	configManager        *configuration.Manager         // Configuration management
	currentContextTokens int                            // Current context size being sent to model
	maxContextTokens     int                            // Model's maximum context window
	contextWarningIssued bool                           // Whether we've warned about approaching context limit
	shellCommandHistory  map[string]*ShellCommandResult // Track shell commands for deduplication
	changeTracker        *ChangeTracker                 // Track file changes for rollback support
	mcpManager           mcp.MCPManager                 // MCP server management
	mcpToolsCache        []api.Tool                     // Cached MCP tools to avoid reloading
	circuitBreaker       *CircuitBreakerState           // Track repetitive actions
	conversationPruner   *ConversationPruner            // Automatic conversation pruning

	// Interrupt handling
	interruptRequested   bool            // Flag indicating interrupt was requested
	interruptMessage     string          // User message to inject after interrupt
	escPressed           chan bool       // Channel to signal Esc key press
	interruptChan        chan string     // Channel for TUI interrupt messages
	inputInjectionChan   chan string     // Channel for injecting new user inputs during processing
	escMonitoringEnabled bool            // Flag to enable/disable Esc monitoring
	outputMutex          *sync.Mutex     // Mutex for synchronized output
	streamingEnabled     bool            // Whether streaming is enabled
	streamingCallback    func(string)    // Custom streaming callback
	streamingBuffer      strings.Builder // Buffer for streaming content
	flushCallback        func()          // Callback to flush buffered output

	// Feature flags
	falseStopDetectionEnabled bool
	statsUpdateCallback       func(int, float64) // Callback for token/cost updates
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

	if model != "" {
		// Check if model includes provider prefix (e.g., "openai:gpt-4")
		parts := strings.SplitN(model, ":", 2)
		if len(parts) == 2 {
			// Provider explicitly specified
			providerName := parts[0]
			finalModel = parts[1]

			// Get ClientType for provider
			clientType, err = getClientTypeFromName(providerName)
			if err != nil {
				return nil, fmt.Errorf("unknown provider '%s': %w", providerName, err)
			}

			// Ensure provider has API key
			if err := configManager.EnsureAPIKey(clientType); err != nil {
				// Try to select a different provider
				clientType, err = configManager.SelectNewProvider()
				if err != nil {
					return nil, fmt.Errorf("failed to select provider: %w", err)
				}
				finalModel = configManager.GetModelForProvider(clientType)
			}
		} else {
			// No provider specified, use current provider with specified model
			clientType, err = configManager.GetProvider()
			if err != nil {
				// No provider set, select one
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
			clientType, err = configManager.SelectNewProvider()
			if err != nil {
				return nil, fmt.Errorf("failed to select provider: %w", err)
			}
		}

		// Ensure provider has API key
		if err := configManager.EnsureAPIKey(clientType); err != nil {
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
		fmt.Printf("⚠️  Warning: Failed to save provider selection: %v\n", err)
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

	// Check connection (skip in CI environments when testing)
	isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
	skipConnectionCheck := isCI && os.Getenv("LEDIT_SKIP_CONNECTION_CHECK") != ""

	if !skipConnectionCheck {
		if err := client.CheckConnection(); err != nil {
			return nil, fmt.Errorf("client connection check failed: %w", err)
		}
	}

	// Use embedded system prompt
	systemPrompt := getEmbeddedSystemPrompt()

	// Clear old todos at session start
	tools.ClearTodos()

	// Clean up old sessions (keep only most recent 20)
	cleanupMemorySessions()

	// Conversation optimization is always enabled
	optimizationEnabled := true

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
		interruptRequested:        false,
		interruptMessage:          "",
		escPressed:                make(chan bool, 1),
		interruptChan:             nil,
		inputInjectionChan:        make(chan string, 10), // Buffer up to 10 inputs
		escMonitoringEnabled:      false,
		falseStopDetectionEnabled: true,
		conversationPruner:        NewConversationPruner(debug),
	}

	// Initialize context limits based on model
	agent.maxContextTokens = agent.getModelContextLimit()
	agent.currentContextTokens = 0
	agent.contextWarningIssued = false

	// Initialize change tracker
	agent.changeTracker = NewChangeTracker(agent, "")
	agent.changeTracker.Disable() // Start disabled

	// Initialize MCP manager
	agent.mcpManager = mcp.NewMCPManager(nil)

	// Initialize circuit breaker
	agent.circuitBreaker = &CircuitBreakerState{
		Actions: make(map[string]*CircuitBreakerAction),
	}

	// Initialize MCP if config has it enabled
	if err := agent.initializeMCP(); err != nil {
		// Non-fatal - MCP is optional
		if debug {
			fmt.Printf("⚠️  MCP initialization skipped: %v\n", err)
		}
	}

	return agent, nil
}

// getClientTypeFromName converts provider name to ClientType
func getClientTypeFromName(name string) (api.ClientType, error) {
	switch name {
	case "openai":
		return api.OpenAIClientType, nil
	case "deepinfra":
		return api.DeepInfraClientType, nil
	case "openrouter":
		return api.OpenRouterClientType, nil
	case "ollama":
		return api.OllamaClientType, nil
	case "ollama-local":
		return api.OllamaLocalClientType, nil
	case "ollama-turbo":
		return api.OllamaTurboClientType, nil
	case "test":
		return api.TestClientType, nil
	// For providers not yet in ClientType constants
	case "anthropic", "gemini", "groq", "cerebras":
		return api.ClientType(name), nil
	default:
		return "", fmt.Errorf("unknown provider: %s", name)
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

// SetInterruptHandler sets the interrupt channel for TUI mode
func (a *Agent) SetInterruptHandler(ch chan string) {
	a.interruptChan = ch
}

// monitorEscKey monitors for Esc key press in a separate goroutine
func (a *Agent) monitorEscKey() {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return
	}

	// Get the current terminal state
	oldState, err := term.GetState(int(os.Stdin.Fd()))
	if err != nil {
		return
	}

	// Make sure to restore the terminal state when done
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Put terminal in raw mode to capture single key presses
	rawState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return
	}
	defer term.Restore(int(os.Stdin.Fd()), rawState)

	// Read single bytes
	buf := make([]byte, 1)
	for a.escMonitoringEnabled {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		// Check for ESC key (ASCII 27)
		if buf[0] == 27 {
			select {
			case a.escPressed <- true:
			default:
				// Channel is full, skip
			}
		}
	}
}

// EnableEscMonitoring starts monitoring for Esc key
func (a *Agent) EnableEscMonitoring() {
	// Note: Esc monitoring is currently disabled due to terminal conflicts
	// Will implement proper escape handling through readline library
	a.escMonitoringEnabled = false
}

// DisableEscMonitoring stops monitoring for Esc key
func (a *Agent) DisableEscMonitoring() {
	a.escMonitoringEnabled = false
}

// CheckForInterrupt checks if an interrupt was requested
func (a *Agent) CheckForInterrupt() bool {
	select {
	case <-a.escPressed:
		a.interruptRequested = true
		return true
	default:
		return a.interruptRequested
	}
}

// HandleInterrupt processes the interrupt request
func (a *Agent) HandleInterrupt() string {
	if !a.interruptRequested {
		return ""
	}

	// If we have an interrupt channel (TUI mode), use it
	if a.interruptChan != nil {
		select {
		case msg := <-a.interruptChan:
			return msg
		case <-time.After(100 * time.Millisecond):
			// No message, just stop
			return "STOP"
		}
	}

	// Otherwise prompt for input
	fmt.Println("\n🛑 Processing interrupted. Enter new instructions (or press Enter to stop):")

	// Read user input
	var input string
	fmt.Scanln(&input)

	if strings.TrimSpace(input) == "" {
		return "STOP"
	}

	return input
}

// ClearInterrupt resets the interrupt state
func (a *Agent) ClearInterrupt() {
	a.interruptRequested = false
	a.interruptMessage = ""
	// Drain the channel
	select {
	case <-a.escPressed:
	default:
	}
}

// GetMessages returns the current conversation messages
func (a *Agent) GetMessages() []api.Message {
	return a.messages
}

// SetMessages sets the conversation messages (for restore)
func (a *Agent) SetMessages(messages []api.Message) {
	a.messages = messages
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
			fmt.Printf("⚠️  Warning: MCP manager is nil\n")
		}
		return nil
	}

	// Return cached tools if available
	if a.mcpToolsCache != nil {
		if a.debug {
			fmt.Printf("🔧 Using cached MCP tools: %d\n", len(a.mcpToolsCache))
		}
		return a.mcpToolsCache
	}

	ctx := context.Background()
	mcpTools, err := a.mcpManager.GetAllTools(ctx)
	if err != nil {
		if a.debug {
			fmt.Printf("⚠️  Warning: Failed to get MCP tools: %v\n", err)
		}
		return nil
	}

	if a.debug {
		fmt.Printf("🔧 Loading %d MCP tools from manager (first time)\n", len(mcpTools))
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

// suggestCorrectToolName suggests a correct tool name based on common mistakes
func (a *Agent) suggestCorrectToolNameOLD(wrongName string) string {
	wrongLower := strings.ToLower(wrongName)

	// Common tool name mappings
	corrections := map[string]string{
		"bash":          "shell_command",
		"shell":         "shell_command",
		"run":           "shell_command",
		"execute":       "shell_command",
		"cmd":           "shell_command",
		"read":          "read_file",
		"cat":           "read_file",
		"open":          "read_file",
		"write":         "write_file",
		"save":          "write_file",
		"create":        "write_file",
		"edit":          "edit_file",
		"modify":        "edit_file",
		"update":        "edit_file",
		"search":        "search_files",
		"find":          "search_files",
		"grep":          "search_files",
		"list_files":    "shell_command",
		"ls":            "shell_command",
		"todo":          "add_todos",
		"task":          "add_todos",
		"add_todo":      "add_todos",
		"update_status": "update_todo_status",
		"list_tasks":    "list_todos",
		"web":           "web_search",
		"google":        "web_search",
		"search_web":    "web_search",
		"fetch":         "fetch_url",
		"get":           "fetch_url",
		"download":      "fetch_url",
		"analyze_ui":    "analyze_ui_screenshot",
		"screenshot":    "analyze_ui_screenshot",
		"analyze_image": "analyze_image_content",
		"image":         "analyze_image_content",
	}

	if suggestion, found := corrections[wrongLower]; found {
		return suggestion
	}

	// Check for partial matches
	for wrong, correct := range corrections {
		if strings.Contains(wrongLower, wrong) {
			return correct
		}
	}

	return ""
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

// SetInterruptChannel sets the interrupt channel for the agent
func (a *Agent) SetInterruptChannel(ch chan string) {
	a.interruptChan = ch
}

// InjectInput injects a new user input into the conversation flow
func (a *Agent) InjectInput(input string) {
	select {
	case a.inputInjectionChan <- input:
		// Successfully queued
	default:
		// Channel full, could log warning
		a.debugLog("⚠️ Input injection channel full, dropping input: %s\n", input)
	}
}

// GetInputInjectionChannel returns the input injection channel for monitoring
func (a *Agent) GetInputInjectionChannel() <-chan string {
	return a.inputInjectionChan
}

// SetOutputMutex sets the output mutex for synchronized output
func (a *Agent) SetOutputMutex(mutex *sync.Mutex) {
	a.outputMutex = mutex
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

// ensureStopInformation checks if the stop information is present and adds it if missing
func (a *Agent) ensureStopInformation(prompt string) string {
	// Check if the completion signal pattern is already present
	if strings.Contains(prompt, "[[TASK_COMPLETE]]") || strings.Contains(prompt, "TASK_COMPLETE") {
		return prompt
	}

	// Add the critical stop information
	stopInfo := `

## COMPLETION SIGNAL - CRITICAL FOR SYSTEM OPERATION

When you have fully completed the user's request and have no more actions to take, you MUST end your response with:
[[TASK_COMPLETE]]

**IMPORTANT**: This completion signal is REQUIRED to stop the conversation loop. Without it, the system will continue waiting for more actions.

Use [[TASK_COMPLETE]] when you have completed all requested work, provided the full answer, and have no more actions to perform.

**DO NOT provide blank or empty responses**. If you have nothing more to do, use [[TASK_COMPLETE]].`

	return prompt + stopInfo
}
