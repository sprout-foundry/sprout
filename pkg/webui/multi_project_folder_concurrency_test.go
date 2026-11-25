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
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/require"
)

// session describes one concurrent client session in the multi-project-folder gate
// test: a client ID and the project folder it binds to.
type session struct {
	clientID      string
	projectFolder string
}

// TestMultiProjectFolderConcurrentSessions is the SP-136 Phase-1 integration gate.
//
// It simulates 5 concurrent CLI/WebUI sessions across 3 project folders sharing one
// daemon (a single ReactWebServer on one port). It verifies:
//
//  1. Every session completes without hanging, panicking, or returning an
//     unexpected error (agent creation fast-fails with a recognized
//     provider-config sentinel when no model is installed — the important
//     property is that the plumbing routes correctly and never hangs).
//  2. Client contexts are isolated per session: each has its own project folder
//     root, and no session sees another session's project folder.
//  3. The daemon's RSS stays bounded across the concurrent run (Linux only —
//     the daemon runs in-process here, so /proc/self is the daemon's RSS).
func TestMultiProjectFolderConcurrentSessions(t *testing.T) {
	// Isolate session-state persistence: if a provider IS configured in the
	// test environment, getChatAgent builds a real Agent whose autoSaveState
	// would otherwise write into the package-level state dir.
	defer agent.NewTestStateDir(t)()

	daemonRoot := t.TempDir()

	// Canonicalize for macOS: /var is a symlink to /private/var and the
	// server resolves project folders via filepathAbsEval (EvalSymlinks).
	// The isolation assertions below compare raw paths, so everything must
	// be on the same (canonical) footing or they trip on the symlink.
	canonicalPath := func(p string) string {
		if evaled, err := filepath.EvalSymlinks(p); err == nil {
			return evaled
		}
		return p
	}
	daemonRoot = canonicalPath(daemonRoot)

	// Three project folders under the daemon root (required by setClientProjectFolder).
	projectFolderDirs := []string{
		filepath.Join(daemonRoot, "w1"),
		filepath.Join(daemonRoot, "w2"),
		filepath.Join(daemonRoot, "w3"),
	}
	for _, w := range projectFolderDirs {
		require.NoError(t, os.MkdirAll(w, 0o755))
		// A small Go file so the project folder looks like a real code project.
		require.NoError(t, os.WriteFile(
			filepath.Join(w, "main.go"),
			[]byte("package main\nfunc main() {}\n"), 0o644))
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	require.NoError(t, err)
	ws.daemonRoot = daemonRoot
	_, _ = ws.SetProjectFolder(projectFolderDirs[0])

	// 5 sessions across 3 project folders: w1×2, w2×2, w3×1.
	sessions := []session{
		{"sess-1", projectFolderDirs[0]},
		{"sess-2", projectFolderDirs[0]},
		{"sess-3", projectFolderDirs[1]},
		{"sess-4", projectFolderDirs[1]},
		{"sess-5", projectFolderDirs[2]},
	}
	require.Len(t, sessions, 5)
	projectFolderCount := map[string]int{}
	for _, s := range sessions {
		projectFolderCount[s.projectFolder]++
	}
	require.Len(t, projectFolderCount, 3, "test must cover 3 distinct project folders")

	startRSS := processRSSKB(t)

	// Run all 5 sessions concurrently behind a start barrier.
	start := make(chan struct{})
	errCh := make(chan error, len(sessions))
	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func(s session) {
			defer wg.Done()
			<-start
			errCh <- runConcurrentSession(t, ws, s)
		}(s)
	}
	close(start)
	wg.Wait()
	close(errCh)

	// 1. All sessions complete without error (recognized provider sentinels
	//    are acceptable — they prove correct routing, not a hang).
	for err := range errCh {
		require.NoError(t, err, "concurrent session failed")
	}

	// 2. Client contexts are isolated: distinct pointers, correct project folder.
	ws.mutex.RLock()
	distinctRoots := map[string]bool{}
	for _, s := range sessions {
		ctx := ws.clientContexts[s.clientID]
		require.NotNil(t, ctx, "client context for %s must exist", s.clientID)
		require.Equal(t, s.projectFolder, ctx.ProjectFolder,
			"client %s must be bound to its own project folder %s, got %s",
			s.clientID, s.projectFolder, ctx.ProjectFolder)
		distinctRoots[ctx.ProjectFolder] = true
	}
	ws.mutex.RUnlock()
	require.Len(t, distinctRoots, 3,
		"client contexts must map to exactly 3 distinct project folders, got %v", distinctRoots)

	// 3. Daemon RSS stays bounded across the concurrent run.
	endRSS := processRSSKB(t)
	if endRSS > 0 && startRSS > 0 {
		growthKB := endRSS - startRSS
		// 256 MB — very generous on a normal binary; real growth is a
		// few MB. Scaled under -race, whose shadow memory inflates peak
		// RSS several-fold (288MB observed vs the unscaled bound).
		const maxGrowthKB = 256 * 1024 * raceOverheadFactor
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
// set project folder, create/get the default chat session, and force agent
// plumbing via getChatAgent.
func runConcurrentSession(t *testing.T, ws *ReactWebServer, s session) error {
	t.Helper()

	// Bound each session so a hang fails the test instead of wedging CI.
	done := make(chan error, 1)
	go func() {
		done <- func() error {
			// Set project folder — returns the resolved path.
			resolved, err := ws.setClientProjectFolder(s.clientID, s.projectFolder)
			if err != nil {
				return fmt.Errorf("set project folder: %w", err)
			}
			if resolved != s.projectFolder {
				return fmt.Errorf("project folder mismatch: got %q want %q", resolved, s.projectFolder)
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
			return nil
		}()
	}()

	select {
	case err := <-done:
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
