package agent

import (
	"sync"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// --- Test Helpers ---

func newTestToolExecutorWithCircuitBreaker() *ToolExecutor {
	cb := &CircuitBreakerState{Actions: make(map[string]*CircuitBreakerAction)}
	state := &AgentStateManager{
		AgentSessionManager:        NewAgentSessionManager(false),
		AgentMetricsManager:        NewAgentMetricsManager(),
		AgentPersonaManager:       NewAgentPersonaManager(),
		AgentSecurityStateManager: &AgentSecurityStateManager{circuitBreaker: cb},
	}
	return &ToolExecutor{agent: &Agent{state: state}}
}

func newTestToolExecutorWithCircuitBreakerAndProvider(provider string) *ToolExecutor {
	cb := &CircuitBreakerState{Actions: make(map[string]*CircuitBreakerAction)}
	state := &AgentStateManager{
		AgentSessionManager:        NewAgentSessionManager(false),
		AgentMetricsManager:        NewAgentMetricsManager(),
		AgentPersonaManager:       NewAgentPersonaManager(),
		AgentSecurityStateManager: &AgentSecurityStateManager{circuitBreaker: cb},
	}
	if provider != "" {
		state.SetSessionProvider(api.ClientType(provider))
	}
	return &ToolExecutor{agent: &Agent{state: state}}
}

func newTestToolExecutorNilState() *ToolExecutor {
	return &ToolExecutor{agent: &Agent{}}
}

func newTestToolExecutorNilCircuitBreaker() *ToolExecutor {
	state := &AgentStateManager{
		AgentSessionManager:        NewAgentSessionManager(false),
		AgentMetricsManager:        NewAgentMetricsManager(),
		AgentPersonaManager:       NewAgentPersonaManager(),
		AgentSecurityStateManager: &AgentSecurityStateManager{circuitBreaker: nil},
	}
	return &ToolExecutor{agent: &Agent{state: state}}
}

func updateCircuitBreakerCount(te *ToolExecutor, toolName string, args map[string]interface{}) {
	te.updateCircuitBreaker(toolName, args)
}

// --- TestGenerateActionKey ---

func TestGenerateActionKey(t *testing.T) {
	te := &ToolExecutor{}

	t.Run("simple args produces deterministic key", func(t *testing.T) {
		key := te.generateActionKey("read_file", map[string]interface{}{
			"path": "foo.txt",
		})
		// json.Marshal sorts map keys alphabetically, so key is deterministic
		want := `read_file:{"path":"foo.txt"}`
		if key != want {
			t.Errorf("generateActionKey() = %q, want %q", key, want)
		}
	})

	t.Run("same args always produce same key", func(t *testing.T) {
		args := map[string]interface{}{
			"a": 1,
			"b": "hello",
		}
		k1 := te.generateActionKey("my_tool", args)
		k2 := te.generateActionKey("my_tool", args)
		if k1 != k2 {
			t.Errorf("generateActionKey is not deterministic: %q != %q", k1, k2)
		}
	})

	t.Run("empty args", func(t *testing.T) {
		key := te.generateActionKey("tool_name", map[string]interface{}{})
		want := `tool_name:{}`
		if key != want {
			t.Errorf("generateActionKey() = %q, want %q", key, want)
		}
	})

	t.Run("nil args", func(t *testing.T) {
		key := te.generateActionKey("tool_name", nil)
		want := `tool_name:null`
		if key != want {
			t.Errorf("generateActionKey() = %q, want %q", key, want)
		}
	})

	t.Run("args with special characters", func(t *testing.T) {
		key := te.generateActionKey("shell_command", map[string]interface{}{
			"command": "echo 'hello \"world\"' && cat /etc/passwd",
		})
		// Just verify it doesn't panic and includes the tool name
		if len(key) == 0 {
			t.Error("generateActionKey returned empty key for special chars")
		}
	})

	t.Run("different tool names produce different keys", func(t *testing.T) {
		args := map[string]interface{}{"x": 1}
		k1 := te.generateActionKey("read_file", args)
		k2 := te.generateActionKey("edit_file", args)
		if k1 == k2 {
			t.Errorf("different tool names produced same key: %q", k1)
		}
	})

	t.Run("different args produce different keys", func(t *testing.T) {
		k1 := te.generateActionKey("read_file", map[string]interface{}{
			"path": "a.txt",
		})
		k2 := te.generateActionKey("read_file", map[string]interface{}{
			"path": "b.txt",
		})
		if k1 == k2 {
			t.Errorf("different args produced same key: %q", k1)
		}
	})

	t.Run("multiple args with different types", func(t *testing.T) {
		key := te.generateActionKey("tool", map[string]interface{}{
			"str":  "hello",
			"int":  42,
			"bool": true,
		})
		if len(key) == 0 {
			t.Error("generateActionKey returned empty key for mixed-type args")
		}
		// Determinism check
		key2 := te.generateActionKey("tool", map[string]interface{}{
			"str":  "hello",
			"int":  42,
			"bool": true,
		})
		if key != key2 {
			t.Errorf("mixed-type keys not deterministic: %q != %q", key, key2)
		}
	})

	t.Run("args order independence", func(t *testing.T) {
		// json.Marshal sorts map keys, so insertion order should not matter
		a := map[string]interface{}{}
		a["z"] = 1
		a["a"] = 2

		b := map[string]interface{}{}
		b["a"] = 2
		b["z"] = 1

		k1 := te.generateActionKey("tool", a)
		k2 := te.generateActionKey("tool", b)
		if k1 != k2 {
			t.Errorf("arg order affected key: %q != %q", k1, k2)
		}
	})
}

// --- TestCheckCircuitBreaker ---

func TestCheckCircuitBreaker(t *testing.T) {
	t.Run("no action returns false", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		got := te.checkCircuitBreaker("read_file", map[string]interface{}{
			"path": "test.txt",
		})
		if got {
			t.Error("checkCircuitBreaker should return false when no actions exist")
		}
	})

	t.Run("nil state returns false", func(t *testing.T) {
		te := newTestToolExecutorNilState()
		got := te.checkCircuitBreaker("read_file", map[string]interface{}{
			"path": "test.txt",
		})
		if got {
			t.Error("checkCircuitBreaker should return false when state is nil")
		}
	})

	t.Run("nil circuit breaker returns false", func(t *testing.T) {
		te := newTestToolExecutorNilCircuitBreaker()
		got := te.checkCircuitBreaker("read_file", map[string]interface{}{
			"path": "test.txt",
		})
		if got {
			t.Error("checkCircuitBreaker should return false when circuit breaker is nil")
		}
	})

	t.Run("default threshold is 3", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"cmd": "ls"}

		// At count 2, should not block (threshold is 3)
		updateCircuitBreakerCount(te, "some_tool", args)
		updateCircuitBreakerCount(te, "some_tool", args)
		if te.checkCircuitBreaker("some_tool", args) {
			t.Error("should not block at count 2 (threshold 3)")
		}

		// At count 3, should block
		updateCircuitBreakerCount(te, "some_tool", args)
		if !te.checkCircuitBreaker("some_tool", args) {
			t.Error("should block at count 3 (threshold 3)")
		}
	})

	t.Run("read_file threshold is 5 (non-zai provider)", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"path": "file.txt"}

		// At count 4, should not block (threshold is 5)
		for i := 0; i < 4; i++ {
			updateCircuitBreakerCount(te, "read_file", args)
		}
		if te.checkCircuitBreaker("read_file", args) {
			t.Error("read_file should not block at count 4 (threshold 5)")
		}

		// At count 5, should block
		updateCircuitBreakerCount(te, "read_file", args)
		if !te.checkCircuitBreaker("read_file", args) {
			t.Error("read_file should block at count 5 (threshold 5)")
		}
	})

	t.Run("read_file threshold is 3 for zai provider", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreakerAndProvider("zai")
		args := map[string]interface{}{"path": "file.txt"}

		// At count 2, should not block
		updateCircuitBreakerCount(te, "read_file", args)
		updateCircuitBreakerCount(te, "read_file", args)
		if te.checkCircuitBreaker("read_file", args) {
			t.Error("read_file (zai) should not block at count 2 (threshold 3)")
		}

		// At count 3, should block
		updateCircuitBreakerCount(te, "read_file", args)
		if !te.checkCircuitBreaker("read_file", args) {
			t.Error("read_file (zai) should block at count 3 (threshold 3)")
		}
	})

	t.Run("shell_command threshold is 8", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"command": "ls -la"}

		// At count 7, should not block
		for i := 0; i < 7; i++ {
			updateCircuitBreakerCount(te, "shell_command", args)
		}
		if te.checkCircuitBreaker("shell_command", args) {
			t.Error("shell_command should not block at count 7 (threshold 8)")
		}

		// At count 8, should block
		updateCircuitBreakerCount(te, "shell_command", args)
		if !te.checkCircuitBreaker("shell_command", args) {
			t.Error("shell_command should block at count 8 (threshold 8)")
		}
	})

	t.Run("edit_file threshold is 4", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{
			"path":    "file.go",
			"old_str": "foo",
			"new_str": "bar",
		}

		// At count 3, should not block
		for i := 0; i < 3; i++ {
			updateCircuitBreakerCount(te, "edit_file", args)
		}
		if te.checkCircuitBreaker("edit_file", args) {
			t.Error("edit_file should not block at count 3 (threshold 4)")
		}

		// At count 4, should block
		updateCircuitBreakerCount(te, "edit_file", args)
		if !te.checkCircuitBreaker("edit_file", args) {
			t.Error("edit_file should block at count 4 (threshold 4)")
		}
	})

	t.Run("different actions are independent", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args1 := map[string]interface{}{"path": "a.txt"}
		args2 := map[string]interface{}{"path": "b.txt"}

		// Fill up action 1 to threshold
		for i := 0; i < 3; i++ {
			updateCircuitBreakerCount(te, "some_tool", args1)
		}

		// Action 1 should be blocked
		if !te.checkCircuitBreaker("some_tool", args1) {
			t.Error("action 1 should be blocked")
		}

		// Action 2 should not be blocked (only 1 update)
		updateCircuitBreakerCount(te, "some_tool", args2)
		if te.checkCircuitBreaker("some_tool", args2) {
			t.Error("action 2 should not be blocked (count 1)")
		}
	})

	t.Run("different tools are independent", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"x": 1}

		// Fill up tool_a to threshold
		for i := 0; i < 3; i++ {
			updateCircuitBreakerCount(te, "tool_a", args)
		}

		// tool_a blocked, tool_b not
		if !te.checkCircuitBreaker("tool_a", args) {
			t.Error("tool_a should be blocked")
		}
		if te.checkCircuitBreaker("tool_b", args) {
			t.Error("tool_b should not be blocked")
		}
	})
}

