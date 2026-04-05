package tools

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// simpleStubTool is a minimal Tool implementation for testing
type simpleStubTool struct {
	name              string
	description       string
	category          string
	permissions       []string
	estimatedDuration time.Duration
	isAvailable       bool
}

// NewSimpleStubTool creates a new simpleStubTool with the given name, category, and permissions
func NewSimpleStubTool(name, category string, permissions ...string) *simpleStubTool {
	return &simpleStubTool{
		name:              name,
		description:       "Stub tool for testing: " + name,
		category:          category,
		permissions:       permissions,
		estimatedDuration: time.Second,
		isAvailable:       true,
	}
}

func (s *simpleStubTool) Name() string {
	return s.name
}

func (s *simpleStubTool) Description() string {
	return s.description
}

func (s *simpleStubTool) Category() string {
	return s.category
}

func (s *simpleStubTool) Execute(ctx context.Context, params Parameters) (*Result, error) {
	return &Result{
		Success:       true,
		Output:        fmt.Sprintf("executed %s", s.name),
		ExecutionTime: time.Millisecond,
	}, nil
}

func (s *simpleStubTool) CanExecute(ctx context.Context, params Parameters) bool {
	return s.isAvailable
}

func (s *simpleStubTool) RequiredPermissions() []string {
	return s.permissions
}

func (s *simpleStubTool) EstimatedDuration() time.Duration {
	return s.estimatedDuration
}

func (s *simpleStubTool) IsAvailable() bool {
	return s.isAvailable
}

// TestNewDefaultRegistry tests the NewDefaultRegistry constructor
func TestNewDefaultRegistry(t *testing.T) {
	t.Run("creates empty registry", func(t *testing.T) {
		registry := NewDefaultRegistry()
		if registry == nil {
			t.Fatal("expected non-nil registry")
		}
		if registry.GetToolCount() != 0 {
			t.Errorf("expected empty registry, got count %d", registry.GetToolCount())
		}
		if registry.HasTool("any") {
			t.Error("expected no tools registered")
		}
		if len(registry.ListTools()) != 0 {
			t.Error("expected empty tool list")
		}
		if len(registry.GetToolNames()) != 0 {
			t.Error("expected empty tool names")
		}
	})
}

// TestRegisterTool tests the RegisterTool method
func TestRegisterTool(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func() Tool
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil tool error",
			setupFunc:   func() Tool { return nil },
			expectError: true,
			errorMsg:    "cannot register nil tool",
		},
		{
			name:        "empty name error",
			setupFunc:   func() Tool { return &simpleStubTool{name: "", category: CategoryFile} },
			expectError: true,
			errorMsg:    "tool name cannot be empty",
		},
		{
			name:        "duplicate registration error",
			setupFunc:   func() Tool { return NewSimpleStubTool("dup_test_tool", CategoryFile) },
			expectError: true,
			errorMsg:    "already registered",
		},
		{
			name:        "successful registration",
			setupFunc:   func() Tool { return NewSimpleStubTool("unique_tool", CategorySearch) },
			expectError: false,
			errorMsg:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewDefaultRegistry()
			tool := tt.setupFunc()

			// For duplicate test, register the tool first
			if tt.name == "duplicate registration error" {
				_ = registry.RegisterTool(tool)
			}

			err := registry.RegisterTool(tool)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorMsg != "" && !containsSubstring(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}

	// Test successful registration flow
	t.Run("successful registration flow", func(t *testing.T) {
		registry := NewDefaultRegistry()
		tool := NewSimpleStubTool("test_tool", CategoryFile, PermissionReadFile)
		err := registry.RegisterTool(tool)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify tool is registered
		retrieved, exists := registry.GetTool("test_tool")
		if !exists {
			t.Fatal("tool should exist after registration")
		}
		if retrieved.Name() != "test_tool" {
			t.Errorf("expected name 'test_tool', got %q", retrieved.Name())
		}
		if retrieved.Category() != CategoryFile {
			t.Errorf("expected category %q, got %q", CategoryFile, retrieved.Category())
		}
		if len(retrieved.RequiredPermissions()) != 1 || retrieved.RequiredPermissions()[0] != PermissionReadFile {
			t.Errorf("expected permission %q, got %v", PermissionReadFile, retrieved.RequiredPermissions())
		}
	})
}

