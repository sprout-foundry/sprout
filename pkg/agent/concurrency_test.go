package agent

import (
	"fmt"
	"sync"
	"testing"
)

// TestMCPConcurrency_InitInvariant is a fast regression test that pins the
// MCP-init concurrency invariant. It validates the SP-028 Phase 2 deadlock fix:
// sync.Once (via DoInit) ensures initializeMCP() runs exactly once, while
// sync.RWMutex protects the tools cache for the RLock fast-path reads.
//
// 16 goroutines contend on getMCPTools() in a single phase, verifying that
// no panics, races, or state corruption occur.
func TestMCPConcurrency_InitInvariant(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}
	t.Cleanup(func() { agent.Shutdown() })

	// Record state BEFORE any concurrent access
	initBefore := agent.mcpSub.IsInitialized()

	// Single phase: 16 goroutines all contend on getMCPTools simultaneously
	const goroutines = 16
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errs <- fmt.Errorf("panic: %v", r)
				}
			}()
			_ = agent.getMCPTools()
		}()
	}

	wg.Wait()
	close(errs)

	// No panics or errors should have occurred
	for err := range errs {
		t.Errorf("Concurrent access error: %v", err)
	}

	// After concurrent init, state must be consistent
	initAfter := agent.mcpSub.IsInitialized()
	managerNil := agent.mcpSub.GetManager() == nil

	// Invariant: if initialization ran (state changed), manager must not be nil
	if initBefore != initAfter && managerNil {
		t.Error("MCP manager is nil after initialization completed")
	}

	// Invariant: manager should never be nil (NewAgentMCPManager always sets it)
	if managerNil {
		t.Error("MCP manager unexpectedly nil after concurrent access")
	}

	// Additional reads after init to confirm fast path is stable
	tools := agent.getMCPTools()
	_ = tools

	t.Logf("Init: %v → %v, manager: %v", initBefore, initAfter, !managerNil)
}
