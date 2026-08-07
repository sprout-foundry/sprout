//go:build !js

package webui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/require"
)

// session describes one concurrent client session in the multi-workspace gate
// test: a client ID, the workspace it binds to, and the embedding index dir
// that identifies its workspace's manager.
type session struct {
	clientID   string
	workspace  string
	managerKey string // embedding index dir for this workspace
}

// TestMultiWorkspaceConcurrentSessions is the SP-136 Phase-1 integration gate.
//
// It simulates 5 concurrent CLI/WebUI sessions across 3 workspaces sharing one
// daemon (a single ReactWebServer on one port). It verifies:
//
//  1. Every session completes without hanging, panicking, or returning an
//     unexpected error (agent creation fast-fails with a recognized
//     provider-config sentinel when no model is installed — the important
//     property is that the plumbing routes correctly and never hangs).
//  2. Client contexts are isolated per session: each has its own workspace
//     root, and no session sees another session's workspace.
//  3. Embedding managers are isolated per workspace: two sessions in the same
//     workspace share ONE manager (daemon dedup), while different workspaces
//     get DIFFERENT managers (no cross-workspace index sharing).
//  4. The daemon's RSS stays bounded across the concurrent run (Linux only —
//     the daemon runs in-process here, so /proc/self is the daemon's RSS).
func TestMultiWorkspaceConcurrentSessions(t *testing.T) {
	// Isolate session-state persistence: if a provider IS configured in the
	// test environment, getChatAgent builds a real Agent whose autoSaveState
	// would otherwise write into the package-level state dir.
	defer agent.NewTestStateDir(t)()

	daemonRoot := t.TempDir()

	// Three workspaces under the daemon root (required by setClientWorkspaceRoot).
	workspaceDirs := []string{
		filepath.Join(daemonRoot, "w1"),
		filepath.Join(daemonRoot, "w2"),
		filepath.Join(daemonRoot, "w3"),
	}
	for _, w := range workspaceDirs {
		require.NoError(t, os.MkdirAll(w, 0o755))
		// A small Go file so the workspace looks like a real code project.
		require.NoError(t, os.WriteFile(
			filepath.Join(w, "main.go"),
			[]byte("package main\nfunc main() {}\n"), 0o644))
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	require.NoError(t, err)
	ws.daemonRoot = daemonRoot
	ws.SetWorkspaceRoot(workspaceDirs[0])

	// 5 sessions across 3 workspaces: w1×2, w2×2, w3×1.
	sessions := []session{
		{"sess-1", workspaceDirs[0], filepath.Join(daemonRoot, "idx", "w1")},
		{"sess-2", workspaceDirs[0], filepath.Join(daemonRoot, "idx", "w1")},
		{"sess-3", workspaceDirs[1], filepath.Join(daemonRoot, "idx", "w2")},
		{"sess-4", workspaceDirs[1], filepath.Join(daemonRoot, "idx", "w2")},
		{"sess-5", workspaceDirs[2], filepath.Join(daemonRoot, "idx", "w3")},
	}
	require.Len(t, sessions, 5)
	workspaceCount := map[string]int{}
	for _, s := range sessions {
		workspaceCount[s.workspace]++
	}
	require.Len(t, workspaceCount, 3, "test must cover 3 distinct workspaces")

	startRSS := processRSSKB(t)

	// Run all 5 sessions concurrently behind a start barrier.
	start := make(chan struct{})
	errCh := make(chan error, len(sessions))
	managers := make([]*embedding.EmbeddingManager, len(sessions))
	var mgrMu sync.Mutex

	var wg sync.WaitGroup
	for i, s := range sessions {
		wg.Add(1)
		go func(i int, s session) {
			defer wg.Done()
			<-start
			errCh <- runConcurrentSession(t, ws, s, func(m *embedding.EmbeddingManager) {
				mgrMu.Lock()
				managers[i] = m
				mgrMu.Unlock()
			})
		}(i, s)
	}
	close(start)
	wg.Wait()
	close(errCh)

	// 1. All sessions complete without error (recognized provider sentinels
	//    are acceptable — they prove correct routing, not a hang).
	for err := range errCh {
		require.NoError(t, err, "concurrent session failed")
	}

	// 2. Client contexts are isolated: distinct pointers, correct workspace.
	ws.mutex.RLock()
	distinctRoots := map[string]bool{}
	for _, s := range sessions {
		ctx := ws.clientContexts[s.clientID]
		require.NotNil(t, ctx, "client context for %s must exist", s.clientID)
		require.Equal(t, s.workspace, ctx.WorkspaceRoot,
			"client %s must be bound to its own workspace %s, got %s",
			s.clientID, s.workspace, ctx.WorkspaceRoot)
		distinctRoots[ctx.WorkspaceRoot] = true
	}
	ws.mutex.RUnlock()
	require.Len(t, distinctRoots, 3,
		"client contexts must map to exactly 3 distinct workspace roots, got %v", distinctRoots)

	// 3. Embedding manager isolation: same workspace → same manager pointer;
	//    different workspace → different manager pointer.
	require.NotNil(t, managers[0])
	for i := range managers {
		require.NotNil(t, managers[i], "session %d must have acquired an embedding manager", i)
	}
	require.Same(t, managers[0], managers[1], "sessions in w1 must share one embedding manager")
	require.Same(t, managers[2], managers[3], "sessions in w2 must share one embedding manager")
	require.NotSame(t, managers[0], managers[2], "w1 and w2 must have different embedding managers")
	require.NotSame(t, managers[0], managers[4], "w1 and w3 must have different embedding managers")
	require.NotSame(t, managers[2], managers[4], "w2 and w3 must have different embedding managers")

	// 4. Daemon RSS stays bounded across the concurrent run.
	endRSS := processRSSKB(t)
	if endRSS > 0 && startRSS > 0 {
		growthKB := endRSS - startRSS
		const maxGrowthKB = 256 * 1024 // 256 MB — very generous; real growth is a few MB
		require.Less(t, growthKB, maxGrowthKB,
			"daemon RSS grew by %d KB across 5 concurrent sessions (bounded check)", growthKB)
		t.Logf("daemon RSS bounded: %d KB before, %d KB after (%d KB growth)",
			startRSS, endRSS, growthKB)
	}

	// Shut down any agents the sessions created so background autoSaveState
	// timers are stopped before the test's state-dir isolation is torn down.
	ws.mutex.RLock()
	var agents []*agent.Agent
	for _, s := range sessions {
		if ctx := ws.clientContexts[s.clientID]; ctx != nil {
			agents = append(agents, ctx.Agent)
			for _, cs := range ctx.ChatSessions {
				if cs != nil {
					agents = append(agents, cs.Agent)
				}
			}
		}
	}
	ws.mutex.RUnlock()
	ws.releaseAgents("test_cleanup", agents...)
	ws.waitForAgentTeardown()
}

// runConcurrentSession drives one session through the daemon plumbing:
// set workspace root, create/get the default chat session, force agent
// plumbing via getChatAgent, and acquire the workspace embedding manager.
func runConcurrentSession(t *testing.T, ws *ReactWebServer, s session, onManager func(*embedding.EmbeddingManager)) error {
	t.Helper()

	// Bound each session so a hang fails the test instead of wedging CI.
	done := make(chan error, 1)
	go func() {
		done <- func() error {
			// Set workspace root — returns the resolved path.
			resolved, err := ws.setClientWorkspaceRoot(s.clientID, s.workspace)
			if err != nil {
				return fmt.Errorf("set workspace root: %w", err)
			}
			if resolved != s.workspace {
				return fmt.Errorf("workspace root mismatch: got %q want %q", resolved, s.workspace)
			}

			// Exercise agent-creation plumbing. Without a configured provider
			// this fast-fails with a sentinel; with one it creates a real
			// agent. Either is correct — a hang or an unrelated error is not.
			_, agentErr := ws.getChatAgent(s.clientID, "")
			if agentErr != nil {
				switch {
				case errors.Is(agentErr, ErrNoProviderConfigured),
					errors.Is(agentErr, agent.ErrModelNotAvailable),
					errors.Is(agentErr, agent.ErrProviderNotConfigured),
					isProviderConfigError(agentErr):
					// Expected: no provider/model in this environment.
				default:
					return fmt.Errorf("getChatAgent returned unexpected error: %w", agentErr)
				}
			}

			// Acquire the workspace-scoped embedding manager.
			mgr := embedding.AcquireManager(
				&configuration.EmbeddingIndexConfig{IndexDir: s.managerKey},
				s.workspace,
			)
			if mgr == nil {
				return fmt.Errorf("embedding manager acquisition returned nil for %s", s.clientID)
			}
			onManager(mgr)
			return nil
		}()
	}()

	select {
	case err := <-done:
		// Release any manager acquired by this session (matches AcquireManager).
		return err
	case <-time.After(30 * time.Second):
		return fmt.Errorf("session %s hung for 30s (daemon did not respond)", s.clientID)
	}
}

// processRSSKB returns the current process RSS in KB via /proc/self/status on
// Linux, or 0 on unsupported platforms (caller skips the bounded check).
func processRSSKB(t *testing.T) int {
	t.Helper()
	if runtime.GOOS != "linux" {
		return 0
	}
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.Atoi(fields[1])
				if err == nil {
					return kb
				}
			}
		}
	}
	return 0
}
