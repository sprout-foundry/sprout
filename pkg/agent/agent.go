package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/ui"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	agent_config "github.com/alantheprice/ledit/pkg/agent_config"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/mcp"
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
	currentContextTokens int                            // Current context size being sent to model
	maxContextTokens     int                            // Model's maximum context window
	contextWarningIssued bool                           // Whether we've warned about approaching context limit
	shellCommandHistory  map[string]*ShellCommandResult // Track shell commands for deduplication
	changeTracker        *ChangeTracker                 // Track file changes for rollback support
	mcpManager           mcp.MCPManager                 // MCP server management

	// Interrupt handling
	interruptRequested   bool      // Flag indicating interrupt was requested
	interruptMessage     string    // User message to inject after interrupt
	escPressed           chan bool // Channel to signal Esc key press
	interruptChan        chan string // Channel for TUI interrupt messages
	escMonitoringEnabled bool      // Flag to enable/disable Esc monitoring
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
		finalModel = model
		// When a model is specified, use the best available provider
		// The provider should be explicitly set via command line --provider flag
		// or via interactive /provider selection before this point
		clientType, _, _ = configManager.GetBestProvider()
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

	agent := &Agent{
		client:               client,
		messages:             []api.Message{},
		systemPrompt:         systemPrompt,
		maxIterations:        100,
		totalCost:            0.0,
		clientType:           clientType,
		debug:                debug,
		optimizer:            NewConversationOptimizer(optimizationEnabled, debug),
		configManager:        configManager,
		shellCommandHistory:  make(map[string]*ShellCommandResult),
		interruptRequested:   false,
		interruptMessage:     "",
		escPressed:           make(chan bool, 1),
		interruptChan:        nil,
		escMonitoringEnabled: false, // Start disabled
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

	// Initialize MCP configuration and auto-start servers if configured
	if err := agent.initializeMCP(); err != nil {
		// Don't fail agent creation if MCP fails, just log warning
		if debug {
			fmt.Printf("⚠️  Warning: Failed to initialize MCP: %v\n", err)
		}
	}

	return agent, nil
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
	// Create a basic config structure for tools that need it
	cfg := &config.Config{
		SkipPrompt: !a.debug, // Don't prompt for API keys if not in debug mode
	}
	return cfg
}

func (a *Agent) GetTotalCost() float64 {
	return a.totalCost
}

func (a *Agent) GetCurrentIteration() int {
	return a.currentIteration
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
		// TUI mode: wait for response from channel with timeout
		select {
		case input := <-a.interruptChan:
			input = strings.TrimSpace(input)
			switch input {
			case "", "resume", "continue":
				ui.Log("▶️  Resuming current task...")
				return ""
			case "quit", "exit", "stop":
				ui.Log("🚪 Exiting...")
				ui.Log("=====================================")
				a.PrintConversationSummary(true)
				os.Exit(0)
				return "exit" // Unreachable, but for completeness
			default:
				ui.Logf("📝 Injecting new instruction: %s", input)
				ui.Log("▶️  Continuing with modified task...")
				return input
			}
		case <-time.After(30 * time.Second):
			ui.Log("⏰ Interrupt timeout - resuming task")
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

func (a *Agent) GetMaxIterations() int {
	return a.maxIterations
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
	cfg := &config.Config{} // Placeholder - should use actual config
	mcpConfig, err := mcp.LoadMCPConfig(cfg)
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

	return nil
}

// getMCPTools retrieves all available MCP tools and converts them to agent tool format
func (a *Agent) getMCPTools() []api.Tool {
	if a.mcpManager == nil {
		return nil
	}

	ctx := context.Background()
	mcpTools, err := a.mcpManager.GetAllTools(ctx)
	if err != nil {
		if a.debug {
			fmt.Printf("⚠️  Warning: Failed to get MCP tools: %v\n", err)
		}
		return nil
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