// TestGetTool tests the GetTool method
func TestGetTool(t *testing.T) {
	registry := NewDefaultRegistry()
	registry.RegisterTool(NewSimpleStubTool("tool_a", CategoryFile))
	registry.RegisterTool(NewSimpleStubTool("tool_b", CategorySearch))

	tests := []struct {
		name         string
		toolName     string
		expectExists bool
		expectName   string
	}{
		{
			name:         "found",
			toolName:     "tool_a",
			expectExists: true,
			expectName:   "tool_a",
		},
		{
			name:         "not found",
			toolName:     "nonexistent",
			expectExists: false,
			expectName:   "",
		},
		{
			name:         "empty name",
			toolName:     "",
			expectExists: false,
			expectName:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, exists := registry.GetTool(tt.toolName)
			if exists != tt.expectExists {
				t.Errorf("expected exists=%v, got %v", tt.expectExists, exists)
			}
			if exists && tool.Name() != tt.expectName {
				t.Errorf("expected name %q, got %q", tt.expectName, tool.Name())
			}
		})
	}
}

// TestUnregisterTool tests the UnregisterTool method
func TestUnregisterTool(t *testing.T) {
	tests := []struct {
		name        string
		toolName    string
		setupFunc   func(*DefaultRegistry)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "not found error",
			toolName:    "nonexistent",
			setupFunc:   func(r *DefaultRegistry) {},
			expectError: true,
			errorMsg:    "is not registered",
		},
		{
			name: "successful removal",
			toolName: "test_tool",
			setupFunc: func(r *DefaultRegistry) {
				r.RegisterTool(NewSimpleStubTool("test_tool", CategoryFile))
			},
			expectError: false,
			errorMsg:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewDefaultRegistry()
			tt.setupFunc(registry)

			err := registry.UnregisterTool(tt.toolName)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorMsg != "" && !containsSubstring(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}

	// Test that removed tool is no longer accessible
	t.Run("verify removed", func(t *testing.T) {
		registry := NewDefaultRegistry()
		tool := NewSimpleStubTool("to_remove", CategoryEdit)
		err := registry.RegisterTool(tool)
		if err != nil {
			t.Fatalf("setup: unexpected error: %v", err)
		}

		// Verify tool exists
		if _, exists := registry.GetTool("to_remove"); !exists {
			t.Fatal("tool should exist before removal")
		}

		// Remove the tool
		err = registry.UnregisterTool("to_remove")
		if err != nil {
			t.Fatalf("unexpected error during removal: %v", err)
		}

		// Verify tool is gone
		if _, exists := registry.GetTool("to_remove"); exists {
			t.Error("tool should not exist after removal")
		}
		if registry.HasTool("to_remove") {
			t.Error("HasTool should return false after removal")
		}
		if registry.GetToolCount() != 0 {
			t.Errorf("expected count 0, got %d", registry.GetToolCount())
		}
	})
}

// TestListTools tests the ListTools method
func TestListTools(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(*DefaultRegistry)
		expectCount   int
		expectNames   []string
		expectEmpty   bool
	}{
		{
			name:        "empty",
			setupFunc:   func(r *DefaultRegistry) {},
			expectCount: 0,
			expectEmpty: true,
		},
		{
			name: "single",
			setupFunc: func(r *DefaultRegistry) {
				r.RegisterTool(NewSimpleStubTool("single_tool", CategoryFile))
			},
			expectCount: 1,
			expectNames: []string{"single_tool"},
		},
		{
			name: "multiple",
			setupFunc: func(r *DefaultRegistry) {
				r.RegisterTool(NewSimpleStubTool("tool_1", CategoryFile))
				r.RegisterTool(NewSimpleStubTool("tool_2", CategorySearch))
				r.RegisterTool(NewSimpleStubTool("tool_3", CategoryEdit))
			},
			expectCount: 3,
			expectNames: []string{"tool_1", "tool_2", "tool_3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewDefaultRegistry()
			tt.setupFunc(registry)

			tools := registry.ListTools()
			if len(tools) != tt.expectCount {
				t.Errorf("expected %d tools, got %d", tt.expectCount, len(tools))
			}
			if tt.expectEmpty && len(tools) != 0 {
				t.Error("expected empty list")
			}

			if len(tt.expectNames) > 0 {
				gotNames := make([]string, len(tools))
				for i, tool := range tools {
					gotNames[i] = tool.Name()
				}
				for _, expected := range tt.expectNames {
					found := false
					for _, got := range gotNames {
						if got == expected {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected tool %q in list, not found", expected)
					}
				}
			}
		})
	}
}

// TestListToolsByCategory tests the ListToolsByCategory method
func TestListToolsByCategory(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(*DefaultRegistry)
		category      string
		expectCount   int
		expectNames   []string
		expectEmpty   bool
	}{
		{
			name: "filter by category",
			setupFunc: func(r *DefaultRegistry) {
				r.RegisterTool(NewSimpleStubTool("file_tool", CategoryFile))
				r.RegisterTool(NewSimpleStubTool("search_tool", CategorySearch))
				r.RegisterTool(NewSimpleStubTool("another_file", CategoryFile))
			},
			category:    CategoryFile,
			expectCount: 2,
			expectNames: []string{"file_tool", "another_file"},
		},
		{
			name:        "empty category",
			setupFunc: func(r *DefaultRegistry) {
				r.RegisterTool(NewSimpleStubTool("tool_1", CategoryFile))
				r.RegisterTool(NewSimpleStubTool("tool_2", CategorySearch))
			},
			category:    "",
			expectCount: 0,
			expectEmpty: true,
		},
		{
			name:        "nonexistent category",
			setupFunc: func(r *DefaultRegistry) {
				r.RegisterTool(NewSimpleStubTool("tool_1", CategoryFile))
			},
			category:    "nonexistent",
			expectCount: 0,
			expectEmpty: true,
		},
		{
			name:        "empty registry",
			setupFunc:   func(r *DefaultRegistry) {},
			category:    CategoryFile,
			expectCount: 0,
			expectEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewDefaultRegistry()
			tt.setupFunc(registry)

			tools := registry.ListToolsByCategory(tt.category)
			if len(tools) != tt.expectCount {
				t.Errorf("expected %d tools, got %d", tt.expectCount, len(tools))
			}
			if tt.expectEmpty && len(tools) != 0 {
				t.Error("expected empty list")
			}

			for _, tool := range tools {
				if tool.Category() != tt.category {
					t.Errorf("expected all tools in category %q, got %q", tt.category, tool.Category())
				}
			}

			if len(tt.expectNames) > 0 {
				gotNames := make([]string, len(tools))
				for i, tool := range tools {
					gotNames[i] = tool.Name()
				}
				for _, expected := range tt.expectNames {
					found := false
					for _, got := range gotNames {
						if got == expected {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected tool %q in filtered list, not found", expected)
					}
				}
			}
		})
	}
}

