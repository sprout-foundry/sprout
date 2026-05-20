package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSetupDaemonLogging_DoesNothingWithoutServiceEnv verifies that
// setupDaemonLogging is a no-op when SPROUT_SERVICE is not set.
func TestSetupDaemonLogging_DoesNothingWithoutServiceEnv(t *testing.T) {
	// Ensure SPROUT_SERVICE is not set.
	orig := os.Getenv("SPROUT_SERVICE")
	os.Unsetenv("SPROUT_SERVICE")
	defer os.Setenv("SPROUT_SERVICE", orig)

	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	tmpDir := t.TempDir()
	setupDaemonLogging(tmpDir)

	// os.Stdout and os.Stderr should be unchanged.
	if os.Stdout != origStdout {
		t.Error("os.Stdout should not change when SPROUT_SERVICE is unset")
	}
	if os.Stderr != origStderr {
		t.Error("os.Stderr should not change when SPROUT_SERVICE is unset")
	}

	// No log directory should be created.
	logDir := filepath.Join(tmpDir, ".sprout", "logs")
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Error("log directory should not be created when SPROUT_SERVICE is unset")
	}
}

// TestSetupDaemonLogging_CreatesLogDir verifies that setupDaemonLogging
// creates the log directory when SPROUT_SERVICE=1.
func TestSetupDaemonLogging_CreatesLogDir(t *testing.T) {
	orig := os.Getenv("SPROUT_SERVICE")
	os.Setenv("SPROUT_SERVICE", "1")
	defer os.Setenv("SPROUT_SERVICE", orig)

	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	tmpDir := t.TempDir()
	setupDaemonLogging(tmpDir)

	logDir := filepath.Join(tmpDir, ".sprout", "logs")
	info, err := os.Stat(logDir)
	if err != nil {
		t.Fatalf("expected log directory to exist at %s: %v", logDir, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", logDir)
	}
}

// TestSetupDaemonLogging_RedirectsStreams verifies that os.Stdout and
// os.Stderr are replaced with pipe writers when SPROUT_SERVICE=1.
func TestSetupDaemonLogging_RedirectsStreams(t *testing.T) {
	orig := os.Getenv("SPROUT_SERVICE")
	os.Setenv("SPROUT_SERVICE", "1")
	defer os.Setenv("SPROUT_SERVICE", orig)

	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	tmpDir := t.TempDir()
	setupDaemonLogging(tmpDir)

	// os.Stdout and os.Stderr should now be pipe write-ends (different files).
	if os.Stdout == origStdout {
		t.Error("os.Stdout should have been replaced with a pipe writer")
	}
	if os.Stderr == origStderr {
		t.Error("os.Stderr should have been replaced with a pipe writer")
	}

	// Verify log files are created when we write to the new streams.
	fmt.Fprintf(os.Stdout, "test stdout\n")
	fmt.Fprintf(os.Stderr, "test stderr\n")

	// Close the pipe writers so the goroutines finish and flush to disk.
	os.Stdout.Close()
	os.Stderr.Close()

	// Give the background goroutines time to finish io.Copy + lumberjack write.
	// This is needed because Close() on the write end signals EOF, but the
	// goroutine must still read the remaining buffered data and flush to disk.
	time.Sleep(100 * time.Millisecond)

	stdoutPath := filepath.Join(tmpDir, ".sprout", "logs", "daemon.stdout.log")
	stderrPath := filepath.Join(tmpDir, ".sprout", "logs", "daemon.stderr.log")

	data, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("failed to read stdout log: %v", err)
	}
	if string(data) != "test stdout\n" {
		t.Errorf("stdout log = %q, want %q", string(data), "test stdout\n")
	}

	data, err = os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("failed to read stderr log: %v", err)
	}
	if string(data) != "test stderr\n" {
		t.Errorf("stderr log = %q, want %q", string(data), "test stderr\n")
	}
}
