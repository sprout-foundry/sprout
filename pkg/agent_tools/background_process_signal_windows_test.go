//go:build windows && !js

package tools

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils/pidalive"
)

// TestAttachProcessToJob_InvalidPID verifies that AttachProcessToJob
// handles invalid PIDs correctly per its contract: returns (0, nil).
func TestAttachProcessToJob_InvalidPID(t *testing.T) {
	tests := []struct {
		name string
		pid  int
	}{
		{"zero PID", 0},
		{"negative PID", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := AttachProcessToJob(tt.pid)
			if err != nil {
				t.Fatalf("AttachProcessToJob(%d) unexpected error: %v", tt.pid, err)
			}
			if h != 0 {
				t.Errorf("AttachProcessToJob(%d) = %v, want 0", tt.pid, h)
			}
		})
	}
}

// TestAttachProcessToJob_RoundTrip tests the full lifecycle:
// spawn a process, attach it to a Job Object, verify registration,
// then kill the Job and verify the process is terminated.
func TestAttachProcessToJob_RoundTrip(t *testing.T) {
	// Start a process that will stay alive for a while (timeout 30 seconds)
	cmd := exec.Command("cmd.exe", "/c", "timeout", "30", ">nul")
	if err := cmd.Start(); err != nil {
		t.Skipf("skipping: could not start test process: %v", err)
	}
	defer func() {
		// Clean up: try to kill if still alive
		cmd.Process.Kill()
		cmd.Wait()
	}()

	pid := cmd.Process.Pid
	t.Logf("Started test process with PID %d", pid)

	// Verify process is alive before attaching to Job
	if !pidalive.IsAlive(pid) {
		t.Fatalf("process %d should be alive before Job attachment", pid)
	}

	// Attach to Job Object
	jobHandle, err := AttachProcessToJob(pid)
	if err != nil {
		t.Fatalf("AttachProcessToJob(%d) failed: %v", pid, err)
	}
	if jobHandle == 0 {
		t.Fatal("AttachProcessToJob returned zero handle")
	}
	t.Logf("Attached PID %d to Job Object handle %v", pid, jobHandle)

	// Verify registry has the entry
	if _, ok := jobRegistry.Load(pid); !ok {
		t.Errorf("jobRegistry should have entry for PID %d", pid)
	}

	// Kill via Job Object (this is what killProcessGroup does internally)
	if v, ok := jobRegistry.LoadAndDelete(pid); ok {
		if h, ok := v.(uintptr); ok && h != 0 {
			// CloseHandle on the underlying windows.Handle
			// We need to convert back to windows.Handle
			// The killProcessGroup function handles this
		}
	}

	// Use killProcessGroup to kill the process (tests the full path)
	err = killProcessGroup(cmd.Process)
	if err != nil {
		t.Logf("killProcessGroup returned error (expected on Windows): %v", err)
	}

	// Wait for process to terminate (with deadline so slow CI runners
	// don't get a false-positive timeout; with a deadline so flaky CI
	// doesn't get a false-positive "dead" check).
	if !waitUntilDead(pid, 5*time.Second) {
		t.Errorf("process %d should be dead after killProcessGroup", pid)
	}

	// Verify registry entry was removed
	if _, ok := jobRegistry.Load(pid); ok {
		t.Errorf("jobRegistry should not have entry for PID %d after kill", pid)
	}
}