// TestGetToolNames tests the GetToolNames method
func TestGetToolNames(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(*DefaultRegistry)
		expectCount   int
		expectNames   []string
		expectEmpty   bool
	}{
		{
			name:        "empty registry",
			setupFunc:   func(r *DefaultRegistry) {},
			expectCount: 0,
			expectEmpty: true,
		},
		{
			name: "single tool",
			setupFunc: func(r *DefaultRegistry) {
				r.RegisterTool(NewSimpleStubTool("my_tool", CategoryFile))
			},
			expectCount: 1,
			expectNames: []string{"my_tool"},
		},
		{
			name: "multiple tools",
			setupFunc: func(r *DefaultRegistry) {
				r.RegisterTool(NewSimpleStubTool("tool_a", CategoryFile))
				r.RegisterTool(NewSimpleStubTool("tool_b", CategorySearch))
				r.RegisterTool(NewSimpleStubTool("tool_c", CategoryEdit))
			},
			expectCount: 3,
			expectNames: []string{"tool_a", "tool_b", "tool_c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewDefaultRegistry()
			tt.setupFunc(registry)

			names := registry.GetToolNames()
			if len(names) != tt.expectCount {
				t.Errorf("expected %d names, got %d", tt.expectCount, len(names))
			}
			if tt.expectEmpty && len(names) != 0 {
				t.Error("expected empty names list")
			}

			// Verify names are strings, not tools
			for _, name := range names {
				if name == "" {
					t.Error("tool names should not be empty")
				}
			}

			if len(tt.expectNames) > 0 {
				for _, expected := range tt.expectNames {
					found := false
					for _, got := range names {
						if got == expected {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected name %q in list, not found", expected)
					}
				}
			}
		})
	}
}

