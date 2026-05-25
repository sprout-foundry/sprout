package agent

import (
	"testing"
	"time"
)

// TestShellCommandApprovalCache exercises the read-and-delete approval
// cache that bridges Gate 1 (seed pre-execute hook, no ctx) with Gate 2
// (persona risk cascade). See risk_prompt.go for the rationale.
func TestShellCommandApprovalCache(t *testing.T) {
	a := &Agent{}

	if a.consumeShellCommandApproval("rm -rf foo") {
		t.Fatal("unseeded cache should not approve")
	}

	a.markShellCommandApproved("rm -rf foo")

	if !a.consumeShellCommandApproval("rm -rf foo") {
		t.Fatal("first consume after mark should approve")
	}

	if a.consumeShellCommandApproval("rm -rf foo") {
		t.Fatal("second consume should miss — entry must be deleted")
	}

	if a.consumeShellCommandApproval("rm -rf bar") {
		t.Fatal("different command should not match a stored approval")
	}
}

func TestShellCommandApprovalCacheExpiry(t *testing.T) {
	a := &Agent{}
	a.recentlyApprovedShellCommands.Store("expired", time.Now().Add(-2*recentApprovalTTL))

	if a.consumeShellCommandApproval("expired") {
		t.Fatal("expired entry must not approve")
	}

	// Entry should still be drained even when expired so it doesn't
	// linger to satisfy a much-later re-prompt window.
	if _, ok := a.recentlyApprovedShellCommands.Load("expired"); ok {
		t.Fatal("expired entry should be deleted after consume attempt")
	}
}

func TestShellCommandApprovalCacheEmptyCommand(t *testing.T) {
	a := &Agent{}
	a.markShellCommandApproved("")
	if a.consumeShellCommandApproval("") {
		t.Fatal("empty command must never be considered approved")
	}
}

func TestShellCommandApprovalCacheNilAgent(t *testing.T) {
	var a *Agent
	a.markShellCommandApproved("rm -rf foo") // must not panic
	if a.consumeShellCommandApproval("rm -rf foo") {
		t.Fatal("nil agent must return false from consume")
	}
}
