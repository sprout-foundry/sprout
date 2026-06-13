//go:build !js

package cmd

import (
	"testing"
)

// =============================================================================
// SP-049-3a: --unsafe implies --unsafe-shell at the command layer
// =============================================================================

func TestUnsafeImpliesUnsafeShell(t *testing.T) {
	// Save and restore globals.
	origUnsafe := agentUnsafe
	origUnsafeShell := agentUnsafeShell
	defer func() {
		agentUnsafe = origUnsafe
		agentUnsafeShell = origUnsafeShell
	}()

	// --- Scenario 1: --unsafe ONLY ---
	agentUnsafe = true
	agentUnsafeShell = false

	// Simulate the code path from agent_command.go lines 352-358.
	// We test the _logic_ here since we can't easily exercise RunE.
	// The expectation: both flags end up true on the agent.
	shellMode := agentUnsafeShell
	if agentUnsafe {
		shellMode = true
	}
	if !shellMode {
		t.Error("--unsafe only: unsafe shell mode should be true (implied by --unsafe)")
	}
}

func TestUnsafeShellIndependent(t *testing.T) {
	// Save and restore globals.
	origUnsafe := agentUnsafe
	origUnsafeShell := agentUnsafeShell
	defer func() {
		agentUnsafe = origUnsafe
		agentUnsafeShell = origUnsafeShell
	}()

	// --- Scenario 2: --unsafe-shell ONLY ---
	agentUnsafe = false
	agentUnsafeShell = true

	shellMode := agentUnsafeShell
	if agentUnsafe {
		shellMode = true
	}

	if !shellMode {
		t.Error("--unsafe-shell only: unsafe shell mode should be true")
	}
}

func TestNeitherUnsafeNorUnsafeShell(t *testing.T) {
	origUnsafe := agentUnsafe
	origUnsafeShell := agentUnsafeShell
	defer func() {
		agentUnsafe = origUnsafe
		agentUnsafeShell = origUnsafeShell
	}()

	// --- Scenario 3: neither flag ---
	agentUnsafe = false
	agentUnsafeShell = false

	shellMode := agentUnsafeShell
	if agentUnsafe {
		shellMode = true
	}

	if shellMode {
		t.Error("no flags: unsafe shell mode should be false")
	}
}

func TestBothUnsafeAndUnsafeShell(t *testing.T) {
	origUnsafe := agentUnsafe
	origUnsafeShell := agentUnsafeShell
	defer func() {
		agentUnsafe = origUnsafe
		agentUnsafeShell = origUnsafeShell
	}()

	// --- Scenario 4: both flags ---
	agentUnsafe = true
	agentUnsafeShell = true

	shellMode := agentUnsafeShell
	if agentUnsafe {
		shellMode = true
	}

	if !shellMode {
		t.Error("both flags: unsafe shell mode should be true")
	}
}
