//go:build !windows && !js

package computer_use

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// saveHooks captures the current package-level hooks and returns a restore func.
func saveHooks() func() {
	hooksMu.Lock()
	oldStarted := processStartedHook
	oldFinished := processFinishedHook
	hooksMu.Unlock()
	return func() {
		hooksMu.Lock()
		processStartedHook = oldStarted
		processFinishedHook = oldFinished
		hooksMu.Unlock()
	}
}

func TestPanicableBackend_HaltIsIdempotent(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	mock := &MockBackend{}
	p := NewPanicableBackend(mock)
	p.now = func() time.Time { return time.Unix(1000, 0) }

	// First halt should succeed.
	if err := p.Halt("panic_key"); err != nil {
		t.Fatalf("first Halt() = %v, want nil", err)
	}
	if !p.IsHalted() {
		t.Fatal("IsHalted() = false after Halt()")
	}

	// Second halt should return ErrAlreadyHalted.
	if err := p.Halt("panic_key_again"); err != ErrAlreadyHalted {
		t.Fatalf("second Halt() = %v, want ErrAlreadyHalted", err)
	}
}

func TestPanicableBackend_ResetClearsState(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	mock := &MockBackend{}
	p := NewPanicableBackend(mock)
	p.now = func() time.Time { return time.Unix(1000, 0) }

	// Halt first.
	if err := p.Halt("test_halt"); err != nil {
		t.Fatalf("Halt() = %v", err)
	}
	if !p.IsHalted() {
		t.Fatal("IsHalted() = false after Halt()")
	}

	// Reset should clear the state.
	p.Reset()
	if p.IsHalted() {
		t.Fatal("IsHalted() = true after Reset()")
	}

	// A delegated call should now succeed.
	if err := p.MouseClick(1, 2, MouseLeft, false); err != nil {
		t.Fatalf("MouseClick after Reset() = %v", err)
	}
	if len(mock.Records) != 1 || mock.Records[0].Action != "MouseClick" {
		t.Fatalf("expected MouseClick record, got %v", mock.Records)
	}
}

func TestPanicableBackend_HaltedMethodsReturnErrPanicKeyHalted(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	mock := &MockBackend{}
	p := NewPanicableBackend(mock)
	p.now = func() time.Time { return time.Unix(1000, 0) }

	// Halt the backend.
	if err := p.Halt("panic_key"); err != nil {
		t.Fatalf("Halt() = %v", err)
	}

	// Clear any records that might have been created during setup.
	mock.Records = nil

	// All 7 methods should return ErrPanicKeyHalted without delegating.
	tests := []struct {
		name string
		fn   func() error
	}{
		{"Screenshot", func() error { _, _, err := p.Screenshot(nil); return err }},
		{"MouseClick", func() error { return p.MouseClick(0, 0, MouseLeft, false) }},
		{"MouseDrag", func() error { return p.MouseDrag(Point{0, 0}, Point{1, 1}, MouseLeft) }},
		{"MoveTo", func() error { return p.MoveTo(0, 0) }},
		{"KeyboardType", func() error { return p.KeyboardType("hello") }},
		{"KeyboardPress", func() error { return p.KeyboardPress("Enter") }},
		{"Scroll", func() error { return p.Scroll(ScrollDown, 1, nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err != ErrPanicKeyHalted {
				t.Errorf("%s() = %v, want ErrPanicKeyHalted", tt.name, err)
			}
		})
	}

	// No records should have been delegated to the mock.
	if len(mock.Records) != 0 {
		t.Errorf("expected no delegated records, got %d: %v", len(mock.Records), mock.Records)
	}
}

func TestPanicableBackend_AuditEventsOnHalt(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	dir := t.TempDir()
	mock := &MockBackend{}

	// Build chain: auditing(panicable(mock))
	panicable := NewPanicableBackend(mock)
	ab, err := NewAuditingBackend(panicable, dir, "audit-test")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}

	// RecordSafetyEvent checks the global backend var to see if it's an
	// *auditingBackend. Set it so the panic_key_triggered event is captured.
	prevBackend := GetBackend()
	SetBackend(ab)
	t.Cleanup(func() { SetBackend(prevBackend) })

	// Trigger the halt through the panicable.
	if err := panicable.Halt("audit_test_reason"); err != nil {
		t.Fatalf("Halt() = %v", err)
	}

	// Close the audit writer so the file is flushed.
	if err := ab.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the JSONL log and look for the panic_key_triggered event.
	path := filepath.Join(dir, "audit-test.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	found := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("bad jsonl line: %v", err)
		}
		if rec.Action == "panic_key_triggered" {
			found = true
			if rec.Args["reason"] != "audit_test_reason" {
				t.Errorf("panic_key_triggered reason = %v, want 'audit_test_reason'", rec.Args["reason"])
			}
		}
	}
	if !found {
		t.Error("panic_key_triggered event not found in audit log")
	}
}

