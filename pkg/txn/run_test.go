//go:build !js

package txn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// RunCommand tests. The shell is /bin/sh (or $SHELL when absolute), so the
// commands below stick to POSIX sh.

func TestRunCommand_ExitCodeZero(t *testing.T) {
	result, err := RunCommand(context.Background(), t.TempDir(), RunRequest{Command: "echo hello"})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0; stderr=%q", result.ExitCode, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != "hello" {
		t.Fatalf("stdout = %q, want hello", result.Stdout)
	}
	if result.TimedOut || result.Truncated {
		t.Fatalf("flags = timed_out:%v truncated:%v", result.TimedOut, result.Truncated)
	}
	if result.DurationMs < 0 {
		t.Fatalf("duration_ms = %d", result.DurationMs)
	}
}

func TestRunCommand_StreamsCapturedSeparately(t *testing.T) {
	result, err := RunCommand(context.Background(), t.TempDir(), RunRequest{
		Command: "echo to-out; echo to-err 1>&2",
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "to-out" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if strings.TrimSpace(result.Stderr) != "to-err" {
		t.Fatalf("stderr = %q", result.Stderr)
	}
}

func TestRunCommand_NonZeroExitCode(t *testing.T) {
	result, err := RunCommand(context.Background(), t.TempDir(), RunRequest{Command: "exit 42"})
	if err != nil {
		t.Fatalf("a non-zero exit is reportable, not an error: %v", err)
	}
	if result.ExitCode != 42 {
		t.Fatalf("exit_code = %d, want 42", result.ExitCode)
	}
}

func TestRunCommand_CommandNotFoundIsReportedNotErrored(t *testing.T) {
	result, err := RunCommand(context.Background(), t.TempDir(), RunRequest{Command: "definitely-not-a-command-xyz"})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	// sh reports 127 for a command it cannot find.
	if result.ExitCode != 127 {
		t.Fatalf("exit_code = %d, want 127; stderr=%q", result.ExitCode, result.Stderr)
	}
	if result.Stderr == "" {
		t.Fatal("sh should say something on stderr")
	}
}

func TestRunCommand_WorkdirIsCwd(t *testing.T) {
	dir := t.TempDir()
	result, err := RunCommand(context.Background(), dir, RunRequest{Command: "pwd"})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Stdout) != want {
		t.Fatalf("pwd = %q, want %q", result.Stdout, want)
	}
}

func TestRunCommand_RelativeWorkdirConfinedToRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := RunCommand(context.Background(), dir, RunRequest{
		Command: "pwd",
		Workdir: "sub",
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(result.Stdout), "sub") {
		t.Fatalf("pwd = %q, want a path ending in /sub", result.Stdout)
	}
}

func TestRunCommand_AbsoluteWorkdirRefused(t *testing.T) {
	if _, err := RunCommand(context.Background(), t.TempDir(), RunRequest{
		Command: "pwd",
		Workdir: "/etc",
	}); err == nil {
		t.Fatal("an absolute workdir must be refused")
	}
}

func TestRunCommand_WorkdirTraversalRefused(t *testing.T) {
	if _, err := RunCommand(context.Background(), t.TempDir(), RunRequest{
		Command: "pwd",
		Workdir: "../elsewhere",
	}); err == nil {
		t.Fatal("a .. workdir must be refused")
	}
}

func TestRunCommand_MissingWorkdirTargetIsReportable(t *testing.T) {
	dir := t.TempDir()
	result, err := RunCommand(context.Background(), dir, RunRequest{
		Command: "pwd",
		Workdir: "does-not-exist",
	})
	if err != nil {
		t.Fatalf("a start failure is reportable, not an error: %v", err)
	}
	if result.ExitCode != StartFailureExitCode {
		t.Fatalf("exit_code = %d, want %d", result.ExitCode, StartFailureExitCode)
	}
	if result.Stderr == "" {
		t.Fatal("the start failure must be explained in stderr")
	}
}

func TestRunCommand_TimeoutKillsAndReports124(t *testing.T) {
	started := time.Now()
	result, err := RunCommand(context.Background(), t.TempDir(), RunRequest{
		Command:        "sleep 30",
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("timed_out = false; exit_code=%d", result.ExitCode)
	}
	if result.ExitCode != TimeoutExitCode {
		t.Fatalf("exit_code = %d, want 124", result.ExitCode)
	}
	elapsed := time.Since(started)
	if elapsed > 10*time.Second {
		t.Fatalf("the run took %v — the timeout did not kill it", elapsed)
	}
}

func TestRunCommand_TimeoutKillsWholeProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no process groups on Windows")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "child-lived")
	script := fmt.Sprintf(
		// A child that outlives the shell writes a marker two seconds
		// after the shell is gone. If the GROUP is killed (not just the
		// shell), the marker never appears.
		"sh -c 'sleep 2; touch %s' & sleep 30",
		marker)
	result, err := RunCommand(context.Background(), dir, RunRequest{Command: script, TimeoutSeconds: 1})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !result.TimedOut || result.ExitCode != TimeoutExitCode {
		t.Fatalf("result = %+v, want a timeout with exit 124", result)
	}

	// Give the orphan window time to write the marker if it survived.
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a child of the timed-out shell survived — the process group was not killed")
	}
}

