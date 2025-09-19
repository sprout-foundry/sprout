package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	agent_config "github.com/alantheprice/ledit/pkg/agent_config"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/mcp"
	"github.com/alantheprice/ledit/pkg/utils"
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
	configManager        *agent_config.Manager          // Configuration management
	config               *config.Config                 // Main application configuration
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
	interruptRequested   bool        // Flag indicating interrupt was requested
	interruptMessage     string      // User message to inject after interrupt
	escPressed           chan bool   // Channel to signal Esc key press
	interruptChan        chan string // Channel for TUI interrupt messages
	escMonitoringEnabled bool        // Flag to enable/disable Esc monitoring

	// UI callback for real-time updates
	statsUpdateCallback func(totalTokens int, totalCost float64)

	// Output synchronization
	outputMutex *sync.Mutex

	// Hang detection
	hangDetector    *utils.HangDetector
	progressMonitor *utils.ProgressMonitor
	debugHangMode   bool

	// False stop detection
	falseStopDetectionEnabled bool

	// Streaming support
	streamingEnabled  bool
	streamingCallback api.StreamCallback
	streamingBuffer   strings.Builder
}

func NewAgent() (*Agent, error) {
	return NewAgentWithModel("")
}

func NewAgentWithModel(model string) (*Agent, error) {
	// Initialize configuration manager
	configManager, err := agent_config.NewManager()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize configuration: %w", err)
	}

	// Determine best provider and model
	var clientType api.ClientType
	var finalModel string

	if model != "" {
		// Check if model includes provider prefix (e.g., "deepinfra:model-name")
		parts := strings.SplitN(model, ":", 2)
		if len(parts) == 2 {
			// Provider explicitly specified
			providerName := parts[0]
			finalModel = parts[1]

			// Convert provider name to ClientType
			clientType, err = agent_config.GetProviderFromConfigName(providerName)
			if err != nil {
				return nil, fmt.Errorf("unknown provider '%s': %w", providerName, err)
			}
		} else {
			// No provider specified, use the model with best available provider
			finalModel = model
			clientType, _, _ = configManager.GetBestProvider()
		}
	} else {
		// Use configured provider and model
		clientType, finalModel, err = configManager.GetBestProvider()
		if err != nil {
			return nil, fmt.Errorf("no available providers: %w", err)
		}
	}

	// Create the client
	client, err := api.NewUnifiedClientWithModel(clientType, finalModel)
	if err != nil {
		return nil, fmt.Errorf("failed to create API client: %w", err)
	}

	// Save the selection for future use
	if err := configManager.SetProviderAndModel(clientType, finalModel); err != nil {
		// Log warning but don't fail - this is not critical
		fmt.Printf("⚠️  Warning: Failed to save provider selection: %v\n", err)
	}

	// Check if debug mode is enabled
	debug := os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG") == "1"
	debugHang := os.Getenv("LEDIT_DEBUG_HANG") == "true" || os.Getenv("LEDIT_DEBUG_HANG") == "1"

	// Set debug mode on the client
	client.SetDebug(debug)

	// Check connection
	if err := client.CheckConnection(); err != nil {
		return nil, fmt.Errorf("client connection check failed: %w", err)
	}

	// Use embedded system prompt
	systemPrompt := getEmbeddedSystemPrompt()

	// Clear old todos at session start
	tools.ClearTodos()

	// Conversation optimization is always enabled
	optimizationEnabled := true

	// Load the actual configuration
	cfg, err := config.LoadOrInitConfig(true)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

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
		config:                    cfg,
		shellCommandHistory:       make(map[string]*ShellCommandResult),
		interruptRequested:        false,
		interruptMessage:          "",
		escPressed:                make(chan bool, 1),
		interruptChan:             nil,
		escMonitoringEnabled:      false, // Start disabled
		falseStopDetectionEnabled: true,  // Enable by default
		conversationPruner:        NewConversationPruner(debug),
		debugHangMode:             debugHang,
	}

	// NOTE: Esc key monitoring removed - was interfering with Ctrl+C and terminal control
	// Will implement proper escape handling through readline library instead

	// Initialize context limits based on model
	agent.maxContextTokens = agent.getModelContextLimit()
	agent.currentContextTokens = 0
	agent.contextWarningIssued = false

	// Load previous conversation summary for continuity
	agent.loadPreviousSummary()

	// Initialize change tracker (will be activated when user starts making changes)
	agent.changeTracker = NewChangeTracker(agent, "")
	agent.changeTracker.Disable() // Start disabled, enable when user makes first request

	// Initialize MCP manager
	agent.mcpManager = mcp.NewMCPManager(nil) // nil logger for now

	// Initialize circuit breaker
	agent.circuitBreaker = &CircuitBreakerState{
		Actions: make(map[string]*CircuitBreakerAction),
	}

	// Initialize MCP configuration and auto-start servers if configured
	if err := agent.initializeMCP(); err != nil {
		// Don't fail agent creation if MCP fails, just log warning
		if debug {
			fmt.Printf("⚠️  Warning: Failed to initialize MCP: %v\n", err)
		}
	}

	// Initialize hang detection
	agent.initializeHangDetection()

	return agent, nil
}

