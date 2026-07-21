//go:build windows && !js

package tools

import (
	"context"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils/pidalive"
)

// TestBackgroundProcess_AttachesJobOnStart verifies that when a background
// process is started, it gets attached to a Job Object on Windows.
func TestBackgroundProcess_AttachesJobOnStart(t *testing.T) {
	// Create a BackgroundProcessManager
	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Start a background process that will stay alive for a while
	sessionID, err := bpm.Start(context.Background(), "timeout /t 30 /nobreak > nul", "")
	if err != nil {
		t.Fatalf("Failed to start background process: %v", err)
	}

	// Get the process
	proc, ok := bpm.GetProcess(sessionID)
	if !ok {
		t.Fatal("Process not found in BPM")
	}

	// Get the PID
	pid := proc.GetPID()
	if pid == 0 {
		t.Fatal("PID should not be zero")
	}
	t.Logf("Started background process %s with PID %d", sessionID, pid)

	// Verify the process is alive
	if !pidalive.IsAlive(pid) {
		t.Errorf("Process %d should be alive", pid)
	}

	// Verify jobRegistry has an entry for this PID
	if _, ok := jobRegistry.Load(pid); !ok {
		t.Errorf("jobRegistry should have entry for PID %d", pid)
	}

	// Clean up: stop the process
	if err := bpm.Stop(sessionID, time.Second); err != nil {
		t.Logf("Stop returned error: %v", err)
	}

	// Wait for process to exit
	select {
	case <-proc.Done():
		t.Log("Process exited successfully")
	case <-time.After(5 * time.Second):
		t.Error("Process did not exit within timeout")
	}

	// Verify jobRegistry entry is removed
	if _, ok := jobRegistry.Load(pid); ok {
		t.Errorf("jobRegistry should not have entry for PID %d after Stop", pid)
	}
}

// TestBackgroundProcess_JobHandleClosedOnExit verifies that when a background
// process exits, its Job Object handle is closed and removed from the registry.
func TestBackgroundProcess_JobHandleClosedOnExit(t *testing.T) {
	// Create a BackgroundProcessManager
	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Start a background process that exits immediately
	sessionID, err := bpm.Start(context.Background(), "exit 0", "")
	if err != nil {
		t.Fatalf("Failed to start background process: %v", err)
	}

	// Get the process
	proc, ok := bpm.GetProcess(sessionID)
	if !ok {
		t.Fatal("Process not found in BPM")
	}

	// Get the PID
	pid := proc.GetPID()
	if pid == 0 {
		t.Fatal("PID should not be zero")
	}
	t.Logf("Started background process %s with PID %d", sessionID, pid)

	// Verify jobRegistry has an entry before exit
	if _, ok := jobRegistry.Load(pid); !ok {
		t.Errorf("jobRegistry should have entry for PID %d before exit", pid)
	}

	// Wait for Done() to close (process should exit quickly)
	select {
	case <-proc.Done():
		t.Log("Process exited successfully")
	case <-time.After(5 * time.Second):
		t.Error("Process did not exit within timeout")
	}

	// Give the monitor goroutine time to clean up
	time.Sleep(500 * time.Millisecond)

	// Verify jobRegistry entry is removed after exit
	if _, ok := jobRegistry.Load(pid); ok {
		t.Errorf("jobRegistry should not have entry for PID %d after exit", pid)
	}

	// Verify the process state is cleaned up
	if proc.GetPID() != 0 {
		t.Errorf("Process PID should be 0 after exit, got %d", proc.GetPID())
	}
}

// TestBackgroundProcess_StopKillsViaJobObject verifies that Stop() uses the
// Job Object to kill the process and its descendants.
func TestBackgroundProcess_StopKillsViaJobObject(t *testing.T) {
	// Create a BackgroundProcessManager
	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Start a background process that stays alive
	sessionID, err := bpm.Start(context.Background(), "timeout /t 30 /nobreak > nul", "")
	if err != nil {
		t.Fatalf("Failed to start background process: %v", err)
	}

	// Get the process
	proc, ok := bpm.GetProcess(sessionID)
	if !ok {
		t.Fatal("Process not found in BPM")
	}

	// Get the PID
	pid := proc.GetPID()
	if pid == 0 {
		t.Fatal("PID should not be zero")
	}
	t.Logf("Started background process %s with PID %d", sessionID, pid)

	// Verify it's alive
	if !pidalive.IsAlive(pid) {
		t.Errorf("Process %d should be alive", pid)
	}

	// Stop the process via BPM.Stop (which uses killProcessGroup)
	if err := bpm.Stop(sessionID, time.Second); err != nil {
		t.Logf("Stop returned error: %v", err)
	}

	// Wait for Done() to close
	select {
	case <-proc.Done():
		t.Log("Process exited successfully after Stop")
	case <-time.After(5 * time.Second):
		t.Error("Process did not exit within timeout after Stop")
	}

	// Verify it's dead
	if pidalive.IsAlive(pid) {
		t.Errorf("Process %d should be dead after Stop", pid)
	}

	// Verify jobRegistry entry is removed
	if _, ok := jobRegistry.Load(pid); ok {
		t.Errorf("jobRegistry should not have entry for PID %d after Stop", pid)
	}
}

// TestBackgroundProcessManager_MultipleProcesses verifies that multiple
// background processes can be managed concurrently.
func TestBackgroundProcessManager_MultipleProcesses(t *testing.T) {
	// Create a BackgroundProcessManager
	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Start multiple processes
	const numProcesses = 3
	sessionIDs := make([]string, numProcesses)

	for i := 0; i < numProcesses; i++ {
		sid, err := bpm.Start(context.Background(), "timeout /t 30 /nobreak > nul", "")
		if err != nil {
			t.Fatalf("Failed to start background process %d: %v", i, err)
		}
		sessionIDs[i] = sid
		t.Logf("Started background process %s", sid)
	}

	// Verify all are active
	for _, sid := range sessionIDs {
		if !bpm.IsActive(sid) {
			t.Errorf("Session %s should be active", sid)
		}
	}

	// Stop all
	for _, sid := range sessionIDs {
		if err := bpm.Stop(sid, time.Second); err != nil {
			t.Logf("Stop(%s) returned error: %v", sid, err)
		}
	}

	// Wait for all to exit
	time.Sleep(1 * time.Second)

	// Verify all are inactive
	for _, sid := range sessionIDs {
		if bpm.IsActive(sid) {
			t.Errorf("Session %s should be inactive after Stop", sid)
		}
	}
}

// TestBackgroundProcessManager_StopNonexistent verifies that Stop handles
// non-existent sessions gracefully.
func TestBackgroundProcessManager_StopNonexistent(t *testing.T) {
	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Stop a session that doesn't exist
	err := bpm.Stop("nonexistent-session", time.Second)
	if err == nil {
		t.Error("Stop should return error for nonexistent session")
	}
}

// TestBackgroundProcessManager_GetBaseDir verifies that GetBaseDir returns
// a valid directory path.
func TestBackgroundProcessManager_GetBaseDir(t *testing.T) {
	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	baseDir := bpm.GetBaseDir()
	if baseDir == "" {
		t.Error("GetBaseDir should not return empty string")
	}
	// The directory should exist (or be creatable)
	// We can't easily test this without modifying the filesystem,
	// but we can verify the path looks reasonable
	t.Logf("Background process base directory: %s", baseDir)
}
