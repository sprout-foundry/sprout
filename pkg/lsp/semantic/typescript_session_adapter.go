package semantic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// workerReadTimeout bounds how long Run waits for a response line from the
// worker. A worker that stalls (e.g. a wedged LanguageService) is killed and
// recycled instead of blocking every subsequent request for that workspace.
//
// 30s was too tight for the FIRST request on a large TypeScript workspace:
// the initial LanguageService program build routinely exceeds it, the worker
// gets killed mid-build, and every retry restarts the build and dies again —
// the "diagnostics never finish" symptom. 120s still bounds a truly wedged
// worker while letting a slow-but-progressing first build complete; the
// per-workspace session cache makes the cost one-time.
const workerReadTimeout = 120 * time.Second

// workerReadResult is the outcome of a single line-based read from the
// worker's stdout. It is delivered on a buffered channel so the reader
// goroutine never blocks on delivery.
type workerReadResult struct {
	line []byte
	err  error
}

type typeScriptSessionAdapter struct {
	mu     sync.Mutex
	closed bool
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr bytes.Buffer
}

// NewTypeScriptSessionPool creates a reusable per-workspace adapter pool for
// TypeScript-family languages backed by a persistent Node worker process.
func NewTypeScriptSessionPool(idleTTL time.Duration) *SessionPool {
	return NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		return &typeScriptSessionAdapter{}, nil
	}, idleTTL)
}

func (a *typeScriptSessionAdapter) Run(input ToolInput) (ToolResult, error) {
	// The mutex stays held for the whole round-trip on purpose: the worker is a
	// single line-based process with no request IDs, so it can only handle one
	// request at a time (per-write locking is impossible without a protocol
	// change). The watchdog below prevents a wedged worker from blocking
	// forever — it kills and recycles the worker after a stall.
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return ToolResult{}, fmt.Errorf("typescript session closed")
	}

	if err := a.ensureWorkerLocked(input.WorkspaceRoot); err != nil {
		return ToolResult{}, err
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return ToolResult{}, err
	}

	if _, err := a.stdin.Write(append(payload, '\n')); err != nil {
		a.resetWorkerLocked()
		return ToolResult{}, fmt.Errorf("typescript worker write failed: %w", err)
	}

	// Read the response line with a watchdog. The read itself is unbounded, so
	// without a timeout a hung worker would hold a.mu — and thus block every
	// request for this workspace — forever. Capture the reader in a local so
	// the goroutine never touches adapter state after a reset.
	reader := a.stdout
	resultCh := make(chan workerReadResult, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		resultCh <- workerReadResult{line: line, err: err}
	}()

	var line []byte
	select {
	case res := <-resultCh:
		line, err = res.line, res.err
	case <-time.After(workerReadTimeout):
		return ToolResult{}, a.recoverStalledWorkerLocked(resultCh)
	}
	if err != nil {
		// Reset first: resetWorkerLocked calls cmd.Wait(), which reaps the
		// child and establishes a happens-before edge with exec's internal
		// stderr-copy goroutine. Reading the buffer before Wait races with
		// that goroutine (bytes.Buffer is not concurrency-safe).
		a.resetWorkerLocked()
		errMsg := strings.TrimSpace(a.stderr.String())
		if errMsg == "" {
			return ToolResult{}, fmt.Errorf("typescript worker read failed: %w", err)
		}
		return ToolResult{}, fmt.Errorf("typescript worker read failed: %w (%s)", err, errMsg)
	}

	var result ToolResult
	if err := json.Unmarshal(bytes.TrimSpace(line), &result); err != nil {
		return ToolResult{}, fmt.Errorf("typescript worker response parse failed: %w", err)
	}
	return result, nil
}

func (a *typeScriptSessionAdapter) Healthy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	if a.cmd == nil || a.cmd.Process == nil {
		return false
	}
	return a.cmd.ProcessState == nil
}

func (a *typeScriptSessionAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	a.resetWorkerLocked()
	return nil
}

func (a *typeScriptSessionAdapter) ensureWorkerLocked(workspaceRoot string) error {
	if a.cmd != nil && a.cmd.Process != nil && a.cmd.ProcessState == nil {
		return nil
	}

	cmd := exec.Command("node", "-e", typeScriptNodeWorkerScript)
	cmd.Dir = workspaceRoot

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("typescript worker stdin pipe failed: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("typescript worker stdout pipe failed: %w", err)
	}
	a.stderr.Reset()
	cmd.Stderr = &a.stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("typescript worker start failed: %w", err)
	}

	a.cmd = cmd
	a.stdin = stdin
	a.stdout = bufio.NewReader(stdout)
	return nil
}

func (a *typeScriptSessionAdapter) resetWorkerLocked() {
	if a.stdin != nil {
		_ = a.stdin.Close()
		a.stdin = nil
	}
	if a.cmd != nil {
		if a.cmd.Process != nil && a.cmd.ProcessState == nil {
			_ = a.cmd.Process.Kill()
		}
		_ = a.cmd.Wait()
		a.cmd = nil
	}
	a.stdout = nil
}

// killWorkerLocked forcibly terminates the worker process and closes its
// stdin. Killing the process closes its stdout pipe, which unblocks any
// in-flight read. It does not reap the process or clear adapter state;
// callers must follow up with resetWorkerLocked once the read has drained.
func (a *typeScriptSessionAdapter) killWorkerLocked() {
	if a.stdin != nil {
		_ = a.stdin.Close()
		a.stdin = nil
	}
	if a.cmd != nil && a.cmd.Process != nil && a.cmd.ProcessState == nil {
		_ = a.cmd.Process.Kill()
	}
}

// recoverStalledWorkerLocked handles a response read that exceeded
// workerReadTimeout. It kills the wedged worker FIRST — the kill closes the
// stdout pipe, which unblocks the read goroutine — then waits briefly for the
// goroutine to drain so it is not leaked, resets the worker state so the next
// Run spawns a fresh process, and returns the timeout error.
func (a *typeScriptSessionAdapter) recoverStalledWorkerLocked(resultCh chan workerReadResult) error {
	a.killWorkerLocked()
	readErr := errors.New("typescript worker read interrupted")
	select {
	case res := <-resultCh:
		if res.err != nil {
			readErr = res.err
		}
	case <-time.After(time.Second):
		// The process is already killed; the blocked read will error out on
		// its own once the pipe fully drains, and the buffered channel lets
		// the goroutine exit without a receiver.
	}
	a.resetWorkerLocked()
	return fmt.Errorf("typescript worker timed out after %v: %w", workerReadTimeout, readErr)
}