// initializeHangDetection sets up hang detection monitoring
func (a *Agent) initializeHangDetection() {
	// Default timeout for operations (adjustable via env var)
	timeout := 5 * time.Minute // Default 5 minutes
	if timeoutStr := os.Getenv("LEDIT_HANG_TIMEOUT"); timeoutStr != "" {
		if parsedTimeout, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = parsedTimeout
		}
	}

	// Create hang detector for overall agent operations
	a.hangDetector = utils.NewHangDetector("Agent", timeout)

	// Create progress monitor for detailed operation tracking
	a.progressMonitor = utils.NewProgressMonitor("AgentProgress", timeout)

	// Set custom hang handler if in debug mode
	if a.debugHangMode {
		a.hangDetector.SetHangHandler(func(name string, duration time.Duration) {
			// Use mutex for synchronized output if available
			if a.outputMutex != nil {
				a.outputMutex.Lock()
				defer a.outputMutex.Unlock()
			}

			fmt.Printf("\n🚨 HANG DETECTED: No progress updates from %s for %v\n", name, duration)
			fmt.Printf("   This may indicate the operation is stuck or taking longer than expected.\n")
			fmt.Printf("   Use Ctrl+C to interrupt, or wait for the operation to complete.\n")
			fmt.Printf("   Check .ledit/workspace.log and latest .ledit/runlogs/ for details\n\n")
		})

		// Start monitoring immediately in debug mode
		a.hangDetector.Start()
		a.progressMonitor.Start()
	}
}

// CheckCircuitBreaker checks if an action should be blocked due to repetitive behavior
func (a *Agent) CheckCircuitBreaker(actionType, target string, threshold int) (bool, string) {
	key := fmt.Sprintf("%s:%s", actionType, target)
	action, exists := a.circuitBreaker.Actions[key]

	if !exists {
		// First time doing this action
		a.circuitBreaker.Actions[key] = &CircuitBreakerAction{
			ActionType: actionType,
			Target:     target,
			Count:      1,
			LastUsed:   time.Now().Unix(),
		}
		return false, ""
	}

	action.Count++
	action.LastUsed = time.Now().Unix()

	if action.Count >= threshold {
		warning := fmt.Sprintf("🛑 CIRCUIT BREAKER TRIGGERED: You've attempted '%s' on '%s' %d times. This suggests you may be stuck in a loop.\n\n"+
			"DEBUGGING SUGGESTIONS:\n"+
			"1. Read the error message carefully - what is it actually telling you?\n"+
			"2. Check if you're fixing the right thing - are you editing tests when you should fix source code?\n"+
			"3. Search the codebase for missing functions or dependencies\n"+
			"4. Step back and analyze the root cause instead of making random changes\n\n"+
			"Consider trying a different approach or asking for help.",
			actionType, target, action.Count)
		return true, warning
	}

	return false, ""
}