func TestPanicableBackend_DecoratorChainComposition(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	dir := t.TempDir()
	mock := &MockBackend{}

	// Build the full chain: auditing(rateLimited(panicable(mock)))
	panicable := NewPanicableBackend(mock)
	rateLimited := NewRateLimitedBackend(panicable, 60)
	ab, err := NewAuditingBackend(rateLimited, dir, "chain-test")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}

	// RecordSafetyEvent checks the global backend var. Set it so
	// panic_key_triggered is captured by the auditing layer.
	prevBackend := GetBackend()
	SetBackend(ab)
	t.Cleanup(func() { SetBackend(prevBackend) })

	// First action: a screenshot that should succeed and be recorded.
	_, _, err = ab.Screenshot(nil)
	if err != nil {
		t.Fatalf("first Screenshot() = %v", err)
	}

	// Now halt via the panicable.
	if err := panicable.Halt("chain_halt"); err != nil {
		t.Fatalf("Halt() = %v", err)
	}

	// Second action: should be blocked by panic key (ErrPanicKeyHalted).
	_, _, err = ab.Screenshot(nil)
	if err != ErrPanicKeyHalted {
		t.Fatalf("second Screenshot() = %v, want ErrPanicKeyHalted", err)
	}

	// Close the audit writer.
	if err := ab.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the JSONL log and verify both the screenshot and panic_key_triggered.
	path := filepath.Join(dir, "chain-test.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	var actions []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("bad jsonl line: %v", err)
		}
		actions = append(actions, rec.Action)
	}

	// We expect: "screenshot" (the first action) + "panic_key_triggered" (the halt).
	// The second Screenshot() was blocked by panic key, so the auditing layer
	// still records it (it delegates to inner, gets ErrPanicKeyHalted, and records).
	foundScreenshot := false
	foundPanicKey := false
	for _, a := range actions {
		if a == "screenshot" {
			foundScreenshot = true
		}
		if a == "panic_key_triggered" {
			foundPanicKey = true
		}
	}
	if !foundScreenshot {
		t.Error("screenshot event not found in audit log")
	}
	if !foundPanicKey {
		t.Error("panic_key_triggered event not found in audit log")
	}
}

func TestPanicableBackend_HaltReasonAndHaltedAt(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	mock := &MockBackend{}
	p := NewPanicableBackend(mock)
	fixed := time.Unix(5000, 0)
	p.now = func() time.Time { return fixed }

	if err := p.Halt("test_reason"); err != nil {
		t.Fatalf("Halt() = %v", err)
	}

	if got := p.HaltReason(); got != "test_reason" {
		t.Errorf("HaltReason() = %q, want %q", got, "test_reason")
	}
	if got := p.HaltedAt(); !got.Equal(fixed) {
		t.Errorf("HaltedAt() = %v, want %v", got, fixed)
	}
}

func TestPanicableBackend_ResetNoOpWhenNotHalted(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	dir := t.TempDir()
	mock := &MockBackend{}

	// Build chain: auditing(panicable(mock))
	panicable := NewPanicableBackend(mock)
	ab, err := NewAuditingBackend(panicable, dir, "reset-test")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}

	// Reset when not halted should be a no-op (no audit event).
	panicable.Reset()

	if err := ab.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Check that no panic_key_reset event was emitted.
	path := filepath.Join(dir, "reset-test.jsonl")
	f, err := os.Open(path)
	if err != nil {
		// File might not exist if no events were written — that's fine.
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("bad jsonl line: %v", err)
		}
		if rec.Action == "panic_key_reset" {
			t.Error("panic_key_reset event found when not halted — should be no-op")
		}
	}
}

