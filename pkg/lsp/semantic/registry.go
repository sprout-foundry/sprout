package semantic

import (
	"strings"
	"time"
)

// Adapter is implemented by language-specific semantic backends.
type Adapter interface {
	Run(input ToolInput) (ToolResult, error)
}

// AdapterFactory creates a new adapter instance.
type AdapterFactory func() Adapter

// Registry maps language IDs to adapters.
// Two registration modes are supported:
//   - Factory (RegisterAliases / Register): a new Adapter is created per request.
//     Use for lightweight stateless adapters.
//   - Singleton (RegisterSingleton): a shared Adapter instance handles all requests
//     for the registered language IDs. Use for SessionPool or other stateful adapters
//     that are expensive to create and should live across requests.
type Registry struct {
	factories  map[string]AdapterFactory
	singletons map[string]Adapter
}

// NewRegistry creates an empty adapter registry.
func NewRegistry() *Registry {
	return &Registry{
		factories:  make(map[string]AdapterFactory),
		singletons: make(map[string]Adapter),
	}
}

// Register binds one language ID to an adapter factory.
func (r *Registry) Register(languageID string, factory AdapterFactory) {
	if r == nil || factory == nil {
		return
	}
	id := strings.TrimSpace(strings.ToLower(languageID))
	if id == "" {
		return
	}
	r.factories[id] = factory
}

// RegisterAliases binds one factory to many language IDs.
func (r *Registry) RegisterAliases(factory AdapterFactory, languageIDs ...string) {
	for _, languageID := range languageIDs {
		r.Register(languageID, factory)
	}
}

// RegisterSingleton binds a shared adapter instance to one or more language IDs.
// The same adapter instance is reused for every request on those language IDs.
// This is the right choice for a SessionPool or any adapter that maintains
// per-workspace state across calls.
func (r *Registry) RegisterSingleton(adapter Adapter, languageIDs ...string) {
	if r == nil || adapter == nil {
		return
	}
	for _, languageID := range languageIDs {
		id := strings.TrimSpace(strings.ToLower(languageID))
		if id == "" {
			continue
		}
		r.singletons[id] = adapter
	}
}

// AdapterForLanguage returns an adapter for the language ID and records the
// dispatch duration in the result's DurationMs field.
// Singletons take precedence over factory registrations for the same language ID.
func (r *Registry) AdapterForLanguage(languageID string) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	id := strings.TrimSpace(strings.ToLower(languageID))

	if singleton, ok := r.singletons[id]; ok {
		return timedAdapter{inner: singleton}, true
	}
	factory, ok := r.factories[id]
	if !ok {
		return nil, false
	}
	return timedAdapter{inner: factory()}, true
}

// timedAdapter wraps any Adapter and stamps DurationMs onto the result.
type timedAdapter struct {
	inner Adapter
}

func (t timedAdapter) Run(input ToolInput) (ToolResult, error) {
	start := time.Now()
	result, err := t.inner.Run(input)
	result.DurationMs = time.Since(start).Milliseconds()
	return result, err
}
