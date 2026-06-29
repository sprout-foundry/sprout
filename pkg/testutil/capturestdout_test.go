package testutil

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestCaptureStdout_LargeOutput exercises CaptureStdout with output that
// exceeds the OS pipe buffer (64 KiB on Linux). Without a concurrent
// reader, the writer inside fn would block once the buffer fills.
func TestCaptureStdout_LargeOutput(t *testing.T) {
	const size = 256 * 1024 // 4x the typical pipe buffer
	want := strings.Repeat("x", size)

	got := CaptureStdout(t, func() {
		fmt.Print(want)
	})

	if len(got) != size {
		t.Fatalf("captured %d bytes, want %d", len(got), size)
	}
	if got != want {
		t.Fatal("output content mismatch")
	}
}

// TestCaptureStdout_SmallOutput covers the common case where the
// output fits in the pipe buffer.
func TestCaptureStdout_SmallOutput(t *testing.T) {
	got := CaptureStdout(t, func() {
		fmt.Print("hello, world")
	})
	if got != "hello, world" {
		t.Errorf("got %q, want %q", got, "hello, world")
	}
}

// TestCaptureStdout_PanicPropagates verifies that a panic from fn
// propagates out of CaptureStdout. The deferred w.Close() in the
// implementation ensures the reader goroutine can exit; t.Cleanup
// (registered by the testing framework) restores os.Stdout after the
// test goroutine recovers.
func TestCaptureStdout_PanicPropagates(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from fn, got none")
		}
		// At this point the test goroutine has recovered; os.Stdout is
		// about to be restored by t.Cleanup. The pipe writer w has been
		// closed (either explicitly or via defer), so the reader
		// goroutine has exited. We can't reliably assert os.Stdout
		// from here — t.Cleanup hasn't fired yet.
	}()

	CaptureStdout(t, func() {
		panic("boom")
	})
}

// TestCaptureStdout_NestedCalls validates that CaptureStdout can be
// called recursively without leaking goroutines or breaking os.Stdout.
// t.Cleanup hooks are LIFO, so the inner cleanup fires first, then
// the outer one — restoration order is correct.
//
// Behavior: the outer capture sees only what its own fn wrote
// directly to os.Stdout; the inner capture sees only what the inner
// fn wrote. This is correct because os.Stdout is a single global
// pointer — once the inner redirected it, the inner's writes went to
// the inner pipe (not the outer one).
func TestCaptureStdout_NestedCalls(t *testing.T) {
	outer := CaptureStdout(t, func() {
		fmt.Print("outer-")
		inner := CaptureStdout(t, func() {
			fmt.Print("inner")
		})
		if inner != "inner" {
			t.Errorf("inner capture: got %q, want %q", inner, "inner")
		}
	})
	if outer != "outer-" {
		t.Errorf("outer capture: got %q, want %q", outer, "outer-")
	}
}

// TestCaptureStdout_EmptyOutput covers the degenerate case where fn
// writes nothing.
func TestCaptureStdout_EmptyOutput(t *testing.T) {
	got := CaptureStdout(t, func() {})
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// TestCaptureStdout_RestoresStdout verifies that os.Stdout is restored
// to its original value after a successful call. Without this, any
// fmt.Print between CaptureStdout returning and the test finishing
// would silently write to a closed file descriptor.
//
// After the explicit restore, t.Cleanup is a no-op (already restored).
func TestCaptureStdout_RestoresStdout(t *testing.T) {
	original := os.Stdout
	CaptureStdout(t, func() { fmt.Print("hi") })
	if os.Stdout != original {
		t.Errorf("os.Stdout not restored: got %p, want %p", os.Stdout, original)
	}
}

// TestCaptureStdout_UTF8 exercises multibyte UTF-8 content. The 4096-
// byte read buffer splits arbitrarily across the stream; we rely on
// strings.Builder to reassemble raw bytes correctly.
func TestCaptureStdout_UTF8(t *testing.T) {
	want := "日本語 🚀 café\n"
	got := CaptureStdout(t, func() {
		fmt.Print(want)
	})
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCaptureStdoutPanicking_LargeOutput is the panic-based sibling's
// equivalent of TestCaptureStdout_LargeOutput. The no-t variant is
// intended for legacy helpers that cannot thread *testing.T through.
func TestCaptureStdoutPanicking_LargeOutput(t *testing.T) {
	const size = 256 * 1024
	want := strings.Repeat("y", size)

	got := CaptureStdoutPanicking(func() {
		fmt.Print(want)
	})

	if len(got) != size {
		t.Fatalf("captured %d bytes, want %d", len(got), size)
	}
	if got != want {
		t.Fatal("output content mismatch")
	}
}

// TestCaptureStdoutPanicking_RestoresStdoutOnPanic verifies that
// CaptureStdoutPanicking restores os.Stdout even when fn panics.
func TestCaptureStdoutPanicking_RestoresStdoutOnPanic(t *testing.T) {
	originalStdout := os.Stdout

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from fn, got none")
		}
		if os.Stdout != originalStdout {
			t.Errorf("os.Stdout not restored on panic: got %p, want %p", os.Stdout, originalStdout)
		}
	}()

	CaptureStdoutPanicking(func() {
		panic("boom")
	})
}