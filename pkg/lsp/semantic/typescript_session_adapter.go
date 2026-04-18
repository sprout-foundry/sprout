package semantic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

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

	line, err := a.stdout.ReadBytes('\n')
	if err != nil {
		errMsg := strings.TrimSpace(a.stderr.String())
		a.resetWorkerLocked()
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
