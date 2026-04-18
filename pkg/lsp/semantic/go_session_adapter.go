package semantic

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var errGoplsNotAvailable = errors.New("gopls_not_available")

type goSessionAdapter struct {
	mu         sync.Mutex
	closed     bool
	goplsPath  string
	serverCmd  *exec.Cmd
	serverErr  bytes.Buffer
	remoteAddr string
	tmpDir     string
	socketPath string
}

// NewGoSessionPool creates a reusable per-workspace adapter pool for Go.
// Diagnostics remain local and stateless, while definitions are routed through
// a persistent gopls server for faster repeated lookups.
func NewGoSessionPool(idleTTL time.Duration) *SessionPool {
	return NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		return &goSessionAdapter{}, nil
	}, idleTTL)
}

func (a *goSessionAdapter) Run(input ToolInput) (ToolResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return ToolResult{}, fmt.Errorf("go session closed")
	}

	switch input.Method {
	case "diagnostics":
		return runGoDiagnostics(input)
	case "definition":
		if err := a.ensureServerLocked(input.WorkspaceRoot); err != nil {
			if errors.Is(err, errGoplsNotAvailable) {
				return ToolResult{
					Capabilities: Capabilities{Diagnostics: true, Definition: false},
					Error:        "gopls_not_available",
				}, nil
			}
			return ToolResult{}, err
		}

		result, err := runGoDefinitionWithRemote(input, a.goplsPath, a.remoteAddr)
		if err != nil {
			a.resetServerLocked()
		}
		return result, err
	default:
		return ToolResult{Capabilities: Capabilities{}}, nil
	}
}

func (a *goSessionAdapter) Healthy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return false
	}
	if a.serverCmd == nil {
		return true
	}
	if a.serverCmd.Process == nil {
		return false
	}
	return a.serverCmd.ProcessState == nil
}

func (a *goSessionAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	a.resetServerLocked()
	return nil
}

func (a *goSessionAdapter) ensureServerLocked(workspaceRoot string) error {
	if a.serverCmd != nil && a.serverCmd.Process != nil && a.serverCmd.ProcessState == nil {
		return nil
	}

	if a.goplsPath == "" {
		path, err := exec.LookPath("gopls")
		if err != nil {
			return errGoplsNotAvailable
		}
		a.goplsPath = path
	}

	tmpDir, err := os.MkdirTemp("", "ledit-gopls-*")
	if err != nil {
		return fmt.Errorf("failed to create gopls temp dir: %w", err)
	}
	socketPath := filepath.Join(tmpDir, "gopls.sock")
	remoteAddr := "unix;" + socketPath

	cmd := exec.Command(a.goplsPath, "serve", "-listen="+remoteAddr)
	cmd.Dir = workspaceRoot
	a.serverErr.Reset()
	cmd.Stderr = &a.serverErr

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to start gopls server: %w", err)
	}

	a.serverCmd = cmd
	a.remoteAddr = remoteAddr
	a.tmpDir = tmpDir
	a.socketPath = socketPath

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(socketPath); statErr == nil {
			return nil
		}
		if cmd.ProcessState != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	errMsg := a.serverErr.String()
	a.resetServerLocked()
	if errMsg == "" {
		return fmt.Errorf("gopls server did not become ready")
	}
	return fmt.Errorf("gopls server failed to become ready: %s", errMsg)
}

func (a *goSessionAdapter) resetServerLocked() {
	if a.serverCmd != nil {
		if a.serverCmd.Process != nil && a.serverCmd.ProcessState == nil {
			_ = a.serverCmd.Process.Kill()
		}
		_ = a.serverCmd.Wait()
		a.serverCmd = nil
	}
	if a.tmpDir != "" {
		_ = os.RemoveAll(a.tmpDir)
	}
	a.remoteAddr = ""
	a.tmpDir = ""
	a.socketPath = ""
}