func TestProcessGroupHooksCalledByRunWithCtx(t *testing.T) {
	// This test verifies that runWithCtx calls the processStartedHook and
	// processFinishedHook when executing a real subprocess.
	// Skip on non-unix platforms where SetProcessGroup doesn't exist.
	if testing.Short() {
		t.Skip("skip subprocess test in short mode")
	}

	restore := saveHooks()
	t.Cleanup(restore)

	var capturedProcess *os.Process
	hooksMu.Lock()
	processStartedHook = func(proc *os.Process) {
		capturedProcess = proc
	}
	processFinishedHook = func() {
		// no-op for this test
	}
	hooksMu.Unlock()

	// Create a subprocessBackend that will use real exec (we're on unix).
	b := &subprocessBackend{
		os:      "linux",
		tmpDir:  t.TempDir(),
		cliTool: "xdotool",
		capTool: "scrot",
	}

	// Run a trivial command via runWithCtx (echo is always available).
	err := b.runWithCtx(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("runWithCtx(echo) = %v", err)
	}

	if capturedProcess == nil {
		t.Error("processStartedHook was not called — capturedProcess is nil")
	}
}

func TestComputerUseConfig_ResolvePanicKeyChord(t *testing.T) {
	// Test that Resolve() applies the default PanicKeyChord.
	type testCase struct {
		name     string
		cfg      *configuration.ComputerUseConfig
		expected string
	}

	tests := []testCase{
		{
			name:     "nil config defaults to ctrl+shift+escape",
			cfg:      nil,
			expected: "ctrl+shift+escape",
		},
		{
			name:     "empty struct defaults to ctrl+shift+escape",
			cfg:      &configuration.ComputerUseConfig{},
			expected: "ctrl+shift+escape",
		},
		{
			name: "user-set value is preserved",
			cfg: &configuration.ComputerUseConfig{
				PanicKeyChord: "ctrl+break",
			},
			expected: "ctrl+break",
		},
		{
			name: "disabled sentinel is preserved",
			cfg: &configuration.ComputerUseConfig{
				PanicKeyChord: "disabled",
			},
			expected: "disabled",
		},
		{
			name: "empty string in non-nil config defaults to ctrl+shift+escape",
			cfg: &configuration.ComputerUseConfig{
				PanicKeyChord: "",
			},
			expected: "ctrl+shift+escape",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := tt.cfg.Resolve()
			if resolved.PanicKeyChord != tt.expected {
				t.Errorf("PanicKeyChord = %q, want %q", resolved.PanicKeyChord, tt.expected)
			}
		})
	}
}

// Helper to find an action in JSONL audit log.
func findAuditAction(t *testing.T, path string, wantAction string) AuditRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("bad jsonl line: %v", err)
		}
		if rec.Action == wantAction {
			return rec
		}
	}
	t.Fatalf("action %q not found in %s", wantAction, path)
	return AuditRecord{}
}

// -----------------------------------------------------------------------
// New tests added for SP-063-4g coverage gaps
// -----------------------------------------------------------------------

func TestPanicableBackend_ResetEmitsAuditEvent(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	dir := t.TempDir()
	mock := &MockBackend{}

	// Build chain: auditing(panicable(mock))
	panicable := NewPanicableBackend(mock)
	ab, err := NewAuditingBackend(panicable, dir, "reset-audit-test")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}

	// Set the global backend so RecordSafetyEvent routes to the auditing layer.
	prevBackend := GetBackend()
	SetBackend(ab)
	t.Cleanup(func() { SetBackend(prevBackend) })

	// Halt first (with a fixed time so we can compute duration).
	haltTime := time.Unix(10000, 0)
	panicable.now = func() time.Time { return haltTime }
	if err := panicable.Halt("reset_test_reason"); err != nil {
		t.Fatalf("Halt() = %v", err)
	}

	// Wait a small amount so the duration is measurable.
	time.Sleep(50 * time.Millisecond)

	// Reset should emit a panic_key_reset event.
	panicable.Reset()

	// Close the audit writer so the file is flushed.
	if err := ab.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the JSONL log and look for the panic_key_reset event.
	path := filepath.Join(dir, "reset-audit-test.jsonl")
	rec := findAuditAction(t, path, "panic_key_reset")

	// Verify halt_reason is recorded.
	if rec.Args["halt_reason"] != "reset_test_reason" {
		t.Errorf("halt_reason = %v, want 'reset_test_reason'", rec.Args["halt_reason"])
	}

	// Verify halt_duration_ms is present and is a non-negative number.
	if _, ok := rec.Args["halt_duration_ms"].(float64); !ok {
		t.Errorf("halt_duration_ms should be a number, got %T", rec.Args["halt_duration_ms"])
	}
}