// --- TestUpdateCircuitBreaker ---

func TestUpdateCircuitBreaker(t *testing.T) {
	t.Run("creates new action on first update", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"path": "test.txt"}

		te.updateCircuitBreaker("read_file", args)

		cb := te.agent.state.GetCircuitBreaker()
		cb.mu.RLock()
		defer cb.mu.RUnlock()

		key := te.generateActionKey("read_file", args)
		action, ok := cb.Actions[key]
		if !ok {
			t.Fatal("action not found in circuit breaker state")
		}
		if action.Count != 1 {
			t.Errorf("expected count 1, got %d", action.Count)
		}
		if action.ActionType != "read_file" {
			t.Errorf("expected action type 'read_file', got %q", action.ActionType)
		}
		if action.LastUsed == 0 {
			t.Error("expected LastUsed to be set")
		}
	})

	t.Run("increments count on repeated updates", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"path": "test.txt"}

		for i := 0; i < 5; i++ {
			te.updateCircuitBreaker("read_file", args)
		}

		cb := te.agent.state.GetCircuitBreaker()
		cb.mu.RLock()
		defer cb.mu.RUnlock()

		key := te.generateActionKey("read_file", args)
		action := cb.Actions[key]
		if action.Count != 5 {
			t.Errorf("expected count 5, got %d", action.Count)
		}
	})

	t.Run("updates LastUsed on each call", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"path": "test.txt"}

		te.updateCircuitBreaker("read_file", args)
		cb := te.agent.state.GetCircuitBreaker()
		cb.mu.RLock()
		key := te.generateActionKey("read_file", args)
		firstUsed := cb.Actions[key].LastUsed
		cb.mu.RUnlock()

		time.Sleep(1 * time.Second)

		te.updateCircuitBreaker("read_file", args)
		cb.mu.RLock()
		secondUsed := cb.Actions[key].LastUsed
		cb.mu.RUnlock()

		if secondUsed <= firstUsed {
			t.Errorf("LastUsed should increase; first=%d, second=%d", firstUsed, secondUsed)
		}
	})

	t.Run("nil state is handled", func(t *testing.T) {
		te := newTestToolExecutorNilState()
		// Should not panic
		te.updateCircuitBreaker("read_file", map[string]interface{}{"path": "test.txt"})
	})

	t.Run("nil circuit breaker is handled", func(t *testing.T) {
		te := newTestToolExecutorNilCircuitBreaker()
		// Should not panic
		te.updateCircuitBreaker("read_file", map[string]interface{}{"path": "test.txt"})
	})
}

