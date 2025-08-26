package boundaries

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alantheprice/ledit/internal/domain/agent"
	"github.com/alantheprice/ledit/internal/domain/todo"
	"github.com/alantheprice/ledit/pkg/adapters"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/interfaces"
	legacyLLM "github.com/alantheprice/ledit/pkg/llm"
)

// EnhancedContainer extends the basic container with domain services and improved DI
type EnhancedContainer interface {
	Container // Embed existing container interface

	// Domain services
	GetTodoService() todo.TodoService
	GetAgentWorkflow() agent.AgentWorkflow
	GetCodeGenerator() interfaces.CodeGenerator
	GetWorkspaceAnalyzer() interfaces.WorkspaceAnalyzer

	// Enhanced provider services
	GetLLMProviderNew() interfaces.LLMProvider
	GetPromptProvider() interfaces.PromptProvider
	GetConfigProvider() interfaces.ConfigProvider

	// Service registration and discovery
	RegisterService(name string, factory ServiceFactory) error
	RegisterSingleton(name string, instance interface{}) error
	GetRegisteredService(name string) (interface{}, error)
	ListServices() []ServiceInfo

	// Advanced lifecycle management
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	IsReady() bool
}

// ServiceFactory defines how services are created
type ServiceFactory interface {
	Create(container EnhancedContainer) (interface{}, error)
	GetName() string
	GetType() ServiceType
	GetDependencies() []string
}

// ServiceType represents the lifecycle type of a service
type ServiceType int

const (
	ServiceTypeSingleton ServiceType = iota
	ServiceTypeTransient
	ServiceTypeScoped
)

// ServiceInfo contains information about a registered service
type ServiceInfo struct {
	Name         string        `json:"name"`
	Type         ServiceType   `json:"type"`
	Status       ServiceStatus `json:"status"`
	Dependencies []string      `json:"dependencies"`
	CreatedAt    time.Time     `json:"created_at"`
	LastAccessed time.Time     `json:"last_accessed"`
	AccessCount  int64         `json:"access_count"`
}

// ServiceStatus represents the status of a service
type ServiceStatus int

const (
	ServiceStatusRegistered ServiceStatus = iota
	ServiceStatusInitializing
	ServiceStatusReady
	ServiceStatusFailed
	ServiceStatusStopped
)

// enhancedContainerImpl implements EnhancedContainer
type enhancedContainerImpl struct {
	*DefaultContainer // Embed existing container

	mu                sync.RWMutex
	serviceFactories  map[string]ServiceFactory
	singletonServices map[string]interface{}
	serviceInfo       map[string]*ServiceInfo

	// Domain services
	todoService       todo.TodoService
	agentWorkflow     agent.AgentWorkflow
	codeGenerator     interfaces.CodeGenerator
	workspaceAnalyzer interfaces.WorkspaceAnalyzer

	// Enhanced providers
	llmProviderNew    interfaces.LLMProvider
	promptProvider    interfaces.PromptProvider
	configProviderNew interfaces.ConfigProvider

	// Adapter layer
	adapterFactory *adapters.AdapterFactory
	adapterBundle  *adapters.AdapterBundle
	domainServices *adapters.DomainServices

	// State
	isStarted bool
	startTime time.Time
}

// NewEnhancedContainer creates a new enhanced dependency injection container
func NewEnhancedContainer(cfg *config.Config) EnhancedContainer {
	baseContainer := NewContainer(cfg).(*DefaultContainer)

	return &enhancedContainerImpl{
		DefaultContainer:  baseContainer,
		serviceFactories:  make(map[string]ServiceFactory),
		singletonServices: make(map[string]interface{}),
		serviceInfo:       make(map[string]*ServiceInfo),
		isStarted:         false,
	}
}

// Start implements EnhancedContainer.Start
func (c *enhancedContainerImpl) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isStarted {
		return nil
	}

	// Initialize base container first
	if err := c.DefaultContainer.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize base container: %w", err)
	}

	// Initialize enhanced services
	if err := c.initializeEnhancedServices(ctx); err != nil {
		return fmt.Errorf("failed to initialize enhanced services: %w", err)
	}

	c.isStarted = true
	c.startTime = time.Now()

	return nil
}

// Stop implements EnhancedContainer.Stop
func (c *enhancedContainerImpl) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isStarted {
		return nil
	}

	// Stop enhanced services
	c.stopEnhancedServices(ctx)

	// Stop base container
	if err := c.DefaultContainer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown base container: %w", err)
	}

	c.isStarted = false

	return nil
}

// Restart implements EnhancedContainer.Restart
func (c *enhancedContainerImpl) Restart(ctx context.Context) error {
	if err := c.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// IsReady implements EnhancedContainer.IsReady
func (c *enhancedContainerImpl) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isStarted
}