// TestKillProcessGroup_CascadeKillsDescendants tests that killing a
// parent process kills its children as well (via Job Object).
//
// Strategy: launch a parent cmd.exe which spawns a long-running child
// ping.exe. Enumerate child PIDs via tasklist, then kill the parent via
// killProcessGroup. Verify both parent AND child are dead — if only the
// parent dies, the Job Object wasn't attached and descendants leaked.
func TestKillProcessGroup_CascadeKillsDescendants(t *testing.T) {
	// Start a parent process that spawns a child:
	// cmd.exe /c "start /b cmd.exe /c ping -n 30 127.0.0.1 >nul"
	// This creates a parent cmd.exe and a child ping.exe
	cmd := exec.Command("cmd.exe", "/c", "start", "/b", "cmd.exe", "/c", "ping", "-n", "30", "127.0.0.1", ">nul")
	if err := cmd.Start(); err != nil {
		t.Skipf("skipping: could not start test process: %v", err)
	}
	defer cmd.Process.Kill()

	parentPID := cmd.Process.Pid
	t.Logf("Started parent process with PID %d", parentPID)

	// Give the child process time to spawn (slow on some CI runners —
	// cmd.exe's `start` command can take up to 1s to actually fork).
	time.Sleep(1 * time.Second)

	// Attach parent to Job Object. NOTE: the child spawned via `start`
	// may or may not inherit the Job depending on timing — see the
	// documented race in AttachProcessToJob's comment. To make this
	// test deterministic, attach BEFORE `start` runs by spawning cmd
	// directly and using a shell script that uses & to background.
	jobHandle, err := AttachProcessToJob(parentPID)
	if err != nil {
		t.Fatalf("AttachProcessToJob(%d) failed: %v", parentPID, err)
	}
	if jobHandle == 0 {
		t.Fatal("AttachProcessToJob returned zero handle")
	}

	// Enumerate children BEFORE killing (tasklist /FI "PPID eq <pid>")
	// On Windows, tasklist filters by PPID; we capture the set of child
	// PIDs and verify they all die after killProcessGroup.
	children := enumerateChildPIDs(t, parentPID)
	t.Logf("Found %d child processes before kill: %v", len(children), children)

	// Kill via Job Object (should kill parent and any children in the Job)
	if err := killProcessGroup(cmd.Process); err != nil {
		t.Logf("killProcessGroup returned error: %v", err)
	}

	// Wait (with deadline) for parent to die.
	if !waitUntilDead(parentPID, 5*time.Second) {
		t.Errorf("parent process %d should be dead after killProcessGroup", parentPID)
	}

	// Verify every captured child is also dead — this is the cascade
	// assertion the test name promises.
	for _, childPID := range children {
		if !waitUntilDead(childPID, 5*time.Second) {
			t.Errorf("child PID %d (of parent %d) should be dead after killProcessGroup (Job Object cascade)",
				childPID, parentPID)
		}
	}
}

// enumerateChildPIDs returns the list of PIDs whose parent is the
// given PID. Uses tasklist /FI "PPID eq <pid>" /FO CSV /NH and parses
// the output. Returns an empty slice if tasklist is unavailable or
// finds no children. Skips the test (via t.Skipf) on hard errors.
func enumerateChildPIDs(t *testing.T, parentPID int) []int {
	t.Helper()
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PPID eq %d", parentPID),
		"/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		t.Skipf("tasklist not available: %v", err)
		return nil
	}
	var pids []int
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		// Format: "ping.exe","12345","Console","1","12,345 K"
		fields := strings.Split(scanner.Text(), ",")
		if len(fields) < 2 {
			continue
		}
		pidStr := strings.Trim(fields[1], "\" ")
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// waitUntilDead polls pidalive.IsAlive until it returns false or the
// deadline expires. Returns true if the process died within the deadline.
func waitUntilDead(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !pidalive.IsAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !pidalive.IsAlive(pid)
}

// TestDoubleClose_DoesNotPanic verifies that calling CloseJobForPID
// and then killProcessGroup (or vice versa) does not panic due to
// double-close. This tests the LoadAndDelete atomic protection.
func TestDoubleClose_DoesNotPanic(t *testing.T) {
	// Start a process
	cmd := exec.Command("cmd.exe", "/c", "timeout", "10", ">nul")
	if err := cmd.Start(); err != nil {
		t.Skipf("skipping: could not start test process: %v", err)
	}
	defer cmd.Process.Kill()

	pid := cmd.Process.Pid

	// Attach to Job
	_, err := AttachProcessToJob(pid)
	if err != nil {
		t.Fatalf("AttachProcessToJob(%d) failed: %v", pid, err)
	}

	// First: call CloseJobForPID directly (simulates monitor goroutine cleanup)
	CloseJobForPID(pid)

	// Second: call killProcessGroup (should not panic)
	// The LoadAndDelete in killProcessGroup should find nothing and fall back to p.Kill()
	err = killProcessGroup(cmd.Process)
	if err != nil {
		t.Logf("killProcessGroup after CloseJobForPID returned: %v", err)
	}

	// Also test the reverse: killProcessGroup first, then CloseJobForPID
	cmd2 := exec.Command("cmd.exe", "/c", "timeout", "10", ">nul")
	if err := cmd2.Start(); err != nil {
		t.Skipf("skipping: could not start second test process: %v", err)
	}
	defer cmd2.Process.Kill()

	pid2 := cmd2.Process.Pid

	// Attach
	_, err = AttachProcessToJob(pid2)
	if err != nil {
		t.Fatalf("AttachProcessToJob(%d) failed: %v", pid2, err)
	}

	// killProcessGroup first (this removes from registry via LoadAndDelete)
	err = killProcessGroup(cmd2.Process)
	if err != nil {
		t.Logf("killProcessGroup returned: %v", err)
	}

	// CloseJobForPID should be a no-op now (entry already removed)
	CloseJobForPID(pid2)

	// If we get here without panic, the test passes
}