// --- TestCleanupOldCircuitBreakerEntries ---

func TestCleanupOldCircuitBreakerEntries(t *testing.T) {
	t.Run("removes entries older than 5 minutes", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		cb := te.agent.state.GetCircuitBreaker()

		// Manually insert an old entry (10 minutes ago)
		oldTime := getCurrentTime() - 600 // 10 minutes ago
		cb.mu.Lock()
		cb.Actions["old_action"] = &CircuitBreakerAction{
			ActionType: "some_tool",
			Target:     "old_action",
			Count:      1,
			LastUsed:   oldTime,
		}
		// Insert a recent entry
		cb.Actions["recent_action"] = &CircuitBreakerAction{
			ActionType: "some_tool",
			Target:     "recent_action",
			Count:      1,
			LastUsed:   getCurrentTime(),
		}
		cb.mu.Unlock()

		// Run cleanup
		te.cleanupOldCircuitBreakerEntries()

		cb.mu.RLock()
		defer cb.mu.RUnlock()

		if _, ok := cb.Actions["old_action"]; ok {
			t.Error("old entry should have been cleaned up")
		}
		if _, ok := cb.Actions["recent_action"]; !ok {
			t.Error("recent entry should still exist")
		}
	})

	t.Run("cleanupLocked with entries exactly at threshold", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		cb := te.agent.state.GetCircuitBreaker()

		currentTime := getCurrentTime()
		fiveMinutesAgo := currentTime - 300

		cb.mu.Lock()
		// Exactly at the boundary: NOT removed since cleanup uses strict < comparison
		cb.Actions["boundary"] = &CircuitBreakerAction{
			ActionType: "tool",
			Target:     "boundary",
			Count:      1,
			LastUsed:   fiveMinutesAgo,
		}
		// Well past the boundary: should be removed
		cb.Actions["old"] = &CircuitBreakerAction{
			ActionType: "tool",
			Target:     "old",
			Count:      1,
			LastUsed:   fiveMinutesAgo - 10,
		}
		cb.mu.Unlock()

		cb.mu.Lock()
		te.cleanupOldCircuitBreakerEntriesLocked()
		cb.mu.Unlock()

		cb.mu.RLock()
		boundaryExists := cb.Actions["boundary"] != nil
		oldExists := cb.Actions["old"] != nil
		cb.mu.RUnlock()

		if !boundaryExists {
			t.Error("entry at exactly 5 minutes ago should still exist (< comparison, not <=)")
		}
		if oldExists {
			t.Error("entry well past 5 minutes should be removed")
		}
	})

	t.Run("nil circuit breaker handled", func(t *testing.T) {
		te := newTestToolExecutorNilCircuitBreaker()
		// This will panic because cleanupOldCircuitBreakerEntries calls
		// GetCircuitBreaker() without nil check first... actually let's check
		// the code: it does check == nil. But newTestToolExecutorNilCircuitBreaker
		// has state != nil but circuitBreaker == nil, so it returns early.
		te.cleanupOldCircuitBreakerEntries()
	})
}