// ResetCircuitBreaker resets the circuit breaker for a specific action (for testing)
func (a *Agent) ResetCircuitBreaker(actionType, target string) {
	key := fmt.Sprintf("%s:%s", actionType, target)
	delete(a.circuitBreaker.Actions, key)
}

func getProjectContext() string {
	// Check for project context files in order of priority
	contextFiles := []string{
		".cursor/markdown/project.md",
		".cursor/markdown/context.md",
		".claude/project.md",
		".claude/context.md",
		".project_context.md",
		"PROJECT_CONTEXT.md",
	}

	for _, filePath := range contextFiles {
		content, err := tools.ReadFile(filePath)
		if err == nil && strings.TrimSpace(content) != "" {
			return fmt.Sprintf("PROJECT CONTEXT:\n%s", content)
		}
	}

	return ""
}

// Basic getter methods
func (a *Agent) GetConfigManager() *agent_config.Manager {
	return a.configManager
}

func (a *Agent) GetConfig() *config.Config {
	return a.config
}

func (a *Agent) GetTotalCost() float64 {
	return a.totalCost
}

func (a *Agent) GetTotalTokens() int {
	return a.totalTokens
}

func (a *Agent) GetCurrentIteration() int {
	return a.currentIteration
}

func (a *Agent) GetCurrentContextTokens() int {
	return a.currentContextTokens
}

