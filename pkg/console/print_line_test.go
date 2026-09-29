package console

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func captureStderrOutput(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stderr = orig
	_ = w.Close()
	return <-done
}

func TestPrintLine_NoPromptWritesToStderr(t *testing.T) {
	out := captureStderrOutput(t, func() { PrintLine("daemon started") })
	if out != "daemon started\n" {
		t.Fatalf("stderr = %q, want the line with a trailing newline", out)
	}
}

// Callers mid-redraw hold the output lock; PrintLine must not wait on it.
func TestPrintLine_DoesNotDeadlockUnderOutputLock(t *testing.T) {
	LockOutput()
	defer UnlockOutput()
	done := make(chan string)
	go func() {
		done <- captureStderrOutput(t, func() { PrintLine("while locked") })
	}()
	select {
	case out := <-done:
		if !strings.Contains(out, "while locked") {
			t.Fatalf("stderr = %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PrintLine blocked on the output lock")
	}
}