// --- Integration: Check + Update ---

func TestCheckAndUpdateIntegration(t *testing.T) {
	t.Run("check returns false before any update", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"path": "file.txt"}

		if te.checkCircuitBreaker("read_file", args) {
			t.Error("should not block before any update")
		}
	})

	t.Run("check becomes true after threshold updates", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"cmd": "echo hello"}

		// Default threshold is 3
		for i := 0; i < 3; i++ {
			if te.checkCircuitBreaker("some_tool", args) {
				t.Errorf("should not block at count %d", i)
			}
			te.updateCircuitBreaker("some_tool", args)
		}

		// Now should block
		if !te.checkCircuitBreaker("some_tool", args) {
			t.Error("should block after 3 updates")
		}
	})

	t.Run("shell_command allows more iterations before blocking", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"command": "make build"}

		// At 7 iterations, still not blocked
		for i := 0; i < 7; i++ {
			te.updateCircuitBreaker("shell_command", args)
		}
		if te.checkCircuitBreaker("shell_command", args) {
			t.Error("shell_command should not block at count 7")
		}

		// At 8, blocked
		te.updateCircuitBreaker("shell_command", args)
		if !te.checkCircuitBreaker("shell_command", args) {
			t.Error("shell_command should block at count 8")
		}
	})

	t.Run("concurrent updates and checks", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{"cmd": "ls"}

		var wg sync.WaitGroup
		// 10 goroutines each updating 5 times = 50 updates total
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 5; j++ {
					te.updateCircuitBreaker("some_tool", args)
				}
			}()
		}
		// Meanwhile, some goroutines reading
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					te.checkCircuitBreaker("some_tool", args)
				}
			}()
		}
		wg.Wait()

		cb := te.agent.state.GetCircuitBreaker()
		cb.mu.RLock()
		key := te.generateActionKey("some_tool", args)
		action := cb.Actions[key]
		cb.mu.RUnlock()

		if action == nil {
			t.Fatal("action should exist after concurrent updates")
		}
		if action.Count != 50 {
			t.Errorf("expected count 50, got %d", action.Count)
		}
	})
}