func TestPanicableBackend_HaltKillsInFlightProcess(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("requires Unix process groups")
	}
	if testing.Short() {
		t.Skip("skip subprocess test in short mode")
	}

	restore := saveHooks()
	t.Cleanup(restore)

	mock := &MockBackend{}
	panicable := NewPanicableBackend(mock)

	// We need a subprocessBackend that will run a real long-lived command.
	// Use "sleep 60" which will be killed by the panic key.
	b := &subprocessBackend{
		os:      runtime.GOOS,
		tmpDir:  t.TempDir(),
		cliTool: "xdotool", // doesn't matter; we call runWithCtx directly
		capTool: "sleep",
	}

	// Run sleep 60 in a goroutine — it should be killed when we call Halt.
	done := make(chan error, 1)
	go func() {
		done <- b.runWithCtx(context.Background(), "sleep", "60")
	}()

	// Wait for the process to be captured by the hook.
	// Use a polling loop with timeout rather than a fixed sleep.
	captured := false
	for i := 0; i < 100; i++ {
		panicable.mu.Lock()
		if panicable.currentProcess != nil {
			captured = true
			panicable.mu.Unlock()
			break
		}
		panicable.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if !captured {
		t.Fatal("processStartedHook was not called — currentProcess is nil after 1s")
	}

	// Give the process a moment to fully start.
	time.Sleep(50 * time.Millisecond)

	// Now call Halt — it should kill the in-flight process.
	if err := panicable.Halt("kill_test"); err != nil {
		t.Fatalf("Halt() = %v", err)
	}

	// The goroutine should eventually return with an error (the process was killed).
	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error from killed subprocess, got nil")
		}
		// The error should mention signal or kill.
		errStr := err.Error()
		if !strings.Contains(errStr, "signal") && !strings.Contains(errStr, "killed") && !strings.Contains(errStr, "exit status 137") {
			t.Logf("subprocess error (may vary by platform): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subprocess did not terminate within 5s after Halt — kill may have failed")
	}
}

func TestPanicableBackend_PanicKeyDuplicateEmitsAuditEvent(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	dir := t.TempDir()
	mock := &MockBackend{}

	// Build chain: auditing(panicable(mock))
	panicable := NewPanicableBackend(mock)
	ab, err := NewAuditingBackend(panicable, dir, "dup-audit-test")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}

	// Set the global backend so RecordSafetyEvent routes to the auditing layer.
	prevBackend := GetBackend()
	SetBackend(ab)
	t.Cleanup(func() { SetBackend(prevBackend) })

	// First halt.
	panicable.now = func() time.Time { return time.Unix(10000, 0) }
	if err := panicable.Halt("first"); err != nil {
		t.Fatalf("first Halt() = %v", err)
	}

	// Second halt — should return ErrAlreadyHalted and emit panic_key_duplicate.
	if err := panicable.Halt("second"); err != ErrAlreadyHalted {
		t.Fatalf("second Halt() = %v, want ErrAlreadyHalted", err)
	}

	// Close the audit writer.
	if err := ab.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the JSONL log and look for the panic_key_duplicate event.
	path := filepath.Join(dir, "dup-audit-test.jsonl")
	rec := findAuditAction(t, path, "panic_key_duplicate")

	// Verify the reason is from the second call.
	if rec.Args["reason"] != "second" {
		t.Errorf("panic_key_duplicate reason = %v, want 'second'", rec.Args["reason"])
	}

	// Verify original_halt_at is recorded.
	if rec.Args["original_halt_at"] == "" {
		t.Error("original_halt_at not recorded in panic_key_duplicate")
	}
}

