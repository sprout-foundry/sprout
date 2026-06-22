package agent

import (
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/security"
)

func TestNewAgentSecurityManager(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.GetSecurityApprovalMgr() == nil {
		t.Error("should have default security approval manager")
	}
	if sm.GetUnsafeMode() {
		t.Error("unsafe mode should be false by default")
	}
	if sm.IsSecurityBypassApproved() {
		t.Error("security bypass should not be approved by default")
	}
	if sm.GetOutputRedactor() == nil {
		t.Error("should have default output redactor")
	}
	if sm.GetElevationGate() == nil {
		t.Error("should have default elevation gate")
	}
	if sm.HasActiveWebUIClients() {
		t.Error("should return false when no callback is set")
	}
}

func TestAgentSecurityManager_SecurityApprovalMgr(t *testing.T) {
	sm := NewAgentSecurityManager()

	mgr := sm.GetSecurityApprovalMgr()
	if mgr == nil {
		t.Fatal("manager should not be nil")
	}

	// Replace
	newMgr := security.NewApprovalManager()
	sm.securityApprovalMgr = newMgr
	if sm.GetSecurityApprovalMgr() != newMgr {
		t.Error("should return replaced manager")
	}
}

func TestAgentSecurityManager_UnsafeMode(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetUnsafeMode(true)
	if !sm.GetUnsafeMode() {
		t.Error("should return true after setting")
	}

	sm.SetUnsafeMode(false)
	if sm.GetUnsafeMode() {
		t.Error("should return false after resetting")
	}
}

func TestAgentSecurityManager_SecurityBypass(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.IsSecurityBypassApproved() {
		t.Error("should not be approved by default")
	}

	// Adding any folder to the session allowlist flips
	// IsSecurityBypassApproved to true (coarse "user consented to
	// some external access" signal).
	sm.AddSessionAllowedFolder("/tmp/foo")
	if !sm.IsSecurityBypassApproved() {
		t.Error("should be approved after adding a folder")
	}

	// Adding the same folder again is a no-op (dedup).
	sm.AddSessionAllowedFolder("/tmp/foo")
	if !sm.IsSecurityBypassApproved() {
		t.Error("should remain approved")
	}
	if got := len(sm.SnapshotSessionAllowedFolders()); got != 1 {
		t.Errorf("expected 1 folder after dup add, got %d", got)
	}
}

func TestAgentSecurityManager_OutputRedactor(t *testing.T) {
	sm := NewAgentSecurityManager()

	redactor := sm.GetOutputRedactor()
	if redactor == nil {
		t.Error("output redactor should not be nil")
	}
}

func TestAgentSecurityManager_ElevationGate(t *testing.T) {
	sm := NewAgentSecurityManager()

	gate := sm.GetElevationGate()
	if gate == nil {
		t.Error("elevation gate should not be nil")
	}

	// Replace
	newGate := security.NewElevationGate(nil)
	sm.SetElevationGate(newGate)
	if sm.GetElevationGate() != newGate {
		t.Error("should return replaced gate")
	}
}

func TestAgentSecurityManager_HasActiveWebUIClients_NoCallback(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.HasActiveWebUIClients() {
		t.Error("should return false when no callback is set")
	}
}

func TestAgentSecurityManager_HasActiveWebUIClients_CallbackTrue(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetHasActiveWebUIClients(func() bool { return true })
	if !sm.HasActiveWebUIClients() {
		t.Error("should return true when callback returns true")
	}
}

func TestAgentSecurityManager_HasActiveWebUIClients_CallbackFalse(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetHasActiveWebUIClients(func() bool { return false })
	if sm.HasActiveWebUIClients() {
		t.Error("should return false when callback returns false")
	}
}

func TestAgentSecurityManager_ConcernIgnored_Empty(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.IsConcernIgnored("file.go", "insecure") {
		t.Error("should return false when no concerns are ignored")
	}
}

func TestAgentSecurityManager_ConcernIgnored_SetAndGet(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetConcernIgnored("file.go", "insecure")

	if !sm.IsConcernIgnored("file.go", "insecure") {
		t.Error("should return true after setting")
	}

	// Different file
	if sm.IsConcernIgnored("other.go", "insecure") {
		t.Error("should return false for different file")
	}

	// Different concern on same file
	if sm.IsConcernIgnored("file.go", "other_concern") {
		t.Error("should return false for different concern")
	}
}

func TestAgentSecurityManager_ConcernIgnored_MultipleConcerns(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetConcernIgnored("file.go", "concern_a")
	sm.SetConcernIgnored("file.go", "concern_b")
	sm.SetConcernIgnored("file2.go", "concern_a")

	if !sm.IsConcernIgnored("file.go", "concern_a") {
		t.Error("should find concern_a for file.go")
	}
	if !sm.IsConcernIgnored("file.go", "concern_b") {
		t.Error("should find concern_b for file.go")
	}
	if !sm.IsConcernIgnored("file2.go", "concern_a") {
		t.Error("should find concern_a for file2.go")
	}
	if sm.IsConcernIgnored("file2.go", "concern_b") {
		t.Error("should not find concern_b for file2.go")
	}
}

func TestAgentSecurityManager_ConcernIgnored_Idempotent(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetConcernIgnored("file.go", "concern")
	sm.SetConcernIgnored("file.go", "concern") // set again

	// Should still find it
	if !sm.IsConcernIgnored("file.go", "concern") {
		t.Error("should still find concern after idempotent set")
	}
}

func TestAgentSecurityManager_ConcurrentAccess(t *testing.T) {
	sm := NewAgentSecurityManager()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			file := "file" + string(rune('a'+n%26)) + ".go"
			sm.SetConcernIgnored(file, "concern")
			if n%2 == 0 {
				sm.SetUnsafeMode(true)
			} else {
				sm.SetUnsafeMode(false)
			}
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.GetUnsafeMode()
			sm.IsSecurityBypassApproved()
			sm.IsConcernIgnored("file.go", "concern")
		}()
	}
	wg.Wait()

	// Should not have panicked
	_ = sm.GetUnsafeMode()
}

// =============================================================================
// SP-049-3a: Unsafe shell mode tests
// =============================================================================

func TestAgentSecurityManager_UnsafeShellMode_Basic(t *testing.T) {
	sm := NewAgentSecurityManager()

	// Default should be false.
	if sm.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be false by default")
	}

	// Enable.
	sm.SetUnsafeShellMode(true)
	if !sm.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be true after setting")
	}

	// Disable again.
	sm.SetUnsafeShellMode(false)
	if sm.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be false after resetting")
	}
}

func TestAgentSecurityManager_UnsafeShellMode_IndependentOfUnsafeMode(t *testing.T) {
	sm := NewAgentSecurityManager()

	// Setting unsafe mode should NOT affect unsafe shell mode.
	sm.SetUnsafeMode(true)
	if sm.GetUnsafeShellMode() {
		t.Error("SetUnsafeMode should not change GetUnsafeShellMode")
	}
	if !sm.GetUnsafeMode() {
		t.Error("unsafe mode should be true")
	}

	// Setting unsafe shell mode should NOT affect unsafe mode.
	sm.SetUnsafeShellMode(true)
	if sm.GetUnsafeMode() != true {
		t.Error("SetUnsafeShellMode should not change GetUnsafeMode (still true)")
	}
	sm.SetUnsafeMode(false)
	sm.SetUnsafeShellMode(true)
	if sm.GetUnsafeMode() {
		t.Error("unsafe mode should remain false after SetUnsafeShellMode")
	}
	if !sm.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be true")
	}
}