// TestRegistryThreadSafety tests concurrent access to the registry
func TestRegistryThreadSafety(t *testing.T) {
	t.Run("concurrent RegisterTool and GetTool", func(t *testing.T) {
		registry := NewDefaultRegistry()
		const numGoroutines = 100

		var wg sync.WaitGroup
		errChan := make(chan error, numGoroutines*2)

		// Goroutines registering tools
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				tool := NewSimpleStubTool(fmt.Sprintf("tool_%d", idx), CategoryFile)
				err := registry.RegisterTool(tool)
				if err != nil {
					errChan <- fmt.Errorf("goroutine %d register error: %w", idx, err)
				}
			}(i)
		}

		// Goroutines reading tools
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				_, exists := registry.GetTool(fmt.Sprintf("tool_%d", idx))
				if exists && idx >= numGoroutines/2 {
					// Some tools may not be registered yet due to race
					// This is acceptable for thread safety test
				}
			}(i)
		}

		wg.Wait()
		close(errChan)

		// Check for errors
		for err := range errChan {
			t.Error(err)
		}

		// Final count should be numGoroutines
		count := registry.GetToolCount()
		if count != numGoroutines {
			t.Errorf("expected %d tools, got %d", numGoroutines, count)
		}
	})

	t.Run("concurrent ListTools", func(t *testing.T) {
		registry := NewDefaultRegistry()
		registry.RegisterTool(NewSimpleStubTool("initial", CategoryFile))

		const numGoroutines = 50
		var wg sync.WaitGroup

		// Multiple goroutines calling ListTools concurrently
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tools := registry.ListTools()
				// Just verify it doesn't panic and returns a slice
				if tools == nil {
					t.Error("ListTools returned nil")
				}
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent RegisterTool, GetTool, ListTools, UnregisterTool", func(t *testing.T) {
		registry := NewDefaultRegistry()
		const numGoroutines = 20
		var wg sync.WaitGroup
		errorCount := 0
		var mu sync.Mutex

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				toolName := fmt.Sprintf("concurrent_tool_%d", idx)

				// Register
				tool := NewSimpleStubTool(toolName, CategoryFile)
				if err := registry.RegisterTool(tool); err != nil {
					mu.Lock()
					errorCount++
					mu.Unlock()
				}

				// Get
				if _, exists := registry.GetTool(toolName); !exists {
					t.Logf("tool %s not found after registration", toolName)
				}

				// List
				tools := registry.ListTools()
				if tools == nil {
					mu.Lock()
					errorCount++
					mu.Unlock()
				}

				// Unregister (may fail if already removed)
				_ = registry.UnregisterTool(toolName)
			}(i)
		}

		wg.Wait()

		if errorCount > 0 {
			t.Errorf("encountered %d errors during concurrent operations", errorCount)
		}
	})

	t.Run("stress test with many tools", func(t *testing.T) {
		registry := NewDefaultRegistry()
		const numTools = 1000
		var wg sync.WaitGroup

		// Register all tools
		for i := 0; i < numTools; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				tool := NewSimpleStubTool(fmt.Sprintf("stress_tool_%d", idx), CategoryFile)
				_ = registry.RegisterTool(tool)
			}(i)
		}
		wg.Wait()

		// Concurrent reads
		var readWg sync.WaitGroup
		for i := 0; i < 100; i++ {
			readWg.Add(1)
			go func() {
				defer readWg.Done()
				_ = registry.ListTools()
				_ = registry.GetToolNames()
				_ = registry.GetToolCount()
				for j := 0; j < 10; j++ {
					idx := j % numTools
					registry.GetTool(fmt.Sprintf("stress_tool_%d", idx))
				}
			}()
		}
		readWg.Wait()

		// Verify count
		count := registry.GetToolCount()
		if count != numTools {
			t.Errorf("expected %d tools, got %d", numTools, count)
		}
	})
}

// TestRegistryClear tests the Clear method
func TestRegistryClear(t *testing.T) {
	t.Run("clear removes all tools", func(t *testing.T) {
		registry := NewDefaultRegistry()
		registry.RegisterTool(NewSimpleStubTool("tool_1", CategoryFile))
		registry.RegisterTool(NewSimpleStubTool("tool_2", CategorySearch))
		registry.RegisterTool(NewSimpleStubTool("tool_3", CategoryEdit))

		if registry.GetToolCount() != 3 {
			t.Fatal("setup failed: expected 3 tools")
		}

		registry.Clear()

		if registry.GetToolCount() != 0 {
			t.Errorf("expected 0 tools after clear, got %d", registry.GetToolCount())
		}
		if len(registry.ListTools()) != 0 {
			t.Error("expected empty list after clear")
		}
		if len(registry.GetToolNames()) != 0 {
			t.Error("expected empty names after clear")
		}
		if registry.HasTool("tool_1") {
			t.Error("tool should not exist after clear")
		}
	})
}

// TestRegistryHasTool tests the HasTool method
func TestRegistryHasTool(t *testing.T) {
	registry := NewDefaultRegistry()
	registry.RegisterTool(NewSimpleStubTool("exists", CategoryFile))

	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"existing tool", "exists", true},
		{"non-existing tool", "missing", false},
		{"empty name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.HasTool(tt.toolName)
			if result != tt.expected {
				t.Errorf("HasTool(%q) = %v, want %v", tt.toolName, result, tt.expected)
			}
		})
	}
}

// containsSubstring checks if haystack contains substring
func containsSubstring(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 ||
		len(haystack) > len(needle) && (haystack[:len(needle)] == needle ||
			haystack[len(haystack)-len(needle):] == needle ||
			func() bool {
				for i := 1; i <= len(haystack)-len(needle); i++ {
					if haystack[i:i+len(needle)] == needle {
						return true
					}
				}
				return false
			}()))
}
