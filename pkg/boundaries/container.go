package boundaries

import (
	"context"
	"fmt"
	"sync"

	"github.com/alantheprice/ledit/pkg/adapters/llm"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/filediscovery"
	legacyLLM "github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/providers"
	"github.com/alantheprice/ledit/pkg/tools"
	"github.com/alantheprice/ledit/pkg/utils"
)

// Container provides dependency injection and service management
// This provides a clean boundary for dependency management
type Container interface {
	// Service accessors
	GetAgentService() AgentService
	GetLLMService() LLMService
	GetFileSystemService() FileSystemService

	// Repository accessors
	GetRepository(entityType string) Repository
	GetConfigRepository() ConfigRepository
	GetSessionRepository() SessionRepository
	GetAuditRepository() AuditRepository
	GetFileRepository() FileRepository
	GetMetricsRepository() MetricsRepository
	GetNotificationRepository() NotificationRepository

	// Utility accessors
	GetLLMProvider() legacyLLM.LLMProvider
	GetFileDiscovery() *filediscovery.FileDiscovery
	GetToolExecutor() *tools.Executor

	// Lifecycle management
	Initialize(ctx context.Context) error
	Shutdown(ctx context.Context) error
	HealthCheck(ctx context.Context) (*HealthStatus, error)

	// Configuration
	GetConfig() *config.Config
	UpdateConfig(cfg *config.Config) error
}

// HealthStatus represents the health status of the container
type HealthStatus struct {
	Status    string            `json:"status"`    // "healthy", "degraded", "unhealthy"
	Services  map[string]string `json:"services"`  // Service name -> status
	Errors    []string          `json:"errors"`    // Any errors encountered
	Timestamp int64             `json:"timestamp"` // Unix timestamp
}

// DefaultContainer provides a default implementation of Container
type DefaultContainer struct {
	mu sync.RWMutex

	// Configuration
	config *config.Config

	// Services
	agentService      AgentService
	llmService        LLMService
	fileSystemService FileSystemService

	// Repositories
	repositories           map[string]Repository
	configRepository       ConfigRepository
	sessionRepository      SessionRepository
	auditRepository        AuditRepository
	fileRepository         FileRepository
	metricsRepository      MetricsRepository
	notificationRepository NotificationRepository

	// Utilities
	llmProvider   legacyLLM.LLMProvider
	fileDiscovery *filediscovery.FileDiscovery
	toolExecutor  *tools.Executor
	errorManager  *utils.ErrorManager

	// Lifecycle
	initialized bool
}

// NewContainer creates a new dependency injection container
func NewContainer(cfg *config.Config) Container {
	return &DefaultContainer{
		config:       cfg,
		repositories: make(map[string]Repository),
		initialized:  false,
	}
}

// Initialize implements Container.Initialize
func (c *DefaultContainer) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	// Initialize error manager first
	c.errorManager = utils.GetErrorManager()
	if c.errorManager == nil {
		c.errorManager = utils.NewErrorManager(utils.GetLogger(true))
		utils.InitErrorHandling(utils.GetLogger(true))
	}

	// Initialize utilities
	c.initializeUtilities()

	// Initialize repositories
	c.initializeRepositories()

	// Initialize services
	c.initializeServices()

	c.initialized = true
	return nil
}

// initializeUtilities initializes utility components
func (c *DefaultContainer) initializeUtilities() {
	logger := utils.GetLogger(c.config.SkipPrompt)

	// Initialize LLM provider using new modular system
	newProvider, err := providers.GetProvider(c.config.EditingModel)
	if err != nil {
		// Fallback to legacy provider if new provider fails
		logger.Log(fmt.Sprintf("Warning: Failed to initialize new provider, falling back to legacy: %v", err))
		c.llmProvider = legacyLLM.NewLLMProvider()
	} else {
		// Use adapter to bridge new provider to legacy interface
		c.llmProvider = llm.NewLLMAdapter(newProvider)
	}

	// Initialize file discovery
	c.fileDiscovery = filediscovery.NewFileDiscovery(c.config, logger)

	// Initialize tool executor
	registry := tools.NewDefaultRegistry()
	permissions := tools.NewSimplePermissionChecker([]string{})
	c.toolExecutor = tools.NewExecutor(registry, permissions, logger, c.config)
}

// initializeRepositories initializes repository components
func (c *DefaultContainer) initializeRepositories() {
	// These would be initialized with actual repository implementations
	// For now, we'll leave them as nil to indicate they're not implemented
	c.configRepository = nil
	c.sessionRepository = nil
	c.auditRepository = nil
	c.fileRepository = nil
	c.metricsRepository = nil
	c.notificationRepository = nil
}

// initializeServices initializes service components
func (c *DefaultContainer) initializeServices() {
	logger := utils.GetLogger(true)

	// Initialize agent service
	c.agentService = NewDefaultAgentService(c.config, logger)

	// Initialize LLM service
	c.llmService = NewDefaultLLMService(c.config, logger)

	// Initialize file system service
	fsConfig := &FileSystemConfig{
		AllowSymlinks:   false,
		MaxFileSize:     100 * 1024 * 1024, // 100MB
		DefaultFileMode: 0644,
		DefaultDirMode:  0755,
		EnableWatch:     false,
		EnableCaching:   false,
	}
	c.fileSystemService = NewDefaultFileSystemService(fsConfig)
}

