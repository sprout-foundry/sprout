package agent

import (
	"fmt"
	"sync"
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ---------------------------------------------------------------------------
// recordSecurityBlock / getSecurityBlockCount / clearSecurityBlock
// ---------------------------------------------------------------------------

func TestRecordSecurityBlock_Increments(t *testing.T) {
	t.Parallel()

	a := &Agent{state: NewAgentStateManager(false)}
	args := map[string]interface{}{"command": "rm -rf /tmp"}

	// First block → count 1
	if got := a.recordSecurityBlock("shell_command", args); got != 1 {
		t.Errorf("first recordSecurityBlock = %d, want 1", got)
	}
	if got := a.getSecurityBlockCount("shell_command", args); got != 1 {
		t.Errorf("getSecurityBlockCount = %d, want 1", got)
	}

	// Second block → count 2
	if got := a.recordSecurityBlock("shell_command", args); got != 2 {
		t.Errorf("second recordSecurityBlock = %d, want 2", got)
	}

	// Third block → count 3
	if got := a.recordSecurityBlock("shell_command", args); got != 3 {
		t.Errorf("third recordSecurityBlock = %d, want 3", got)
	}
}

func TestRecordSecurityBlock_DifferentArgsTrackedSeparately(t *testing.T) {
	t.Parallel()

	a := &Agent{state: NewAgentStateManager(false)}
	args1 := map[string]interface{}{"command": "rm -rf /tmp"}
	args2 := map[string]interface{}{"command": "rm -rf /home"}

	c1 := a.recordSecurityBlock("shell_command", args1)
	c2 := a.recordSecurityBlock("shell_command", args2)

	if c1 != 1 || c2 != 1 {
		t.Errorf("different args should be tracked separately: c1=%d c2=%d, want both 1", c1, c2)
	}

	// Block args1 again — args2 should still be 1.
	c1b := a.recordSecurityBlock("shell_command", args1)
	if c1b != 2 {
		t.Errorf("second block of args1 = %d, want 2", c1b)
	}
	if got := a.getSecurityBlockCount("shell_command", args2); got != 1 {
		t.Errorf("args2 count after blocking args1 twice = %d, want 1", got)
	}
}

func TestRecordSecurityBlock_NilStateReturnsZero(t *testing.T) {
	t.Parallel()

	a := &Agent{} // no state
	if got := a.recordSecurityBlock("shell_command", nil); got != 0 {
		t.Errorf("recordSecurityBlock with nil state = %d, want 0", got)
	}
}

func TestClearSecurityBlock_Resets(t *testing.T) {
	t.Parallel()

	a := &Agent{state: NewAgentStateManager(false)}
	args := map[string]interface{}{"command": "rm -rf /tmp"}

	a.recordSecurityBlock("shell_command", args)
	a.recordSecurityBlock("shell_command", args)
	if got := a.getSecurityBlockCount("shell_command", args); got != 2 {
		t.Fatalf("count before clear = %d, want 2", got)
	}

	a.clearSecurityBlock("shell_command", args)
	if got := a.getSecurityBlockCount("shell_command", args); got != 0 {
		t.Errorf("count after clear = %d, want 0", got)
	}

	// After clearing, next block starts at 1 again.
	if got := a.recordSecurityBlock("shell_command", args); got != 1 {
		t.Errorf("first block after clear = %d, want 1", got)
	}
}

func TestClearSecurityBlock_NilStateNoPanic(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	a.clearSecurityBlock("shell_command", nil) // must not panic
}

// ---------------------------------------------------------------------------
// generateSecurityBlockKey — namespace isolation
// ---------------------------------------------------------------------------

func TestGenerateSecurityBlockKey_Namespace(t *testing.T) {
	t.Parallel()

	args := map[string]interface{}{"command": "ls"}
	key := generateSecurityBlockKey("shell_command", args)

	// Must start with "sec:" to avoid collision with general circuit breaker.
	if key[:4] != "sec:" {
		t.Errorf("key %q does not start with 'sec:'", key)
	}
}

// ---------------------------------------------------------------------------
// wrapSecurityCautionWithLoop — loop detection (Task 1)
// ---------------------------------------------------------------------------

func TestWrapSecurityCautionWithLoop_StandardCaution(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	args := map[string]interface{}{"command": "rm -rf /tmp"}
	err := agenterrors.NewSecurityError("security hard block: test — critical", nil)

	wrapped := wrapSecurityCautionWithLoop(a, err, "shell_command", args)
	if !agenterrors.IsSecurity(wrapped) {
		t.Fatal("expected security error")
	}
	if !contains(wrapped.Error(), "SECURITY_CAUTION_REQUIRED") {
		t.Errorf("standard caution should contain SECURITY_CAUTION_REQUIRED: %s", wrapped.Error())
	}
	if contains(wrapped.Error(), "LOOP_DETECTED") {
		t.Errorf("first block should NOT contain LOOP_DETECTED: %s", wrapped.Error())
	}
}

func TestWrapSecurityCautionWithLoop_LoopDetected(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	args := map[string]interface{}{"command": "rm -rf /tmp"}

	// Block 1: standard caution
	err := agenterrors.NewSecurityError("security hard block: test — critical", nil)
	_ = wrapSecurityCautionWithLoop(a, err, "shell_command", args)

	// Block 2: standard caution (retry-after-caution)
	_ = wrapSecurityCautionWithLoop(a, err, "shell_command", args)

	// Block 3: loop detected (count >= threshold of 2)
	wrapped3 := wrapSecurityCautionWithLoop(a, err, "shell_command", args)
	if !contains(wrapped3.Error(), "SECURITY_CAUTION_LOOP_DETECTED") {
		t.Errorf("third block should contain SECURITY_CAUTION_LOOP_DETECTED: %s", wrapped3.Error())
	}
	if !contains(wrapped3.Error(), "blocked 3 times") {
		t.Errorf("loop message should mention 'blocked 3 times': %s", wrapped3.Error())
	}
}

func TestWrapSecurityCautionWithLoop_DifferentArgsNoLoop(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	err := agenterrors.NewSecurityError("security hard block: test — critical", nil)

	// Block args1 twice (should not loop)
	args1 := map[string]interface{}{"command": "rm -rf /tmp"}
	_ = wrapSecurityCautionWithLoop(a, err, "shell_command", args1)
	_ = wrapSecurityCautionWithLoop(a, err, "shell_command", args1)

	// Block args2 (different args) — should be standard caution, not loop
	args2 := map[string]interface{}{"command": "rm -rf /home"}
	wrapped := wrapSecurityCautionWithLoop(a, err, "shell_command", args2)
	if contains(wrapped.Error(), "LOOP_DETECTED") {
		t.Errorf("different args should NOT trigger loop: %s", wrapped.Error())
	}
}

func TestWrapSecurityCautionWithLoop_TelemetryIncrements(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	args := map[string]interface{}{"command": "rm -rf /tmp"}
	err := agenterrors.NewSecurityError("security hard block: test — critical", nil)

	// Block 1: cautions=1, retries=0, loops=0
	_ = wrapSecurityCautionWithLoop(a, err, "shell_command", args)
	if got := a.GetSecurityCautionsIssued(); got != 1 {
		t.Errorf("after block 1: cautions=%d, want 1", got)
	}
	if got := a.GetSecurityRetriesAfterCaution(); got != 0 {
		t.Errorf("after block 1: retries=%d, want 0", got)
	}

	// Block 2: cautions=2, retries=1, loops=0
	_ = wrapSecurityCautionWithLoop(a, err, "shell_command", args)
	if got := a.GetSecurityCautionsIssued(); got != 2 {
		t.Errorf("after block 2: cautions=%d, want 2", got)
	}
	if got := a.GetSecurityRetriesAfterCaution(); got != 1 {
		t.Errorf("after block 2: retries=%d, want 1", got)
	}

	// Block 3: cautions=3, retries=1, loops=1
	_ = wrapSecurityCautionWithLoop(a, err, "shell_command", args)
	if got := a.GetSecurityLoopsDetected(); got != 1 {
		t.Errorf("after block 3: loops=%d, want 1", got)
	}
}

func TestWrapSecurityCautionWithLoop_NilAgent(t *testing.T) {
	// Must not panic with nil agent.
	err := agenterrors.NewSecurityError("security hard block: test — critical", nil)
	wrapped := wrapSecurityCautionWithLoop(nil, err, "shell_command", nil)
	if !agenterrors.IsSecurity(wrapped) {
		t.Fatal("expected security error even with nil agent")
	}
}

func TestWrapSecurityCautionWithLoop_NonSecurityErrorUnchanged(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	orig := fmt.Errorf("some other error")
	wrapped := wrapSecurityCautionWithLoop(a, orig, "shell_command", nil)
	if wrapped != orig {
		t.Error("non-security error should be returned unchanged")
	}
}

// ---------------------------------------------------------------------------
// Concurrency — thread safety
// ---------------------------------------------------------------------------

func TestRecordSecurityBlock_ConcurrentSafe(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	args := map[string]interface{}{"command": "rm -rf /tmp"}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// All goroutines increment the same key concurrently.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			a.recordSecurityBlock("shell_command", args)
		}()
	}
	wg.Wait()

	if got := a.getSecurityBlockCount("shell_command", args); got != goroutines {
		t.Errorf("after %d concurrent increments: count=%d, want %d", goroutines, got, goroutines)
	}
}

func TestRecordSecurityBlock_ConcurrentDifferentKeys(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}

	const goroutines = 20
	const perGoroutine = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Each goroutine tracks a different key.
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			args := map[string]interface{}{"command": fmt.Sprintf("cmd-%d", id)}
			for j := 0; j < perGoroutine; j++ {
				a.recordSecurityBlock("shell_command", args)
			}
		}(i)
	}
	wg.Wait()

	// Verify each key has exactly perGoroutine blocks.
	for i := 0; i < goroutines; i++ {
		args := map[string]interface{}{"command": fmt.Sprintf("cmd-%d", i)}
		if got := a.getSecurityBlockCount("shell_command", args); got != perGoroutine {
			t.Errorf("key %d: count=%d, want %d", i, got, perGoroutine)
		}
	}
}
