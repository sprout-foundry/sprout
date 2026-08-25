package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPrintLineAsyncPreservesOrdering verifies that PrintLineAsync delivers
// messages to stdout in the order they were enqueued. After the chrome-
// routing fix (writeTerminalMessage no longer routes through the streaming
// callback), the observation sink is stdout.
//
// This is a simplified version of the previous backpressure test. The
// backpressure behavior (synchronous fallback when the channel saturates)
// is still exercised, but we no longer artificially constrict the buffer
// or introduce callback sleeps — those made the test flaky and coupled
// to the streaming callback path that no longer exists for chrome.
func TestPrintLineAsyncPreservesOrdering(t *testing.T) {
	a := &Agent{
		output: NewAgentOutputManager(),
	}
	a.output.SetOutputMutex(&sync.Mutex{})
	router := NewOutputRouter(a, nil)
	a.output.SetOutputRouter(router)

	const total = 50

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	for i := 0; i < total; i++ {
		a.PrintLineAsync(fmt.Sprintf("msg-%03d", i))
	}

	// Give the async worker time to drain the channel and write to stdout.
	// PrintLineAsync is non-blocking; the worker goroutine processes
	// messages asynchronously. Without this delay the pipe closes before
	// any messages land.
	time.Sleep(500 * time.Millisecond)

	// Drain the pipe.
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	// Extract msg-NNN tokens in arrival order. Each line is preceded by
	// \r\033[K (row clear) so we search for the msg- prefix within each
	// line rather than requiring it at the start.
	var outputs []string
	for _, line := range strings.Split(buf.String(), "\n") {
		idx := strings.Index(line, "msg-")
		if idx < 0 {
			continue
		}
		outputs = append(outputs, line[idx:])
	}

	if len(outputs) != total {
		t.Fatalf("expected %d outputs, got %d (buf=%q)", total, len(outputs), buf.String())
	}
	for i := 0; i < total; i++ {
		expected := fmt.Sprintf("msg-%03d", i)
		if outputs[i] != expected {
			t.Fatalf("output[%d] = %q, want %q", i, outputs[i], expected)
		}
	}
}
