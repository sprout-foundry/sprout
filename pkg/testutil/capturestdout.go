// Package testutil provides shared helpers for sprout's test suite.
//
// It is intentionally leaf-only (no imports of internal sprout packages)
// so any package's internal tests can import it without creating cycles.
package testutil

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// CaptureStdout runs fn with os.Stdout redirected to an in-memory pipe
// and returns whatever fn wrote to stdout.
//
// A goroutine drains the pipe's read end concurrently with fn's writes
// so callers can emit arbitrarily large output without deadlocking the
// writer when the OS pipe buffer (~64 KiB on Linux) fills. Without the
// concurrent reader, an output larger than the pipe buffer blocks the
// writer inside fn and the test hangs until its timeout.
//
// os.Stdout is restored to its original value before this function
// returns. t.Cleanup is registered as a safety net so the restoration
// also happens if fn panics (in which case the explicit restore never
// runs but the cleanup does, once the testing framework recovers).
//
// Only os.Stdout is redirected; os.Stderr is left untouched. Use a
// similar helper for stderr if you need it.
//
// The supplied *testing.T is used for t.Helper (so failures are reported
// at the caller's line) and t.Cleanup. Pass the test's t.
func CaptureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("CaptureStdout: pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	done := make(chan string, 1)
	go func() {
		var buf strings.Builder
		tmp := make([]byte, 4096)
		for {
			n, readErr := r.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if readErr != nil {
				break
			}
		}
		done <- buf.String()
	}()

	defer func() { _ = w.Close() }()
	fn()
	_ = w.Close()
	os.Stdout = oldStdout
	return <-done
}

// CaptureStdoutPanicking is like CaptureStdout but for callers that
// cannot supply a *testing.T (legacy helpers, helpers used from
// non-test contexts). On os.Pipe failure it panics with a descriptive
// message; on a panicking fn, os.Stdout is restored via defer.
//
// Prefer CaptureStdout when *testing.T is available.
func CaptureStdoutPanicking(fn func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("CaptureStdoutPanicking: pipe: %v", err))
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf strings.Builder
		tmp := make([]byte, 4096)
		for {
			n, readErr := r.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if readErr != nil {
				break
			}
		}
		done <- buf.String()
	}()

	defer func() {
		_ = w.Close()
		os.Stdout = old
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}
