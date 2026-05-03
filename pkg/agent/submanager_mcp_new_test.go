package agent

import (
	"errors"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestNewAgentMCPManager(t *testing.T) {
	mm := NewAgentMCPManager()

	if mm.GetManager() == nil {
		t.Error("should have default MCP manager")
	}
	if mm.GetToolsCache() != nil {
		t.Errorf("tools cache should be nil by default, got %v", mm.GetToolsCache())
	}
	if mm.IsInitialized() {
		t.Error("should not be initialized by default")
	}
	if mm.GetInitError() != nil {
		t.Errorf("init error should be nil by default, got %v", mm.GetInitError())
	}
}

func TestAgentMCPManager_Manager(t *testing.T) {
	mm := NewAgentMCPManager()

	mgr := mm.GetManager()
	if mgr == nil {
		t.Fatal("manager should not be nil")
	}

	// Replace manager
	newMgr := mm.GetManager()
	mm.SetManager(newMgr)
	if mm.GetManager() != newMgr {
		t.Error("should return replaced manager")
	}

	// Set to nil
	mm.SetManager(nil)
	if mm.GetManager() != nil {
		t.Error("should return nil after setting nil")
	}
}

func TestAgentMCPManager_ToolsCache(t *testing.T) {
	mm := NewAgentMCPManager()

	if mm.GetToolsCache() != nil {
		t.Error("tools cache should be nil by default")
	}

	tools := []api.Tool{
		{Type: "function", Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{Name: "tool1", Description: "First tool", Parameters: nil}},
		{Type: "function", Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{Name: "tool2", Description: "Second tool", Parameters: nil}},
	}
	mm.SetToolsCache(tools)
	got := mm.GetToolsCache()
	if len(got) != 2 {
		t.Errorf("GetToolsCache = %d, want 2", len(got))
	}
	if got[0].Function.Name != "tool1" {
		t.Errorf("first tool name = %q, want tool1", got[0].Function.Name)
	}

	// Replace with empty
	mm.SetToolsCache([]api.Tool{})
	if len(mm.GetToolsCache()) != 0 {
		t.Error("should be empty after setting empty slice")
	}

	// Set to nil
	mm.SetToolsCache(nil)
	if mm.GetToolsCache() != nil {
		t.Error("should be nil after setting nil")
	}
}

func TestAgentMCPManager_Initialized(t *testing.T) {
	mm := NewAgentMCPManager()

	if mm.IsInitialized() {
		t.Error("should not be initialized by default")
	}

	mm.SetInitialized(true)
	if !mm.IsInitialized() {
		t.Error("should be true after setting")
	}

	mm.SetInitialized(false)
	if mm.IsInitialized() {
		t.Error("should be false after resetting")
	}
}

func TestAgentMCPManager_InitError(t *testing.T) {
	mm := NewAgentMCPManager()

	if mm.GetInitError() != nil {
		t.Error("init error should be nil by default")
	}

	err := errors.New("connection refused")
	mm.SetInitError(err)
	if mm.GetInitError() != err {
		t.Error("should return set error")
	}

	// Clear error
	mm.SetInitError(nil)
	if mm.GetInitError() != nil {
		t.Error("should be nil after clearing")
	}
}

func TestAgentMCPManager_LockUnlockInit(t *testing.T) {
	mm := NewAgentMCPManager()

	// Lock and unlock should not panic
	mm.LockInit()
	mm.UnlockInit()
}

func TestAgentMCPManager_ConcurrentAccess(t *testing.T) {
	mm := NewAgentMCPManager()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			mm.SetInitialized(n%2 == 0)
			mm.SetInitError(errors.New("error " + string(rune('a'+n%26))))
			mm.SetToolsCache([]api.Tool{{Type: "function", Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{Name: "tool-" + string(rune('a'+n%26)), Description: "", Parameters: nil}}})
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			mm.IsInitialized()
			mm.GetInitError()
			mm.GetToolsCache()
			mm.GetManager()
		}()
	}
	wg.Wait()

	// Should not have panicked
	_ = mm.IsInitialized()
}

func TestAgentMCPManager_ConcurrentLockUnlock(t *testing.T) {
	mm := NewAgentMCPManager()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mm.LockInit()
			mm.UnlockInit()
		}()
	}
	wg.Wait()
}

func TestAgentMCPManager_AllFieldsDefault(t *testing.T) {
	mm := NewAgentMCPManager()

	// Verify all defaults are correct
	if mm.GetManager() == nil {
		t.Error("manager should not be nil")
	}
	if mm.GetToolsCache() != nil {
		t.Error("tools cache should be nil")
	}
	if mm.IsInitialized() {
		t.Error("should not be initialized")
	}
	if mm.GetInitError() != nil {
		t.Error("init error should be nil")
	}
}