// IsInteractiveMode checks if the agent is running in an interactive terminal
func (a *Agent) IsInteractiveMode() bool {
	// Check if stdin is a terminal
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func (a *Agent) GetMaxContextTokens() int {
	return a.maxContextTokens
}

// SetFalseStopDetection enables or disables false stop detection
func (a *Agent) SetFalseStopDetection(enabled bool) {
	a.falseStopDetectionEnabled = enabled
	if a.debug {
		if enabled {
			a.debugLog("🔍 False stop detection enabled\n")
		} else {
			a.debugLog("🔍 False stop detection disabled\n")
		}
	}
}

// SetStatsUpdateCallback sets a callback for real-time stats updates
func (a *Agent) SetStatsUpdateCallback(callback func(totalTokens int, totalCost float64)) {
	a.statsUpdateCallback = callback
}

// monitorEscKey - DISABLED: was interfering with Ctrl+C and terminal control
// This function was consuming all stdin input, preventing proper signal handling
// and blocking Ctrl+C from working. Need to implement escape handling differently.
func (a *Agent) monitorEscKey() {
	// DISABLED - do nothing to prevent stdin interference
	return
}

// CheckForInterrupt checks if an interrupt has been requested
func (a *Agent) CheckForInterrupt() bool {
	// Check TUI channel if available
	if a.interruptChan != nil {
		select {
		case <-a.interruptChan:
			return true
		default:
			return false
		}
	}

	// Fallback to old logic if enabled
	if a.escMonitoringEnabled {
		select {
		case <-a.escPressed:
			return true
		default:
			return false
		}
	}

	return a.interruptRequested
}

// EnableEscMonitoring - DISABLED: no-op to prevent Ctrl+C interference
func (a *Agent) EnableEscMonitoring() {
	// No-op - escape monitoring disabled
}

// DisableEscMonitoring - DISABLED: no-op to prevent Ctrl+C interference
func (a *Agent) DisableEscMonitoring() {
	// No-op - escape monitoring disabled
}

// HandleInterrupt processes an interrupt request and prompts for continuation
func (a *Agent) HandleInterrupt() string {
	a.interruptRequested = false
	a.DisableEscMonitoring()
	defer a.EnableEscMonitoring() // Re-enable when done

	if a.interruptChan != nil {
		// Console mode with interrupt channel
		select {
		case input := <-a.interruptChan:
			input = strings.TrimSpace(input)

			// Check if it's a command
			if strings.HasPrefix(input, "/") {
				cmd := strings.TrimPrefix(strings.Fields(input)[0], "/")
				switch cmd {
				case "exit", "quit", "q":
					fmt.Println("\n🚪 Exiting via interrupt...")
					fmt.Println("=====================================")
					a.PrintConversationSummary(true)
					os.Exit(0)
				case "stop":
					fmt.Println("\n🛑 Stopping current task...")
					return "STOP" // Special marker to indicate stopping
				default:
					// Other commands are treated as regular input
					fmt.Printf("\n📝 Processing your command: %s\n", input)
					return input
				}
			}

			// Regular input handling
			switch input {
			case "", "resume", "continue":
				fmt.Println("\n▶️  Resuming current task...")
				return ""
			case "quit", "exit", "stop":
				fmt.Println("\n🚪 Exiting...")
				fmt.Println("=====================================")
				a.PrintConversationSummary(true)
				os.Exit(0)
				return "exit" // Unreachable, but for completeness
			default:
				fmt.Printf("\n📝 Processing your input: %s\n", input)
				fmt.Println("▶️  Continuing with your additional context...")
				return input
			}
		case <-time.After(2 * time.Second):
			// Quick timeout to check for interrupts but continue if none
			return ""
		}
	} else {
		// Console fallback
		fmt.Println("\n🛑 Esc key pressed! Current task paused.")
		fmt.Println("💬 Enter instructions to modify or continue the current task:")
		fmt.Println("   (or press Enter to resume, 'quit' to exit)")
		fmt.Print(">>> ")

		var input string
		fmt.Scanln(&input)

		input = strings.TrimSpace(input)

		switch input {
		case "", "resume", "continue":
			fmt.Println("▶️  Resuming current task...")
			return ""
		case "quit", "exit", "stop":
			fmt.Println("🚪 Exiting...")
			fmt.Println("=====================================")
			a.PrintConversationSummary(true)
			os.Exit(0)
			return ""
		default:
			fmt.Printf("📝 Injecting new instruction: %s\n", input)
			fmt.Println("▶️  Continuing with modified task...")
			return input
		}
	}
}

// ClearInterrupt resets interrupt state
func (a *Agent) ClearInterrupt() {
	a.interruptRequested = false
	a.interruptMessage = ""
	// Drain any pending Esc signals
	select {
	case <-a.escPressed:
	default:
	}
	// Drain TUI channel if available
	if a.interruptChan != nil {
		select {
		case <-a.interruptChan:
		default:
		}
	}
}

// SetInterruptChannel sets the interrupt channel for external input
func (a *Agent) SetInterruptChannel(ch chan string) {
	a.interruptChan = ch
}

// SetOutputMutex sets the output mutex for synchronized output
func (a *Agent) SetOutputMutex(mu *sync.Mutex) {
	a.outputMutex = mu
}

// EnableStreaming enables streaming mode with optional callback
func (a *Agent) EnableStreaming(callback api.StreamCallback) {
	a.streamingEnabled = true
	a.streamingCallback = callback
}

// DisableStreaming disables streaming mode
func (a *Agent) DisableStreaming() {
	a.streamingEnabled = false
	a.streamingCallback = nil
}

func (a *Agent) GetMaxIterations() int {
	return a.maxIterations
}

func (a *Agent) SetMaxIterations(max int) {
	if max > 0 {
		a.maxIterations = max
	}
}

func (a *Agent) GetMessages() []api.Message {
	return a.messages
}

func (a *Agent) GetConversationHistory() []api.Message {
	return a.messages
}

func (a *Agent) GetLastAssistantMessage() string {
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].Role == "assistant" {
			return a.messages[i].Content
		}
	}
	return ""
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
	ctx := context.Background()

	// Load MCP configuration from the configuration system
	mcpConfig, err := mcp.LoadMCPConfig(a.config)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// Skip if MCP is disabled
	if !mcpConfig.Enabled {
		if a.debug {
			fmt.Println("🔧 MCP is disabled in configuration")
		}
		return nil
	}

	// Add configured servers
	for _, serverConfig := range mcpConfig.Servers {
		if err := a.mcpManager.AddServer(serverConfig); err != nil {
			if a.debug {
				fmt.Printf("⚠️  Warning: Failed to add MCP server %s: %v\n", serverConfig.Name, err)
			}
			continue
		}
	}

	// Auto-start servers if configured
	if mcpConfig.AutoStart {
		if err := a.mcpManager.StartAll(ctx); err != nil {
			return fmt.Errorf("failed to start MCP servers: %w", err)
		}

		if a.debug {
			servers := a.mcpManager.ListServers()
			runningCount := 0
			for _, server := range servers {
				if server.IsRunning() {
					runningCount++
				}
			}
			fmt.Printf("🚀 Started %d MCP servers\n", runningCount)
		}
	}

	// Generate MCP server summary for the system prompt
	a.generateMCPServerSummary()

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

		if a.debug {
			fmt.Printf("  - Loaded MCP tool: %s\n", agentTool.Function.Name)
		}
	}

	// Cache the tools for future use
	a.mcpToolsCache = agentTools

	return agentTools
}