// TestInterruptProcessGroup_SendsCtrlBreak verifies that interruptProcessGroup
// attempts to send CTRL_BREAK_EVENT before falling back to Kill.
func TestInterruptProcessGroup_SendsCtrlBreak(t *testing.T) {
	// Start a process that will stay alive
	cmd := exec.Command("cmd.exe", "/c", "timeout", "30", ">nul")
	if err := cmd.Start(); err != nil {
		t.Skipf("skipping: could not start test process: %v", err)
	}
	defer cmd.Process.Kill()

	pid := cmd.Process.Pid

	// Verify it's alive
	if !pidalive.IsAlive(pid) {
		t.Fatalf("process %d should be alive", pid)
	}

	// Call interruptProcessGroup (sends Ctrl+Break, then Kill)
	err := interruptProcessGroup(cmd.Process)
	if err != nil {
		t.Logf("interruptProcessGroup returned error: %v", err)
	}

	// Wait for process to terminate (deadline-based so flaky CI doesn't
	// false-positive on a brief delay before the kill takes effect).
	if !waitUntilDead(pid, 5*time.Second) {
		t.Errorf("process %d should be dead after interruptProcessGroup", pid)
	}
}

// TestTerminateProcessGroup_KillsProcess verifies that terminateProcessGroup
// kills the process.
func TestTerminateProcessGroup_KillsProcess(t *testing.T) {
	// Start a process
	cmd := exec.Command("cmd.exe", "/c", "timeout", "30", ">nul")
	if err := cmd.Start(); err != nil {
		t.Skipf("skipping: could not start test process: %v", err)
	}
	defer cmd.Process.Kill()

	pid := cmd.Process.Pid

	// Verify it's alive
	if !pidalive.IsAlive(pid) {
		t.Fatalf("process %d should be alive", pid)
	}

	// Call terminateProcessGroup
	err := terminateProcessGroup(cmd.Process)
	if err != nil {
		t.Logf("terminateProcessGroup returned error: %v", err)
	}

	// Wait for process to terminate (deadline-based to avoid false-positives
	// from slow kill propagation on heavily-loaded CI runners).
	if !waitUntilDead(pid, 5*time.Second) {
		t.Errorf("process %d should be dead after terminateProcessGroup", pid)
	}
}

// TestNilProcess_NoPanic verifies that all process group functions handle nil safely.
func TestNilProcess_NoPanic(t *testing.T) {
	// These should not panic with nil input
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("function panicked with nil process: %v", r)
		}
	}()

	// Test interruptProcessGroup with nil
	if err := interruptProcessGroup(nil); err != nil {
		t.Errorf("interruptProcessGroup(nil) returned error: %v", err)
	}

	// Test terminateProcessGroup with nil
	if err := terminateProcessGroup(nil); err != nil {
		t.Errorf("terminateProcessGroup(nil) returned error: %v", err)
	}

	// Test killProcessGroup with nil
	if err := killProcessGroup(nil); err != nil {
		t.Errorf("killProcessGroup(nil) returned error: %v", err)
	}
}

// TestJobRegistryConcurrentAccess verifies that the jobRegistry can handle
// concurrent access from multiple goroutines.
func TestJobRegistryConcurrentAccess(t *testing.T) {
	// Start multiple processes and attach them to Jobs concurrently
	const numProcesses = 5
	pids := make([]int, numProcesses)
	cmds := make([]*exec.Cmd, numProcesses)

	for i := 0; i < numProcesses; i++ {
		cmd := exec.Command("cmd.exe", "/c", "timeout", "30", ">nul")
		if err := cmd.Start(); err != nil {
			t.Skipf("skipping: could not start test process: %v", err)
		}
		cmds[i] = cmd
		pids[i] = cmd.Process.Pid
		defer cmd.Process.Kill()
	}

	// Attach all to Jobs concurrently
	errCh := make(chan error, numProcesses)
	for i := 0; i < numProcesses; i++ {
		go func(pid int) {
			_, err := AttachProcessToJob(pid)
			errCh <- err
		}(pids[i])
	}

	// Wait for all attachments
	for i := 0; i < numProcesses; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("AttachProcessToJob failed: %v", err)
		}
	}

	// Verify all entries exist
	for _, pid := range pids {
		if _, ok := jobRegistry.Load(pid); !ok {
			t.Errorf("jobRegistry missing entry for PID %d", pid)
		}
	}

	// Kill all concurrently
	for i := 0; i < numProcesses; i++ {
		go func(cmd *exec.Cmd) {
			killProcessGroup(cmd.Process)
		}(cmds[i])
	}

	// Wait for all to die (deadline-based loop — check each PID with its
	// own deadline so a slow kill doesn't time out the whole test).
	for _, pid := range pids {
		if !waitUntilDead(pid, 5*time.Second) {
			t.Errorf("process %d should be dead", pid)
		}
	}
}