// RegisterService implements EnhancedContainer.RegisterService
func (c *enhancedContainerImpl) RegisterService(name string, factory ServiceFactory) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.serviceFactories[name]; exists {
		return fmt.Errorf("service %s is already registered", name)
	}

	c.serviceFactories[name] = factory
	c.serviceInfo[name] = &ServiceInfo{
		Name:         name,
		Type:         factory.GetType(),
		Status:       ServiceStatusRegistered,
		Dependencies: factory.GetDependencies(),
		CreatedAt:    time.Now(),
	}

	return nil
}

// RegisterSingleton implements EnhancedContainer.RegisterSingleton
func (c *enhancedContainerImpl) RegisterSingleton(name string, instance interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.singletonServices[name]; exists {
		return fmt.Errorf("singleton service %s is already registered", name)
	}

	c.singletonServices[name] = instance
	c.serviceInfo[name] = &ServiceInfo{
		Name:      name,
		Type:      ServiceTypeSingleton,
		Status:    ServiceStatusReady,
		CreatedAt: time.Now(),
	}

	return nil
}

// GetRegisteredService implements EnhancedContainer.GetRegisteredService
func (c *enhancedContainerImpl) GetRegisteredService(name string) (interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check singletons first
	if service, exists := c.singletonServices[name]; exists {
		c.updateServiceAccess(name)
		return service, nil
	}

	// Check factories
	factory, exists := c.serviceFactories[name]
	if !exists {
		return nil, fmt.Errorf("service %s is not registered", name)
	}

	// Update service info
	if info, exists := c.serviceInfo[name]; exists {
		info.Status = ServiceStatusInitializing
	}

	// Create service instance
	service, err := factory.Create(c)
	if err != nil {
		if info, exists := c.serviceInfo[name]; exists {
			info.Status = ServiceStatusFailed
		}
		return nil, fmt.Errorf("failed to create service %s: %w", name, err)
	}

	// For singleton services, cache the instance
	if factory.GetType() == ServiceTypeSingleton {
		c.singletonServices[name] = service
	}

	// Update service info
	if info, exists := c.serviceInfo[name]; exists {
		info.Status = ServiceStatusReady
	}

	c.updateServiceAccess(name)
	return service, nil
}

// ListServices implements EnhancedContainer.ListServices
func (c *enhancedContainerImpl) ListServices() []ServiceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	services := make([]ServiceInfo, 0, len(c.serviceInfo))
	for _, info := range c.serviceInfo {
		services = append(services, *info)
	}

	return services
}

// Domain service getters

// GetTodoService implements EnhancedContainer.GetTodoService
func (c *enhancedContainerImpl) GetTodoService() todo.TodoService {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.todoService
}

// GetAgentWorkflow implements EnhancedContainer.GetAgentWorkflow
func (c *enhancedContainerImpl) GetAgentWorkflow() agent.AgentWorkflow {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.agentWorkflow
}

// GetCodeGenerator implements EnhancedContainer.GetCodeGenerator
func (c *enhancedContainerImpl) GetCodeGenerator() interfaces.CodeGenerator {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.codeGenerator
}

// GetWorkspaceAnalyzer implements EnhancedContainer.GetWorkspaceAnalyzer
func (c *enhancedContainerImpl) GetWorkspaceAnalyzer() interfaces.WorkspaceAnalyzer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.workspaceAnalyzer
}

// Enhanced provider getters

// GetLLMProviderNew implements EnhancedContainer.GetLLMProviderNew
func (c *enhancedContainerImpl) GetLLMProviderNew() interfaces.LLMProvider {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.llmProviderNew
}

// GetPromptProvider implements EnhancedContainer.GetPromptProvider
func (c *enhancedContainerImpl) GetPromptProvider() interfaces.PromptProvider {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.promptProvider
}

// GetConfigProvider implements EnhancedContainer.GetConfigProvider
func (c *enhancedContainerImpl) GetConfigProvider() interfaces.ConfigProvider {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.configProviderNew
}

// Private helper methods

// initializeEnhancedServices initializes the enhanced services
func (c *enhancedContainerImpl) initializeEnhancedServices(ctx context.Context) error {
	// Initialize enhanced providers first
	if err := c.initializeEnhancedProviders(); err != nil {
		return fmt.Errorf("failed to initialize enhanced providers: %w", err)
	}

	// Initialize domain services
	if err := c.initializeDomainServices(); err != nil {
		return fmt.Errorf("failed to initialize domain services: %w", err)
	}

	// Initialize registered services
	if err := c.initializeRegisteredServices(); err != nil {
		return fmt.Errorf("failed to initialize registered services: %w", err)
	}

	return nil
}

// initializeEnhancedProviders initializes enhanced provider services
func (c *enhancedContainerImpl) initializeEnhancedProviders() error {
	// Initialize adapter factory
	factory, err := adapters.NewAdapterFactory(c.GetConfig())
	if err != nil {
		return fmt.Errorf("failed to create adapter factory: %w", err)
	}
	c.adapterFactory = factory

	// For now, skip the enhanced LLM provider initialization
	// This will be implemented when the registry factory methods are available
	// Use the existing DefaultContainer's LLM provider instead
	c.llmProviderNew = nil // Will be set up later when needed

	// Create adapter bundle with nil provider for now
	c.adapterBundle = c.adapterFactory.CreateAdapterBundle(nil)
	c.configProviderNew = c.adapterBundle.Config

	return nil
}

