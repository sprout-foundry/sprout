package agent

import (
	"fmt"
	"log"
	"os"

	"github.com/alantheprice/ledit/pkg/agent/prompts/loader"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	agent_config "github.com/alantheprice/ledit/pkg/agent_config"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// getModularSystemPrompt generates a system prompt based on the user request
func getModularSystemPrompt(userRequest string) string {
	// Create the prompt assembler
	assembler, err := loader.DefaultAssembler()
	if err != nil {
		log.Printf("Warning: Failed to create modular prompt assembler: %v", err)
		log.Printf("Falling back to embedded system prompt")
		return getEmbeddedSystemPrompt()
	}

	// Assemble a prompt based on the user request
	prompt, classification, err := assembler.AssemblePromptForRequest(userRequest)
	if err != nil {
		log.Printf("Warning: Failed to assemble modular prompt: %v", err)
		log.Printf("Falling back to embedded system prompt")
		return getEmbeddedSystemPrompt()
	}

	// Log the classification for debugging (if debug mode is enabled)
	log.Printf("Modular prompt assembled: type=%s, complexity=%s, confidence=%.2f",
		classification.PrimaryType, classification.Complexity, classification.Confidence)

	// Add project context if available
	projectContext := getProjectContext()
	if projectContext != "" {
		prompt = prompt + "\n\n" + projectContext
	}

	return prompt
}

// NewAgentWithModularPrompts creates a new agent using the modular prompt system
func NewAgentWithModularPrompts(userRequest string) (*Agent, error) {
	// Initialize configuration manager
	configManager, err := agent_config.NewManager()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize configuration: %w", err)
	}

	// Use configured provider and model
	clientType, finalModel, err := configManager.GetBestProvider()
	if err != nil {
		return nil, fmt.Errorf("no available providers: %w", err)
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

	// Use modular system prompt based on user request
	systemPrompt := getModularSystemPrompt(userRequest)

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

	// Initialize context limits based on model
	agent.maxContextTokens = agent.getModelContextLimit()
	agent.currentContextTokens = 0
	agent.contextWarningIssued = false

	// Load previous conversation summary for continuity
	agent.loadPreviousSummary()

	// Initialize change tracker (will be activated when user starts making changes)
	agent.changeTracker = NewChangeTracker(agent, "")
	agent.changeTracker.Disable() // Start disabled, enable when user makes first request

	// Initialize circuit breaker state
	agent.circuitBreaker = &CircuitBreakerState{
		Actions: make(map[string]*CircuitBreakerAction),
	}

	return agent, nil
}

// IsModularPromptsEnabled checks if modular prompts should be used
func IsModularPromptsEnabled() bool {
	// For now, we can control this via environment variable or config
	// This allows for easy A/B testing and gradual rollout
	return os.Getenv("LEDIT_USE_MODULAR_PROMPTS") == "true"
}