// --- Edge Cases ---

func TestCircuitBreakerEdgeCases(t *testing.T) {
	t.Run("args with nested maps", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{
			"config": map[string]interface{}{
				"key": "value",
			},
		}

		key := te.generateActionKey("tool", args)
		if key == "" {
			t.Error("key should not be empty for nested args")
		}

		// Determinism check
		key2 := te.generateActionKey("tool", map[string]interface{}{
			"config": map[string]interface{}{
				"key": "value",
			},
		})
		if key != key2 {
			t.Errorf("nested args not deterministic: %q != %q", key, key2)
		}
	})

	t.Run("args with arrays", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		args := map[string]interface{}{
			"files": []interface{}{"a.txt", "b.txt"},
		}

		key := te.generateActionKey("tool", args)
		if key == "" {
			t.Error("key should not be empty for array args")
		}
	})

	t.Run("empty tool name", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		key := te.generateActionKey("", map[string]interface{}{})
		if key != ":{}" {
			t.Errorf("empty tool name key = %q, want ':{}'", key)
		}
	})

	t.Run("read_file zai threshold - provider check via session provider", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		// Set session provider to "zai"
		te.agent.state.SetSessionProvider("zai")

		args := map[string]interface{}{"path": "file.txt"}

		// With zai, threshold is 3
		for i := 0; i < 2; i++ {
			updateCircuitBreakerCount(te, "read_file", args)
		}
		if te.checkCircuitBreaker("read_file", args) {
			t.Error("read_file (zai) should not block at count 2")
		}

		updateCircuitBreakerCount(te, "read_file", args)
		if !te.checkCircuitBreaker("read_file", args) {
			t.Error("read_file (zai) should block at count 3")
		}
	})

	t.Run("read_file non-zai provider - threshold is 5", func(t *testing.T) {
		te := newTestToolExecutorWithCircuitBreaker()
		// Set session provider to something other than "zai"
		te.agent.state.SetSessionProvider("openai")

		args := map[string]interface{}{"path": "file.txt"}

		// With openai, threshold is 5
		for i := 0; i < 4; i++ {
			updateCircuitBreakerCount(te, "read_file", args)
		}
		if te.checkCircuitBreaker("read_file", args) {
			t.Error("read_file (openai) should not block at count 4")
		}

		updateCircuitBreakerCount(te, "read_file", args)
		if !te.checkCircuitBreaker("read_file", args) {
			t.Error("read_file (openai) should block at count 5")
		}
	})
}