// Shutdown implements Container.Shutdown
func (c *DefaultContainer) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Shutdown services in reverse order
	// Shutdown agent service
	if c.agentService != nil {
		// No specific shutdown method, but we could add one
	}

	// Shutdown repositories
	// Add shutdown logic for repositories if needed

	c.initialized = false
	return nil
}

// HealthCheck implements Container.HealthCheck
func (c *DefaultContainer) HealthCheck(ctx context.Context) (*HealthStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := &HealthStatus{
		Status:    "healthy",
		Services:  make(map[string]string),
		Errors:    []string{},
		Timestamp: utils.GetCurrentTimestamp(),
	}

	// Check agent service
	if c.agentService != nil {
		status.Services["agent"] = "healthy"
	} else {
		status.Services["agent"] = "unavailable"
		status.Status = "degraded"
		status.Errors = append(status.Errors, "agent service not initialized")
	}

	// Check LLM service
	if c.llmService != nil {
		status.Services["llm"] = "healthy"
	} else {
		status.Services["llm"] = "unavailable"
		status.Status = "degraded"
		status.Errors = append(status.Errors, "LLM service not initialized")
	}

	// Check file system service
	if c.fileSystemService != nil {
		status.Services["filesystem"] = "healthy"
	} else {
		status.Services["filesystem"] = "unavailable"
		status.Status = "degraded"
		status.Errors = append(status.Errors, "file system service not initialized")
	}

	return status, nil
}

// GetAgentService implements Container.GetAgentService
func (c *DefaultContainer) GetAgentService() AgentService {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.agentService
}

// GetLLMService implements Container.GetLLMService
func (c *DefaultContainer) GetLLMService() LLMService {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.llmService
}

// GetFileSystemService implements Container.GetFileSystemService
func (c *DefaultContainer) GetFileSystemService() FileSystemService {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fileSystemService
}

// GetRepository implements Container.GetRepository
func (c *DefaultContainer) GetRepository(entityType string) Repository {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.repositories[entityType]
}

// GetConfigRepository implements Container.GetConfigRepository
func (c *DefaultContainer) GetConfigRepository() ConfigRepository {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.configRepository
}

// GetSessionRepository implements Container.GetSessionRepository
func (c *DefaultContainer) GetSessionRepository() SessionRepository {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionRepository
}

// GetAuditRepository implements Container.GetAuditRepository
func (c *DefaultContainer) GetAuditRepository() AuditRepository {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.auditRepository
}

// GetFileRepository implements Container.GetFileRepository
func (c *DefaultContainer) GetFileRepository() FileRepository {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fileRepository
}

// GetMetricsRepository implements Container.GetMetricsRepository
func (c *DefaultContainer) GetMetricsRepository() MetricsRepository {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metricsRepository
}

// GetNotificationRepository implements Container.GetNotificationRepository
func (c *DefaultContainer) GetNotificationRepository() NotificationRepository {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.notificationRepository
}

// GetLLMProvider implements Container.GetLLMProvider
func (c *DefaultContainer) GetLLMProvider() legacyLLM.LLMProvider {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.llmProvider
}

// GetFileDiscovery implements Container.GetFileDiscovery
func (c *DefaultContainer) GetFileDiscovery() *filediscovery.FileDiscovery {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fileDiscovery
}

// GetToolExecutor implements Container.GetToolExecutor
func (c *DefaultContainer) GetToolExecutor() *tools.Executor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.toolExecutor
}

// GetConfig implements Container.GetConfig
func (c *DefaultContainer) GetConfig() *config.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// UpdateConfig implements Container.UpdateConfig
func (c *DefaultContainer) UpdateConfig(cfg *config.Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.config = cfg

	// Reinitialize components that depend on config
	c.initializeUtilities()
	c.initializeServices()

	return nil
}

// Service Locator pattern for global access (use sparingly)
var globalContainer Container

// SetGlobalContainer sets the global container instance
func SetGlobalContainer(container Container) {
	globalContainer = container
}

// GetGlobalContainer returns the global container instance
func GetGlobalContainer() Container {
	return globalContainer
}

// GetService is a convenience function to get a service from the global container
func GetService[T any](getter func(Container) T) T {
	var zero T
	if globalContainer == nil {
		return zero
	}
	return getter(globalContainer)
}

// GetAgentService is a convenience function to get the agent service
func GetAgentService() AgentService {
	return GetService(func(c Container) AgentService { return c.GetAgentService() })
}

// GetLLMService is a convenience function to get the LLM service
func GetLLMService() LLMService {
	return GetService(func(c Container) LLMService { return c.GetLLMService() })
}

// GetFileSystemService is a convenience function to get the file system service
func GetFileSystemService() FileSystemService {
	return GetService(func(c Container) FileSystemService { return c.GetFileSystemService() })
}