func TestPanicableBackend_NormalCallDelegatesThrough(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	mock := &MockBackend{}
	panicable := NewPanicableBackend(mock)

	// Call all 7 methods without halting — they should delegate to the inner mock.
	_, _, err := panicable.Screenshot(nil)
	requireNil(t, err, "Screenshot")
	err = panicable.MouseClick(10, 20, MouseLeft, false)
	requireNil(t, err, "MouseClick")
	err = panicable.MouseDrag(Point{0, 0}, Point{100, 100}, MouseLeft)
	requireNil(t, err, "MouseDrag")
	err = panicable.MoveTo(50, 60)
	requireNil(t, err, "MoveTo")
	err = panicable.KeyboardType("hello")
	requireNil(t, err, "KeyboardType")
	err = panicable.KeyboardPress("Enter")
	requireNil(t, err, "KeyboardPress")
	err = panicable.Scroll(ScrollDown, 3, nil)
	requireNil(t, err, "Scroll")

	// Verify all 7 calls were recorded by the mock.
	if len(mock.Records) != 7 {
		t.Fatalf("expected 7 delegated records, got %d", len(mock.Records))
	}

	expectedActions := []string{
		"Screenshot", "MouseClick", "MouseDrag", "MoveTo",
		"KeyboardType", "KeyboardPress", "Scroll",
	}
	for i, want := range expectedActions {
		if mock.Records[i].Action != want {
			t.Errorf("record[%d].Action = %q, want %q", i, mock.Records[i].Action, want)
		}
	}

	// Verify not halted.
	if panicable.IsHalted() {
		t.Error("IsHalted() = true after normal calls")
	}
}

func TestPanicableBackend_ConcurrentHaltIsIdempotent(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	mock := &MockBackend{}
	panicable := NewPanicableBackend(mock)
	panicable.now = func() time.Time { return time.Unix(10000, 0) }

	const goroutines = 5
	var (
		nilCount  int64
		haltCount int64
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			err := panicable.Halt("concurrent")
			if err == nil {
				atomic.AddInt64(&nilCount, 1)
			} else if err == ErrAlreadyHalted {
				atomic.AddInt64(&haltCount, 1)
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if nilCount != 1 {
		t.Errorf("expected exactly 1 nil return, got %d", nilCount)
	}
	if haltCount != int64(goroutines-1) {
		t.Errorf("expected %d ErrAlreadyHalted returns, got %d", goroutines-1, haltCount)
	}

	// Verify the backend is halted.
	if !panicable.IsHalted() {
		t.Error("IsHalted() = false after concurrent Halt calls")
	}
}

func TestProcessGroupHelpers_Unix(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Unix-only test")
	}

	// Verify SetProcessGroup sets SysProcAttr.Setpgid = true.
	cmd := &exec.Cmd{}
	SetProcessGroup(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil after SetProcessGroup")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid = false after SetProcessGroup, want true")
	}

	// Verify KillProcessGroup returns an error for a non-existent process.
	// Create a dummy process struct — we can't actually create one without
	// forking, so we test with a known-invalid PID.
	fakeProc := &os.Process{Pid: 999999999}
	err := KillProcessGroup(fakeProc)
	if err == nil {
		t.Error("KillProcessGroup on non-existent process should return error")
	}
	// The error should mention "no such process" or ESRCH.
	errStr := err.Error()
	if !strings.Contains(errStr, "no such process") && !strings.Contains(errStr, "ESRCH") {
		t.Logf("KillProcessGroup error (may vary by platform): %v", err)
	}
}

func TestProcessGroupHelpers_NoPanic(t *testing.T) {
	// On any platform, verify the helpers don't panic with basic inputs.
	cmd := &exec.Cmd{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SetProcessGroup panicked: %v", r)
			}
		}()
		SetProcessGroup(cmd)
	}()

	// KillProcessGroup on a nil process would panic — we don't test that
	// as it's a caller bug, not a helper bug. But on non-Unix platforms
	// the no-op stubs should just return nil.
	if runtime.GOOS == "js" || runtime.GOOS == "windows" {
		// On non-Unix, KillProcessGroup is a no-op that returns nil.
		fakeProc := &os.Process{Pid: 1}
		err := KillProcessGroup(fakeProc)
		if err != nil {
			t.Errorf("KillProcessGroup on non-Unix should return nil, got %v", err)
		}
	}
}

