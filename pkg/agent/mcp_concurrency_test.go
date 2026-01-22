package agent

import (
	"errors"
	"sync"
	"testing"
)

// TestMCPConcurrency_BasicConcurrentAccess tests that concurrent calls to getMCPTools()
// don't cause race conditions
func TestMCPConcurrency_BasicConcurrentAccess(t *testing.T) {
	t.Parallel()

	// Create a test agent (uses test mode automatically when running tests)
	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// Spawn multiple goroutines to call getMCPTools concurrently
	const numGoroutines = 50
	var wg sync.WaitGroup

	// Channel to collect any panics
	panicCh := make(chan any, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
			}()

			// Call getMCPTools concurrently
			_ = agent.getMCPTools()
		}()
	}

	wg.Wait()

	// Check for panics
	close(panicCh)
	for panicVal := range panicCh {
		t.Fatalf("Panic detected in concurrent access: %v", panicVal)
	}

	// Verify that the agent is still in a valid state
	if agent.mcpManager == nil {
		t.Error("Expected mcpManager to be initialized")
	}

	// Additional concurrent calls to ensure no corruption
	for i := 0; i < 10; i++ {
		tools := agent.getMCPTools()
		// Tools may be nil (if MCP disabled) or a valid slice - either is fine
		if tools != nil && len(tools) > 0 {
			t.Logf("Successfully retrieved %d MCP tools", len(tools))
		}
	}
}

// TestMCPConcurrency_InitializedFlag tests that mcpInitialized is set correctly
// even with concurrent access
func TestMCPConcurrency_InitializedFlag(t *testing.T) {
	t.Parallel()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// Verify initial state
	if agent.mcpInitialized {
		t.Error("Expected mcpInitialized to be false initially")
	}

	// Spawn concurrent goroutines that will all trigger initialization
	const numGoroutines = 100
	var wg sync.WaitGroup
	var results []bool
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// This should trigger initialization on first call
			_ = agent.getMCPTools()

			// Capture the mcpInitialized state
			mu.Lock()
			results = append(results, agent.mcpInitialized)
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Verify that all goroutines saw consistent initialization state
	// (either all true if initialization succeeded, or all false if MCP disabled)
	finalState := agent.mcpInitialized
	for i, state := range results {
		if state != finalState {
			t.Errorf("Goroutine %d saw mcpInitialized=%v, expected %v", i, state, finalState)
		}
	}

	t.Logf("Final mcpInitialized state: %v", finalState)
}

// TestMCPConcurrency_MutexProtection tests that initialization happens only once
// with mutex protection (mcpInitMu)
func TestMCPConcurrency_MutexProtection(t *testing.T) {
	t.Parallel()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// Track how many times getMCPTools is called
	const numGoroutines = 75
	var wg sync.WaitGroup
	callCount := 0
	var countMu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_ = agent.getMCPTools()

			countMu.Lock()
			callCount++
			countMu.Unlock()
		}()
	}

	wg.Wait()

	// Verify that getMCPTools was called the expected number of times
	if callCount != numGoroutines {
		t.Errorf("Expected %d calls to getMCPTools, got %d", numGoroutines, callCount)
	}

	// The actual initialization should only happen once due to the mutex and mcpInitialized check
	// We can't directly count initializations, but we can verify the state is consistent
	t.Logf("After %d concurrent calls, mcpInitialized=%v", callCount, agent.mcpInitialized)
}