// isValidMCPTool checks if the given tool name corresponds to a valid MCP tool
func (a *Agent) isValidMCPTool(toolName string) bool {
	if a.mcpManager == nil {
		return false
	}

	// Extract server and tool name from the MCP tool name format: mcp_<server>_<tool>
	if !strings.HasPrefix(toolName, "mcp_") {
		return false
	}

	parts := strings.SplitN(toolName[4:], "_", 2) // Remove "mcp_" prefix and split
	if len(parts) != 2 {
		return false
	}

	serverName := parts[0]
	originalToolName := parts[1]

	// Check if server exists and is running
	server, exists := a.mcpManager.GetServer(serverName)
	if !exists || !server.IsRunning() {
		return false
	}

	// Check if tool exists on the server
	ctx := context.Background()
	tools, err := server.ListTools(ctx)
	if err != nil {
		return false
	}

	for _, tool := range tools {
		if tool.Name == originalToolName {
			return true
		}
	}

	return false
}

// executeMCPTool executes an MCP tool by calling it through the manager
func (a *Agent) executeMCPTool(toolName string, args map[string]interface{}) (string, error) {
	if a.mcpManager == nil {
		return "", fmt.Errorf("MCP manager not initialized")
	}

	// Extract server and tool name from the MCP tool name format: mcp_<server>_<tool>
	if !strings.HasPrefix(toolName, "mcp_") {
		return "", fmt.Errorf("invalid MCP tool name format: %s", toolName)
	}

	parts := strings.SplitN(toolName[4:], "_", 2) // Remove "mcp_" prefix and split
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid MCP tool name format: %s", toolName)
	}

	serverName := parts[0]
	originalToolName := parts[1]

	if a.debug {
		fmt.Printf("🔧 Executing MCP tool: %s on server: %s with args: %v\n", originalToolName, serverName, args)
	}

	// Call the MCP tool
	ctx := context.Background()
	result, err := a.mcpManager.CallTool(ctx, serverName, originalToolName, args)
	if err != nil {
		return "", fmt.Errorf("MCP tool execution failed: %w", err)
	}

	// Convert result to string
	if result.IsError {
		return "", fmt.Errorf("MCP tool returned error: %v", result.Content)
	}

	// Combine all content pieces into a single response
	var response strings.Builder
	for i, content := range result.Content {
		if i > 0 {
			response.WriteString("\n")
		}
		if content.Type == "text" {
			response.WriteString(content.Text)
		} else if content.Data != "" {
			response.WriteString(content.Data)
		}
	}

	return response.String(), nil
}

