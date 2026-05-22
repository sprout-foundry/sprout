package agent

import (
	"context"
	"sync"
	"testing"
)

// TestGetToolRegistry_Singleton verifies that GetToolRegistry returns the same
// instance on every call (lazy-initialized singleton via sync.Once).
func TestGetToolRegistry_Singleton(t *testing.T) {
	// Reset the global state so this test is isolated.
	// Note: registryOnce is unexported, so we test via the public API.
	// Since registryOnce is process-global, this test assumes no other test
	// has already called GetToolRegistry() or InitializeToolRegistry().
	// If they have, GetToolRegistry() will still return the same instance.
	r1 := GetToolRegistry()
	r2 := GetToolRegistry()
	if r1 != r2 {
		t.Error("GetToolRegistry() returned different instances on consecutive calls")
	}
}

// TestGetToolRegistry_ThreadSafety verifies that concurrent calls to
// GetToolRegistry return the same instance without data races.
func TestGetToolRegistry_ThreadSafety(t *testing.T) {
	// We cannot reset registryOnce, so this test uses a local registry
	// to demonstrate the pattern, then verifies the global behaves consistently.
	// Run many goroutines that all call GetToolRegistry simultaneously.
	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	instances := make([]*ToolRegistry, goroutines)
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			r := GetToolRegistry()
			mu.Lock()
			instances[idx] = r
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// All instances should be identical.
	for i := 1; i < goroutines; i++ {
		if instances[i] != instances[0] {
			t.Errorf("goroutine %d returned a different instance than goroutine 0", i)
		}
	}
}

// TestInitializeToolRegistry_PreInitialization verifies that InitializeToolRegistry
// creates the registry before GetToolRegistry is called.
func TestInitializeToolRegistry_PreInitialization(t *testing.T) {
	// We cannot reset the global registryOnce, so test the invariant:
	// InitializeToolRegistry + GetToolRegistry must return the same instance.
	InitializeToolRegistry()
	r := GetToolRegistry()
	if r == nil {
		t.Error("InitializeToolRegistry did not create a non-nil registry")
	}
	if len(r.GetAvailableTools()) == 0 {
		t.Error("InitializeToolRegistry created a registry with no tools")
	}
}

// TestToolRegistry_RegisterAndGet verifies RegisterTool adds a tool and
// GetToolConfig retrieves it correctly.
func TestToolRegistry_RegisterAndGet(t *testing.T) {
	// Create a fresh registry to avoid polluting the global one.
	registry := &ToolRegistry{
		tools: make(map[string]ToolConfig),
	}

	registry.RegisterTool(ToolConfig{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters: []ParameterConfig{
			{"param1", "string", true, []string{}, "First parameter"},
		},
		Handler: func(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
			return "ok", nil
		},
	})

	// Verify GetToolConfig finds it.
	config, found := registry.GetToolConfig("test_tool")
	if !found {
		t.Fatal("GetToolConfig did not find registered tool")
	}
	if config.Name != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got %q", config.Name)
	}
	if config.Description != "A test tool" {
		t.Errorf("expected description 'A test tool', got %q", config.Description)
	}
	if len(config.Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(config.Parameters))
	}
}

// TestToolRegistry_GetToolConfig_NotFound verifies GetToolConfig returns false
// for non-existent tool names.
func TestToolRegistry_GetToolConfig_NotFound(t *testing.T) {
	registry := &ToolRegistry{
		tools: make(map[string]ToolConfig),
	}

	_, found := registry.GetToolConfig("nonexistent")
	if found {
		t.Error("GetToolConfig returned true for non-existent tool")
	}
}

// TestToolRegistry_GetAvailableTools_Empty verifies GetAvailableTools returns
// an empty slice for a registry with no tools.
func TestToolRegistry_GetAvailableTools_Empty(t *testing.T) {
	registry := &ToolRegistry{
		tools: make(map[string]ToolConfig),
	}

	tools := registry.GetAvailableTools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

// TestToolRegistry_GetAvailableTools_Multiple verifies GetAvailableTools returns
// all registered tool names.
func TestToolRegistry_GetAvailableTools_Multiple(t *testing.T) {
	registry := &ToolRegistry{
		tools: make(map[string]ToolConfig),
	}

	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		registry.RegisterTool(ToolConfig{Name: name})
	}

	tools := registry.GetAvailableTools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// Check each name is present (order is not guaranteed due to map iteration).
	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		found := false
		for _, t := range tools {
			if t == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected tool %q in available tools", name)
		}
	}
}

// TestToolRegistry_GetAllToolConfigs_Copy verifies GetAllToolConfigs returns
// a copy of the internal map, not a reference to it. Modifying the returned
// map should not affect the original registry.
func TestToolRegistry_GetAllToolConfigs_Copy(t *testing.T) {
	registry := &ToolRegistry{
		tools: make(map[string]ToolConfig),
	}

	registry.RegisterTool(ToolConfig{
		Name:        "original_tool",
		Description: "Original description",
	})

	// Get a copy of all configs.
	configs := registry.GetAllToolConfigs()

	// Modify the returned map.
	tc := configs["original_tool"]
	tc.Description = "Modified description"
	configs["original_tool"] = tc
	configs["new_tool"] = ToolConfig{Name: "injected"}

	// Verify the original registry is unchanged.
	original, found := registry.GetToolConfig("original_tool")
	if !found {
		t.Fatal("original tool not found after modification")
	}
	if original.Description != "Original description" {
		t.Errorf("registry was modified: description = %q, want 'Original description'", original.Description)
	}
	if _, found := registry.GetToolConfig("new_tool"); found {
		t.Error("injected tool found in registry — GetAllToolConfigs did not return a copy")
	}
}

// TestToolRegistry_GetAllToolConfigs_Empty verifies GetAllToolConfigs returns
// an empty map for a registry with no tools.
func TestToolRegistry_GetAllToolConfigs_Empty(t *testing.T) {
	registry := &ToolRegistry{
		tools: make(map[string]ToolConfig),
	}

	configs := registry.GetAllToolConfigs()
	if len(configs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(configs))
	}
}