// initializeDomainServices initializes domain services
func (c *enhancedContainerImpl) initializeDomainServices() error {
	if c.adapterBundle == nil {
		return fmt.Errorf("adapter bundle must be initialized first")
	}

	// Wire domain services using the adapter factory
	domainServices, err := c.adapterFactory.WireServices(c.adapterBundle)
	if err != nil {
		return fmt.Errorf("failed to wire domain services: %w", err)
	}
	c.domainServices = domainServices

	// Set domain services
	c.todoService = domainServices.TodoService
	c.agentWorkflow = domainServices.AgentWorkflow

	// Initialize other services
	c.codeGenerator = nil     // Placeholder - would implement interfaces.CodeGenerator
	c.workspaceAnalyzer = nil // Placeholder - interface mismatch, will be fixed later

	return nil
}

// initializeRegisteredServices initializes services that were registered via factories
func (c *enhancedContainerImpl) initializeRegisteredServices() error {
	// Initialize singleton services that have dependencies
	for name, factory := range c.serviceFactories {
		if factory.GetType() == ServiceTypeSingleton {
			// Check if service has been created
			if _, exists := c.singletonServices[name]; !exists {
				// Create the service
				_, err := c.GetRegisteredService(name)
				if err != nil {
					return fmt.Errorf("failed to initialize singleton service %s: %w", name, err)
				}
			}
		}
	}

	return nil
}

// stopEnhancedServices stops enhanced services
func (c *enhancedContainerImpl) stopEnhancedServices(ctx context.Context) {
	// Update service statuses
	for name, info := range c.serviceInfo {
		info.Status = ServiceStatusStopped
		_ = name // avoid unused variable
	}

	// Clear singleton services
	c.singletonServices = make(map[string]interface{})
}

// updateServiceAccess updates access statistics for a service
func (c *enhancedContainerImpl) updateServiceAccess(name string) {
	if info, exists := c.serviceInfo[name]; exists {
		info.LastAccessed = time.Now()
		info.AccessCount++
	}
}

// Note: Placeholder LLM provider removed - now using real adapter system

// ServiceFactoryFunc is a convenient way to create simple service factories
type ServiceFactoryFunc struct {
	name         string
	serviceType  ServiceType
	dependencies []string
	createFunc   func(EnhancedContainer) (interface{}, error)
}

// Create implements ServiceFactory.Create
func (f *ServiceFactoryFunc) Create(container EnhancedContainer) (interface{}, error) {
	return f.createFunc(container)
}

// GetName implements ServiceFactory.GetName
func (f *ServiceFactoryFunc) GetName() string {
	return f.name
}

// GetType implements ServiceFactory.GetType
func (f *ServiceFactoryFunc) GetType() ServiceType {
	return f.serviceType
}

// GetDependencies implements ServiceFactory.GetDependencies
func (f *ServiceFactoryFunc) GetDependencies() []string {
	return f.dependencies
}

// NewServiceFactory creates a new service factory
func NewServiceFactory(
	name string,
	serviceType ServiceType,
	dependencies []string,
	createFunc func(EnhancedContainer) (interface{}, error),
) ServiceFactory {
	return &ServiceFactoryFunc{
		name:         name,
		serviceType:  serviceType,
		dependencies: dependencies,
		createFunc:   createFunc,
	}
}

// Convenience functions for common service patterns

// RegisterLLMProvider registers an LLM provider service
func (c *enhancedContainerImpl) RegisterLLMProvider(name string, provider interfaces.LLMProvider) error {
	return c.RegisterSingleton(fmt.Sprintf("llm_provider_%s", name), provider)
}

// GetLLMProvider implements Container.GetLLMProvider - returns the default LLM provider
func (c *enhancedContainerImpl) GetLLMProvider() legacyLLM.LLMProvider {
	// Use the embedded DefaultContainer's LLM provider for compatibility
	return c.DefaultContainer.GetLLMProvider()
}

// GetLLMProviderByName gets a specific LLM provider by name
func (c *enhancedContainerImpl) GetLLMProviderByName(name string) (interfaces.LLMProvider, error) {
	service, err := c.GetRegisteredService(fmt.Sprintf("llm_provider_%s", name))
	if err != nil {
		return nil, err
	}

	provider, ok := service.(interfaces.LLMProvider)
	if !ok {
		return nil, fmt.Errorf("service is not an LLM provider")
	}

	return provider, nil
}

// Global enhanced container instance
var globalEnhancedContainer EnhancedContainer

// SetGlobalEnhancedContainer sets the global enhanced container
func SetGlobalEnhancedContainer(container EnhancedContainer) {
	globalEnhancedContainer = container
}

// GetGlobalEnhancedContainer returns the global enhanced container
func GetGlobalEnhancedContainer() EnhancedContainer {
	return globalEnhancedContainer
}