// requireNil is a small test helper for the delegation test.
func requireNil(t *testing.T, err error, method string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s returned unexpected error: %v", method, err)
	}
}

// TestRunWithCtx_NoHooksFallsBackToStubbable verifies that runWithCtx falls
// back to b.run() (which uses commandRunner) when no panic-key decorator is
// active. This ensures existing tests stubbing commandRunner still work
// (MUST_FIX #4).
func TestRunWithCtx_NoHooksFallsBackToStubbable(t *testing.T) {
	// Ensure hooks are nil (no PanicableBackend registered).
	restore := saveHooks()
	t.Cleanup(restore)
	hooksMu.Lock()
	processStartedHook = nil
	processFinishedHook = nil
	hooksMu.Unlock()

	// Stub commandRunner to capture the call.
	var calledArgs []string
	prevRun := commandRunner
	commandRunner = func(name string, args ...string) ([]byte, error) {
		calledArgs = append([]string{name}, args...)
		return nil, nil
	}
	t.Cleanup(func() { commandRunner = prevRun })

	b := &subprocessBackend{
		os:      "linux",
		tmpDir:  t.TempDir(),
		cliTool: "xdotool",
		capTool: "scrot",
	}

	err := b.runWithCtx(context.Background(), "xdotool", "mousemove", "10", "20", "click", "1")
	if err != nil {
		t.Fatalf("runWithCtx() = %v", err)
	}

	want := []string{"xdotool", "mousemove", "10", "20", "click", "1"}
	if len(calledArgs) != len(want) {
		t.Fatalf("calledArgs = %v, want %v", calledArgs, want)
	}
	for i, w := range want {
		if calledArgs[i] != w {
			t.Errorf("calledArgs[%d] = %q, want %q", i, calledArgs[i], w)
		}
	}
}

// TestTriggerPanicKey_NoBackendRegistered verifies that TriggerPanicKey is a
// no-op when no PanicableBackend has been registered.
func TestTriggerPanicKey_NoBackendRegistered(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	// Clear the global so there's no backend.
	prevGlobal := globalPanicableBackend
	globalPanicableBackend = nil
	t.Cleanup(func() { globalPanicableBackend = prevGlobal })

	// Should be a no-op.
	err := TriggerPanicKey("test")
	if err != nil {
		t.Errorf("TriggerPanicKey() = %v, want nil (no-op)", err)
	}
}

// TestTriggerPanicKey_WithBackend verifies that TriggerPanicKey delegates to
// the registered PanicableBackend.
func TestTriggerPanicKey_WithBackend(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	mock := &MockBackend{}
	p := NewPanicableBackend(mock)
	prevGlobal := globalPanicableBackend
	t.Cleanup(func() { globalPanicableBackend = prevGlobal })

	// First trigger should halt.
	if err := TriggerPanicKey("trigger_test"); err != nil {
		t.Fatalf("first TriggerPanicKey() = %v, want nil", err)
	}
	if !p.IsHalted() {
		t.Error("backend should be halted after TriggerPanicKey")
	}

	// Second trigger should return ErrAlreadyHalted.
	if err := TriggerPanicKey("trigger_test_again"); err != ErrAlreadyHalted {
		t.Errorf("second TriggerPanicKey() = %v, want ErrAlreadyHalted", err)
	}
}

// TestGlobalPanicable_ReturnsRegisteredBackend verifies that GlobalPanicable
// returns the backend set by NewPanicableBackend.
func TestGlobalPanicable_ReturnsRegisteredBackend(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	prevGlobal := globalPanicableBackend
	t.Cleanup(func() { globalPanicableBackend = prevGlobal })

	// Before registration, should be nil (or whatever was set before).
	mock := &MockBackend{}
	p := NewPanicableBackend(mock)

	got := GlobalPanicable()
	if got != p {
		t.Errorf("GlobalPanicable() = %v, want %v", got, p)
	}
}
