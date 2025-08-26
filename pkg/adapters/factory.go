package adapters

import (
	"context"
	"fmt"

	"github.com/alantheprice/ledit/internal/domain/agent"
	"github.com/alantheprice/ledit/internal/domain/todo"
	configAdapter "github.com/alantheprice/ledit/pkg/adapters/config"
	llmAdapter "github.com/alantheprice/ledit/pkg/adapters/llm"
	workspaceAdapter "github.com/alantheprice/ledit/pkg/adapters/workspace"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/llm"
)

// AdapterFactory creates and manages adapters between different layers
type AdapterFactory struct {
	config *config.Config
}

// NewAdapterFactory creates a new adapter factory
func NewAdapterFactory(cfg *config.Config) (*AdapterFactory, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	return &AdapterFactory{
		config: cfg,
	}, nil
}

// CreateLLMAdapters creates LLM adapters for different domains
type LLMAdapters struct {
	Legacy llm.LLMProvider   // For existing code
	Agent  agent.LLMProvider // For agent domain
	Todo   todo.LLMProvider  // For todo domain
}

// CreateLLMAdapters creates all LLM adapters from a new interfaces.LLMProvider
func (f *AdapterFactory) CreateLLMAdapters(provider interfaces.LLMProvider) *LLMAdapters {
	return &LLMAdapters{
		Legacy: llmAdapter.NewLLMAdapter(provider),
		Agent:  llmAdapter.NewDomainLLMAdapter(provider),
		Todo:   llmAdapter.NewTodoLLMAdapter(provider),
	}
}

// CreateWorkspaceAdapters creates workspace adapters for different domains
type WorkspaceAdapters struct {
	Agent      agent.WorkspaceProvider                // For agent domain
	Todo       *workspaceAdapter.TodoWorkspaceAdapter // For todo domain
	Interfaces interfaces.WorkspaceProvider           // For interfaces layer
}

// CreateWorkspaceAdapters creates all workspace adapters
func (f *AdapterFactory) CreateWorkspaceAdapters() *WorkspaceAdapters {
	baseAdapter := workspaceAdapter.NewWorkspaceAdapter()

	return &WorkspaceAdapters{
		Agent:      baseAdapter,
		Todo:       workspaceAdapter.NewTodoWorkspaceAdapter(baseAdapter),
		Interfaces: workspaceAdapter.NewSimpleWorkspaceProvider(),
	}
}

// CreateConfigAdapters creates configuration adapters
func (f *AdapterFactory) CreateConfigAdapters() interfaces.ConfigProvider {
	return configAdapter.NewConfigAdapter(f.config)
}

// AdapterBundle contains all adapters for easy dependency injection
type AdapterBundle struct {
	LLM       *LLMAdapters
	Workspace *WorkspaceAdapters
	Config    interfaces.ConfigProvider
}

// CreateAdapterBundle creates a complete set of adapters
func (f *AdapterFactory) CreateAdapterBundle(llmProvider interfaces.LLMProvider) *AdapterBundle {
	return &AdapterBundle{
		LLM:       f.CreateLLMAdapters(llmProvider),
		Workspace: f.CreateWorkspaceAdapters(),
		Config:    f.CreateConfigAdapters(),
	}
}

// WireServices wires adapters with domain services
func (f *AdapterFactory) WireServices(bundle *AdapterBundle) (*DomainServices, error) {
	// Create domain services with adapted dependencies
	todoService := todo.NewTodoService(bundle.LLM.Todo)

	// Create a simple event bus implementation (placeholder)
	eventBus := &SimpleEventBus{}

	// Create agent workflow
	agentWorkflow := agent.NewAgentWorkflow(
		bundle.LLM.Agent,
		bundle.Workspace.Agent,
		todoService,
		eventBus,
	)

	return &DomainServices{
		TodoService:   todoService,
		AgentWorkflow: agentWorkflow,
		EventBus:      eventBus,
	}, nil
}

// DomainServices contains fully wired domain services
type DomainServices struct {
	TodoService   todo.TodoService
	AgentWorkflow agent.AgentWorkflow
	EventBus      agent.EventBus
}

// SimpleEventBus is a placeholder event bus implementation
type SimpleEventBus struct{}

// Publish implements agent.EventBus
func (b *SimpleEventBus) Publish(ctx context.Context, event *agent.WorkflowEvent) error {
	// For now, just log the event (in production, this would route to event handlers)
	fmt.Printf("Event: %s - %s\n", event.Type, event.Message)
	return nil
}

// Subscribe implements agent.EventBus
func (b *SimpleEventBus) Subscribe(ctx context.Context, eventType agent.EventType, handler func(*agent.WorkflowEvent)) error {
	// For now, return not implemented (in production, this would manage subscriptions)
	return fmt.Errorf("event subscription not implemented in simple event bus")
}

// MigrationSupport provides utilities for gradual migration
type MigrationSupport struct {
	factory *AdapterFactory
}

// NewMigrationSupport creates migration support utilities
func NewMigrationSupport(factory *AdapterFactory) *MigrationSupport {
	return &MigrationSupport{
		factory: factory,
	}
}

// CreateBackwardCompatibleLLMProvider creates an LLM provider that works with existing code
func (m *MigrationSupport) CreateBackwardCompatibleLLMProvider(newProvider interfaces.LLMProvider) llm.LLMProvider {
	return llmAdapter.NewLLMAdapter(newProvider)
}

// GradualMigrationFlags can be used to gradually enable new functionality
type GradualMigrationFlags struct {
	UseDomainServices    bool // Enable domain services
	UseEnhancedContainer bool // Enable enhanced DI container
	UseAdapterLayer      bool // Enable adapter layer
	UseLayeredConfig     bool // Enable layered configuration
}

// GetMigrationFlags gets migration flags from config
func (m *MigrationSupport) GetMigrationFlags() GradualMigrationFlags {
	// For now, use a default value since EnableExperimentalFeatures doesn't exist yet
	enableExperimental := true // Would check m.factory.config.EnableExperimentalFeatures when available

	return GradualMigrationFlags{
		UseDomainServices:    enableExperimental,
		UseEnhancedContainer: enableExperimental,
		UseAdapterLayer:      enableExperimental,
		UseLayeredConfig:     enableExperimental,
	}
}
