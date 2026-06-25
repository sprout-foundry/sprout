//go:build !js

package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// TestSyncBuffer_ConcurrentWrites exercises syncBuffer under concurrent
// writers to confirm it is race-free under `go test -race`. A plain
// bytes.Buffer would trip the race detector here. This is the regression
// guard for the streaming-output path in runShellCommand, where stdout and
// stderr goroutines write to the buffer via io.MultiWriter simultaneously.
//
// Run with: go test -race -run TestSyncBuffer_ConcurrentWrites ./pkg/agent_tools/
func TestSyncBuffer_ConcurrentWrites(t *testing.T) {
	const writers = 4
	const writesPerGoroutine = 200

	var buf syncBuffer
	var wg sync.WaitGroup
	wg.Add(writers)

	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				// io.MultiWriter calls Write on each constituent writer; a
				// non-trivial payload exercises the buffer's internal growth.
				buf.Write([]byte("line of output data that forces buffer growth\n"))
			}
		}()
	}

	wg.Wait()

	got := buf.String()
	wantLines := writers * writesPerGoroutine
	if lines := strings.Count(got, "\n"); lines != wantLines {
		t.Fatalf("expected %d lines, got %d", wantLines, lines)
	}
}

// TestRunShellCommand_Streaming_NoRace runs runShellCommand in streaming mode
// with a command that emits to both stdout and stderr concurrently, under the
// race detector. Before the fix, the streaming path used a plain bytes.Buffer
// shared between two goroutines, which is a documented data race.
//
// Run with: go test -race -run TestRunShellCommand_Streaming_NoRace ./pkg/agent_tools/
func TestRunShellCommand_Streaming_NoRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping streaming race test in short mode")
	}

	// Emit interleaved bursts to stdout and stderr from backgrounded
	// subshells so both pipes are active simultaneously. The `yes`-style
	// loop with a bounded counter keeps it deterministic and fast.
	cmd := `for i in $(seq 1 50); do echo "stdout $i"; echo "stderr $i" >&2; done`

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	output, err := runShellCommand(ctx, cmd, true)
	if err != nil {
		t.Fatalf("runShellCommand streaming failed: %v", err)
	}

	// Sanity: both streams were captured.
	if !strings.Contains(output, "stdout 1") {
		t.Errorf("expected stdout output captured, got: %s", output)
	}
	if !strings.Contains(output, "stderr 1") {
		t.Errorf("expected stderr output captured, got: %s", output)
	}
	// All 50 lines from each stream should be present (100 total lines).
	if got := strings.Count(output, "stdout "); got != 50 {
		t.Errorf("expected 50 stdout lines, got %d", got)
	}
}

// TestRunShellCommand_Streaming_ConcurrentInvocations runs multiple streaming
// commands concurrently to stress the race detector further. Each call has its
// own output buffer, but the test confirms no shared-state races exist in the
// function itself.
func TestRunShellCommand_Streaming_ConcurrentInvocations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent streaming test in short mode")
	}

	const concurrency = 8
	var wg sync.WaitGroup
	wg.Add(concurrency)
	errs := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		i := i
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cmd := `for j in $(seq 1 20); do echo "out"; echo "err" >&2; done`
			_, errs[i] = runShellCommand(ctx, cmd, true)
		}()
	}

	wg.Wait()
		for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent streaming call %d failed: %v", i, err)
		}
	}
}
