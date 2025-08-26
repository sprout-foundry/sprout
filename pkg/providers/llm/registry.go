package llm

import (
	"context"
	"fmt"
	"sync"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// Registry manages LLM provider registration and discovery
type Registry struct {
	mu        sync.RWMutex
	providers map[string]ProviderFactory
	instances map[string]interfaces.LLMProvider
}

// ProviderFactory creates new instances of a specific provider
type ProviderFactory interface {
	// Create creates a new provider instance with the given configuration
	Create(config *types.ProviderConfig) (interfaces.LLMProvider, error)

	// GetName returns the name of the provider
	GetName() string

	// Validate validates the provider configuration
	Validate(config *types.ProviderConfig) error
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]ProviderFactory),
		instances: make(map[string]interfaces.LLMProvider),
	}
}

// Register registers a new provider factory
func (r *Registry) Register(factory ProviderFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := factory.GetName()
	if name == "" {
		return fmt.Errorf("provider factory must have a non-empty name")
	}

	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider '%s' is already registered", name)
	}

	r.providers[name] = factory
	return nil
}

// Unregister removes a provider factory from the registry
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; !exists {
		return fmt.Errorf("provider '%s' is not registered", name)
	}

	delete(r.providers, name)
	delete(r.instances, name) // Also remove any cached instance
	return nil
}

// GetProvider returns a provider instance, creating it if necessary
func (r *Registry) GetProvider(name string, config *types.ProviderConfig) (interfaces.LLMProvider, error) {
	r.mu.RLock()

	// Check if we have a cached instance
	if instance, exists := r.instances[name]; exists {
		r.mu.RUnlock()
		return instance, nil
	}

	// Check if factory is registered
	factory, exists := r.providers[name]
	if !exists {
		r.mu.RUnlock()
		return nil, fmt.Errorf("provider '%s' is not registered", name)
	}

	r.mu.RUnlock()

	// Create new instance
	instance, err := factory.Create(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider '%s': %w", name, err)
	}

	// Cache the instance
	r.mu.Lock()
	r.instances[name] = instance
	r.mu.Unlock()

	return instance, nil
}

// ListProviders returns the names of all registered providers
func (r *Registry) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}

	return names
}

// ValidateConfig validates a configuration for a specific provider
func (r *Registry) ValidateConfig(name string, config *types.ProviderConfig) error {
	r.mu.RLock()
	factory, exists := r.providers[name]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("provider '%s' is not registered", name)
	}

	return factory.Validate(config)
}

// CheckHealth checks the health of all registered providers
func (r *Registry) CheckHealth(ctx context.Context) map[string]error {
	r.mu.RLock()
	instances := make(map[string]interfaces.LLMProvider, len(r.instances))
	for name, instance := range r.instances {
		instances[name] = instance
	}
	r.mu.RUnlock()

	results := make(map[string]error)
	for name, instance := range instances {
		results[name] = instance.IsAvailable(ctx)
	}

	return results
}

// Clear clears all cached provider instances
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.instances = make(map[string]interfaces.LLMProvider)
}

// GetProviderInfo returns information about a registered provider
func (r *Registry) GetProviderInfo(ctx context.Context, name string, config *types.ProviderConfig) (*ProviderInfo, error) {
	provider, err := r.GetProvider(name, config)
	if err != nil {
		return nil, err
	}

	models, err := provider.GetModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get models for provider '%s': %w", name, err)
	}

	healthErr := provider.IsAvailable(ctx)

	return &ProviderInfo{
		Name:      name,
		Available: healthErr == nil,
		Models:    models,
		Error:     healthErr,
	}, nil
}

// ProviderInfo contains information about a provider
type ProviderInfo struct {
	Name      string            `json:"name"`
	Available bool              `json:"available"`
	Models    []types.ModelInfo `json:"models"`
	Error     error             `json:"error,omitempty"`
}

// Global registry instance
var globalRegistry = NewRegistry()

// RegisterProvider registers a provider factory with the global registry
func RegisterProvider(factory ProviderFactory) error {
	return globalRegistry.Register(factory)
}

// GetGlobalRegistry returns the global provider registry
func GetGlobalRegistry() *Registry {
	return globalRegistry
}