func TestRunCommand_TimeoutDefaultsAndHardCap(t *testing.T) {
	if got := normalizeTimeout(0); got != DefaultTimeoutSeconds*time.Second {
		t.Fatalf("normalizeTimeout(0) = %v, want the 600s default", got)
	}
	if got := normalizeTimeout(-5); got != DefaultTimeoutSeconds*time.Second {
		t.Fatalf("normalizeTimeout(-5) = %v, want the 600s default", got)
	}
	if got := normalizeTimeout(MaxTimeoutSeconds + 1000); got != MaxTimeoutSeconds*time.Second {
		t.Fatalf("normalizeTimeout(over cap) = %v, want the 900s ceiling", got)
	}
	if got := normalizeTimeout(2); got != 2*time.Second {
		t.Fatalf("normalizeTimeout(2) = %v, want 2s", got)
	}
}

func TestRunCommand_TruncationKeepsTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on /bin/sh tooling")
	}
	dir := t.TempDir()
	// Number the lines so the retained tail is identifiable.
	result, err := RunCommand(context.Background(), dir, RunRequest{
		Command: fmt.Sprintf("for i in $(seq 1 %d); do echo line-$i; done", MaxOutputBytes/8+50),
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !result.Truncated {
		t.Fatalf("truncated = false, stdout len = %d", len(result.Stdout))
	}
	if len(result.Stdout) > MaxOutputBytes+len("\nline-") {
		t.Fatalf("stdout len = %d, exceeds the cap", len(result.Stdout))
	}
	if !strings.Contains(result.Stdout, fmt.Sprintf("line-%d", MaxOutputBytes/8+50)) {
		t.Fatal("the LAST output must be the retained tail")
	}
}

func TestRollingBuffer_Table(t *testing.T) {
	buf := newRollingBuffer(8)
	if _, err := buf.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "abc" || buf.truncated() {
		t.Fatalf("buf = %q truncated=%v", buf.String(), buf.truncated())
	}
	if _, err := buf.Write([]byte("defghijklmn")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "ghijklmn" {
		t.Fatalf("buf = %q, want the last 8 bytes", buf.String())
	}
	if !buf.truncated() {
		t.Fatal("truncated = false after eviction")
	}
	// A single write larger than the cap keeps only its tail.
	if _, err := buf.Write([]byte(strings.Repeat("x", 20))); err != nil {
		t.Fatal(err)
	}
	if len(buf.String()) != 8 || strings.TrimLeft(buf.String(), "x") != "" {
		t.Fatalf("buf = %q, want 8 x's", buf.String())
	}
}

func TestRunCommand_EmptyCommand(t *testing.T) {
	result, err := RunCommand(context.Background(), t.TempDir(), RunRequest{Command: ""})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	// sh -c "" exits 0 having done nothing.
	if result.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", result.ExitCode)
	}
}

func TestRunCommand_InheritsEnvironment(t *testing.T) {
	t.Setenv("SPROUT_TXN_TEST_VAR", "inherited")
	result, err := RunCommand(context.Background(), t.TempDir(), RunRequest{
		Command: "printf %s \"$SPROUT_TXN_TEST_VAR\"",
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if result.Stdout != "inherited" {
		t.Fatalf("stdout = %q, want the inherited env value", result.Stdout)
	}
}