// handleMCPToolsCommand handles the mcp_tools meta-tool for discovering and calling MCP tools
func (a *Agent) handleMCPToolsCommand(args map[string]interface{}) (string, error) {
	if a.mcpManager == nil {
		return "MCP is not configured or enabled. No MCP servers are available.", nil
	}

	action, ok := args["action"].(string)
	if !ok {
		return "", fmt.Errorf("action parameter is required. Use 'list' to see available tools or 'call' to execute a tool")
	}

	switch action {
	case "list":
		return a.listMCPTools(args)
	case "call":
		return a.callMCPToolViaMetaTool(args)
	default:
		return "", fmt.Errorf("unknown action: %s. Use 'list' or 'call'", action)
	}
}

// listMCPTools lists available MCP tools
func (a *Agent) listMCPTools(args map[string]interface{}) (string, error) {
	serverFilter, _ := args["server"].(string)
	ctx := context.Background()

	var result strings.Builder
	result.WriteString("Available MCP Tools:\n\n")

	servers := a.mcpManager.ListServers()
	toolCount := 0

	for _, server := range servers {
		serverName := server.GetName()

		// Skip if filtering by server name
		if serverFilter != "" && serverName != serverFilter {
			continue
		}

		if !server.IsRunning() {
			result.WriteString(fmt.Sprintf("Server '%s': Not running\n", serverName))
			continue
		}

		tools, err := server.ListTools(ctx)
		if err != nil {
			result.WriteString(fmt.Sprintf("Server '%s': Error listing tools: %v\n", serverName, err))
			continue
		}

		if len(tools) > 0 {
			result.WriteString(fmt.Sprintf("Server '%s':\n", serverName))
			for _, tool := range tools {
				toolCount++
				// Show the full MCP tool name that can be used
				mcpToolName := fmt.Sprintf("mcp_%s_%s", serverName, tool.Name)
				result.WriteString(fmt.Sprintf("  - %s: %s\n", mcpToolName, tool.Description))
			}
			result.WriteString("\n")
		}
	}

	if toolCount == 0 {
		if serverFilter != "" {
			result.WriteString(fmt.Sprintf("No tools found for server '%s'\n", serverFilter))
		} else {
			result.WriteString("No MCP tools currently available. Check if MCP servers are running.\n")
		}
	} else {
		result.WriteString(fmt.Sprintf("\nTotal tools available: %d\n", toolCount))
		result.WriteString("\nTo use a tool, call it directly or use: mcp_tools action='call' server='<server>' tool='<tool>' arguments={...}\n")
	}

	return result.String(), nil
}

// callMCPToolViaMetaTool executes an MCP tool via the mcp_tools meta-tool
func (a *Agent) callMCPToolViaMetaTool(args map[string]interface{}) (string, error) {
	server, ok := args["server"].(string)
	if !ok || server == "" {
		return "", fmt.Errorf("server parameter is required for 'call' action")
	}

	tool, ok := args["tool"].(string)
	if !ok || tool == "" {
		return "", fmt.Errorf("tool parameter is required for 'call' action")
	}

	toolArgs, _ := args["arguments"].(map[string]interface{})
	if toolArgs == nil {
		toolArgs = make(map[string]interface{})
	}

	// Call the tool through the manager
	ctx := context.Background()
	result, err := a.mcpManager.CallTool(ctx, server, tool, toolArgs)
	if err != nil {
		return "", fmt.Errorf("failed to call MCP tool: %w", err)
	}

	if result.IsError {
		return "", fmt.Errorf("MCP tool returned error: %v", result.Content)
	}

	// Combine all content pieces into a single response
	var response strings.Builder
	for i, content := range result.Content {
		if i > 0 {
			response.WriteString("\n")
		}
		if content.Type == "text" {
			response.WriteString(content.Text)
		} else if content.Data != "" {
			response.WriteString(content.Data)
		}
	}

	return response.String(), nil
}

// GetMCPManager returns the MCP manager (for tool access)
func (a *Agent) GetMCPManager() interface{} {
	return a.mcpManager
}

// mcpServerSummary holds the dynamic summary of available MCP servers
var mcpServerSummary string