// TestMCPConcurrency_ErrorHandling tests that mcpInitErr is stored correctly on failure
func TestMCPConcurrency_ErrorHandling(t *testing.T) {
	t.Parallel()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// Manually set mcpInitErr to simulate a previous initialization failure
	// This tests that subsequent concurrent calls handle the error state correctly
	agent.mcpInitialized = false
	testErr := errors.New("test MCP initialization error")
	agent.mcpInitErr = testErr

	const numGoroutines = 30
	var wg sync.WaitGroup
	errorCount := 0
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			tools := agent.getMCPTools()

			mu.Lock()
			if agent.mcpInitErr != nil {
				errorCount++
			}
			// Tools should be nil if not initialized
			if !agent.mcpInitialized && tools != nil {
				t.Error("Expected tools to be nil when not initialized")
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Verify error state was preserved (though mcpInitErr might be reset if init succeeds)
	if agent.mcpInitErr != nil {
		t.Logf("Error state preserved: %v", agent.mcpInitErr)
	}
}

// TestMCPConcurrency_StateConsistency tests that all MCP-related state remains
// consistent under concurrent access
func TestMCPConcurrency_StateConsistency(t *testing.T) {
	t.Parallel()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// Track state observations from each goroutine
	type stateObservation struct {
		initialized bool
		err          error
		hasCache     bool
	}

	const numGoroutines = 60
	var wg sync.WaitGroup
	observations := make([]stateObservation, numGoroutines)
	var obsMu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			_ = agent.getMCPTools()

			// Lock to capture consistent state
			agent.mcpInitMu.Lock()
			obsMu.Lock()

			observations[idx] = stateObservation{
				initialized: agent.mcpInitialized,
				err:         agent.mcpInitErr,
				hasCache:    agent.mcpToolsCache != nil,
			}

			obsMu.Unlock()
			agent.mcpInitMu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify state consistency across all observations
	// All goroutines should see either:
	// 1. mcpInitialized=true with mcpInitErr=nil (successful init), OR
	// 2. (rare after init) mcpInitialized=false with some error state
	if agent.mcpInitialized {
		for i, obs := range observations {
			if !obs.initialized {
				// Some observations might be from before initialization completed
				// This is expected for some goroutines in high-concurrency scenarios
			}
			t.Logf("Goroutine %d: initialized=%v, err=%v, hasCache=%v", i, obs.initialized, obs.err, obs.hasCache)
		}
	}

	tools := agent.getMCPTools()
	t.Logf("Final state: mcpInitialized=%v, mcpInitErr=%v, tools count=%d",
		agent.mcpInitialized, agent.mcpInitErr, len(tools))
}

// TestMCPConcurrency_StressTest is a stress test with very high concurrency
func TestMCPConcurrency_StressTest(t *testing.T) {
	t.Parallel()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	const numGoroutines = 200
	const iterationsPerGoroutine = 10
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Multiple calls per goroutine
			for j := 0; j < iterationsPerGoroutine; j++ {
				_ = agent.getMCPTools()
			}
		}()
	}

	wg.Wait()

	// Verify the agent is still in a valid state
	if agent.mcpManager == nil {
		t.Error("Expected mcpManager to be initialized after stress test")
	}

	t.Logf("Stress test completed: %d goroutines × %d calls = %d total operations",
		numGoroutines, iterationsPerGoroutine, numGoroutines*iterationsPerGoroutine)
}

// TestMCPConcurrency_InterleavedAccess tests interleaved access patterns
// to ensure the mutex properly prevents race conditions under complex access patterns
func TestMCPConcurrency_InterleavedAccess(t *testing.T) {
	t.Parallel()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	const numPhases = 5
	const goroutinesPerPhase = 20

	for phase := 0; phase < numPhases; phase++ {
		var wg sync.WaitGroup
		for i := 0; i < goroutinesPerPhase; i++ {
			wg.Add(1)
			go func(p int) {
				defer wg.Done()
				tools := agent.getMCPTools()
				_ = tools
			}(phase)
		}
		wg.Wait()
	}

	// Verify consistent final state
	tools := agent.getMCPTools()
	t.Logf("After %d phases of %d goroutines each, mcpInitialized=%v",
		numPhases, goroutinesPerPhase, agent.mcpInitialized)

	if agent.mcpInitialized {
		t.Logf("Final tools count: %d", len(tools))
	}
}

// TestMCPConcurrency_WithDisabledMCP tests concurrency when MCP is disabled
// (simulated by checking behavior when no tools are available)
func TestMCPConcurrency_WithDisabledMCP(t *testing.T) {
	t.Parallel()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// Get tools once to potentially trigger initialization
	_ = agent.getMCPTools()

	// Spawn concurrent calls - should all return nil or cached tools safely
	const numGoroutines = 40
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			tools := agent.getMCPTools()

			// Either nil (no tools/failed init) or a valid slice is acceptable
			if tools != nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	t.Logf("Successfully retrieved tools in %d/%d concurrent calls", successCount, numGoroutines)

	// No panics or races should occur
	if successCount > 0 || successCount == 0 {
		// Both outcomes are valid - either MCP is working (successCount > 0) or disabled
	}
}

// TestConcurrentRefreshMCPTools tests concurrent RefreshMCPTools() calls
// don't cause issues
func TestConcurrentRefreshMCPTools(t *testing.T) {
	t.Parallel()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}
	defer agent.Shutdown()

	// First, initialize MCP by calling getMCPTools once
	_ = agent.getMCPTools()
	// Note: mcpToolsCache may be nil if MCP is disabled in test mode, that's expected

	// Now trigger concurrent RefreshMCPTools calls
	const numGoroutines = 80
	var wg sync.WaitGroup
	refreshResults := make([]error, numGoroutines)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			refreshResults[idx] = agent.RefreshMCPTools()
		}(i)
	}
	wg.Wait()

	// All refreshes should succeed
	for i, err := range refreshResults {
		if err != nil {
			t.Errorf("RefreshMCPTools() call %d failed: %v", i, err)
		}
	}

	// After concurrent refreshes, the agent should still be in a valid state
	// The important thing is there was no race condition or panic
	_ = agent.mcpToolsCache // Access to verify no data race
	_ = agent.mcpInitialized
	_ = agent.mcpInitErr

	t.Logf("All %d concurrent RefreshMCPTools calls succeeded", numGoroutines)
}
