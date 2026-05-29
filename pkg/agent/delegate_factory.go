package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/factory"
)

// CreateDelegateAgent creates a child agent for the delegate tool system.
// It inherits provider/model from the parent agent unless overridden in cfg.
func CreateDelegateAgent(parent *Agent, cfg DelegateConfig) (*Agent, error) {
	if parent == nil {
		return nil, fmt.Errorf("parent agent is required")
	}
	if parent.configManager == nil {
		return nil, fmt.Errorf("config manager is required")
	}

	// 1. Check nesting depth
	newDepth := parent.delegateDepth + 1
	maxDepth := getMaxDelegateNestingDepth()
	if newDepth > maxDepth {
		return nil, fmt.Errorf("delegate nesting depth %d exceeds maximum %d", newDepth, maxDepth)
	}

	// 2. Resolve provider/model: use cfg overrides, then parent agent, then config defaults
	provider := cfg.Provider
	model := cfg.Model

	if provider == "" {
		parentProvider := parent.GetProvider()
		if parentProvider != "" && parentProvider != "unknown" {
			provider = parentProvider
		}
	}
	if model == "" {
		parentModel := parent.GetModel()
		if parentModel != "" && parentModel != "unknown" {
			model = parentModel
		}
	}

	// Resolve client type from config
	clientType, finalModel, err := parent.configManager.ResolveProviderModel(provider, model)
	if err != nil {
		return nil, fmt.Errorf("resolve provider/model: %w", err)
	}

	// Create client via factory
	client, err := factory.CreateProviderClient(clientType, finalModel)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	// 3. Build system prompt
	systemPrompt := buildDelegateSystemPrompt(cfg)

	// 4. Create interrupt context
	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	// 5. Create sub-managers
	stateMgr := NewAgentStateManager(false)
	outputMgr := NewAgentOutputManager()
	securityMgr := NewAgentSecurityManager()
	mcpMgr := NewAgentMCPManager()

	// 6. Construct the agent struct (follows subagent_runner.go pattern)
	agent := &Agent{
		client:              client,
		systemPrompt:        systemPrompt,
		baseSystemPrompt:    systemPrompt,
		maxIterations:       cfg.MaxIterations, // use cfg value (0 = unlimited)
		clientType:          clientType,
		debug:               parent.debug,
		configManager:       parent.configManager,
		shellCommandHistory: make(map[string]*ShellCommandResult),
		inputInjectionChan:  make(chan string, inputInjectionBufferSize),
		interruptCtx:        interruptCtx,
		interruptCancel:     interruptCancel,
		workspaceRoot:       parent.workspaceRoot,
		state:               stateMgr,
		output:              outputMgr,
		security:            securityMgr,
		mcpSub:              mcpMgr,
		// Shared resources
		todoMgr:       parent.todoMgr,
		eventBus:      parent.eventBus,
		embeddingMgr:  parent.GetEmbeddingManager(),
	}

	// Inherit the parent's TerminalManager
	if tm := parent.GetTerminalManager(); tm != nil {
		agent.terminalManager = tm
	}

	// 7. Set delegate depth
	agent.delegateDepth = newDepth

	// 8. Propagate rootPersonaID from parent
	if parent.rootPersonaID != "" {
		agent.rootPersonaID = parent.rootPersonaID
	}

	// 9. Set delegate ID (unique identifier for this delegate agent)
	agent.delegateID = fmt.Sprintf("delegate-%d-%d", parent.delegateDepth+1, time.Now().UnixNano())

	// 10. Share the parent's clarification manager so delegate can request clarification
	if parent.clarificationManager != nil {
		agent.clarificationManager = parent.clarificationManager
	}

	// 11. Restrict tools if specified in cfg.Tools
	if len(cfg.Tools) > 0 {
		restrictTools(agent, cfg.Tools)
	}

	return agent, nil
}

// buildDelegateSystemPrompt constructs a system prompt for the delegate agent
func buildDelegateSystemPrompt(cfg DelegateConfig) string {
	parts := []string{}

	if cfg.Role != "" {
		parts = append(parts, fmt.Sprintf("You are a delegated agent with the role: %s.", cfg.Role))
	} else {
		parts = append(parts, "You are a delegated agent assisting with a specific task.")
	}

	if cfg.Context != "" {
		parts = append(parts, fmt.Sprintf("\nContext from parent agent:\n%s", cfg.Context))
	}

	if cfg.Prompt != "" {
		parts = append(parts, fmt.Sprintf("\nTask: %s", cfg.Prompt))
	}

	if len(cfg.Files) > 0 {
		parts = append(parts, fmt.Sprintf("\nRelevant files: %s", strings.Join(cfg.Files, ", ")))
	}

	parts = append(parts, "\nComplete the task and provide a clear summary of your work.")

	return strings.Join(parts, "")
}

// restrictTools limits the child agent's available tools to the specified set
func restrictTools(child *Agent, tools []string) {
	// The tool registry is global (singleton), so we can't modify it per-agent.
	// Instead, we store the allowed tools on the child agent for the tool handler
	// to check when executing tool calls. This is a safety measure.
	child.allowedTools = make(map[string]bool)
	for _, t := range tools {
		child.allowedTools[strings.ToLower(t)] = true
	}
}