// generateMCPServerSummary creates a summary of available MCP servers
func (a *Agent) generateMCPServerSummary() {
	if a.mcpManager == nil {
		return
	}

	servers := a.mcpManager.ListServers()
	if len(servers) == 0 {
		return
	}

	var summary strings.Builder
	summary.WriteString("\n\nMCP SERVERS AVAILABLE:\n")

	ctx := context.Background()
	serverCount := 0

	for _, server := range servers {
		if !server.IsRunning() {
			continue
		}

		serverName := server.GetName()
		serverCount++

		// Get a sample of tools to understand what the server provides
		tools, err := server.ListTools(ctx)
		if err != nil || len(tools) == 0 {
			summary.WriteString(fmt.Sprintf("- %s: MCP server (tools unavailable)\n", serverName))
			continue
		}

		// Analyze tool names to determine server purpose
		serverDesc := analyzeMCPServerPurpose(serverName, tools)
		summary.WriteString(fmt.Sprintf("- %s: %s (%d tools)\n", serverName, serverDesc, len(tools)))
	}

	if serverCount > 0 {
		summary.WriteString("\nUse the 'mcp_tools' tool with action='list' to see all available tools from these servers.")
		mcpServerSummary = summary.String()
	} else {
		mcpServerSummary = ""
	}
}

// analyzeMCPServerPurpose attempts to determine what a server does based on its name and tools
func analyzeMCPServerPurpose(serverName string, tools []mcp.MCPTool) string {
	// First check common server names
	lowerName := strings.ToLower(serverName)

	switch {
	case strings.Contains(lowerName, "github"):
		return "GitHub operations (PRs, issues, repos)"
	case strings.Contains(lowerName, "filesystem") || strings.Contains(lowerName, "fs"):
		return "Advanced file system operations"
	case strings.Contains(lowerName, "postgres") || strings.Contains(lowerName, "pg"):
		return "PostgreSQL database operations"
	case strings.Contains(lowerName, "slack"):
		return "Slack messaging and workspace operations"
	case strings.Contains(lowerName, "aws"):
		return "AWS cloud service operations"
	case strings.Contains(lowerName, "docker"):
		return "Docker container management"
	case strings.Contains(lowerName, "git") && !strings.Contains(lowerName, "github"):
		return "Git version control operations"
	}

	// If name doesn't give a clear hint, analyze tool names
	toolCategories := make(map[string]int)
	for _, tool := range tools {
		toolLower := strings.ToLower(tool.Name)

		// Categorize based on tool names
		switch {
		case strings.Contains(toolLower, "file") || strings.Contains(toolLower, "dir"):
			toolCategories["file"]++
		case strings.Contains(toolLower, "query") || strings.Contains(toolLower, "select") || strings.Contains(toolLower, "insert"):
			toolCategories["database"]++
		case strings.Contains(toolLower, "pr") || strings.Contains(toolLower, "issue") || strings.Contains(toolLower, "commit"):
			toolCategories["vcs"]++
		case strings.Contains(toolLower, "send") || strings.Contains(toolLower, "message") || strings.Contains(toolLower, "channel"):
			toolCategories["messaging"]++
		case strings.Contains(toolLower, "deploy") || strings.Contains(toolLower, "build") || strings.Contains(toolLower, "run"):
			toolCategories["devops"]++
		}
	}

	// Find dominant category
	maxCount := 0
	dominantCategory := ""
	for cat, count := range toolCategories {
		if count > maxCount {
			maxCount = count
			dominantCategory = cat
		}
	}

	// Return description based on dominant category
	switch dominantCategory {
	case "file":
		return "File and directory operations"
	case "database":
		return "Database operations"
	case "vcs":
		return "Version control operations"
	case "messaging":
		return "Messaging and communication"
	case "devops":
		return "DevOps and deployment operations"
	default:
		// Generic description with some sample tools
		if len(tools) > 0 {
			sampleTools := []string{}
			for i, tool := range tools {
				if i >= 3 {
					break
				}
				sampleTools = append(sampleTools, tool.Name)
			}
			return fmt.Sprintf("Various operations including %s", strings.Join(sampleTools, ", "))
		}
		return "Various operations"
	}
}
