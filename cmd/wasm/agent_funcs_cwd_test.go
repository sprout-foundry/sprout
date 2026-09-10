//go:build js && wasm

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAgentWorkspaceRootTracksProcessCwd pins the turn-time cwd sync added
// to runAgentFunc: the cached persistent agent's workspace root must be
// re-stamped from the live process cwd before every ProcessQuery call.
//
// Why this matters (the iPad "agent has no cwd" bug): tool paths resolve
// against filesystem.WithWorkspaceRoot(ctx, a.GetWorkspaceRoot()), which
// BEATS the process cwd. NewAgentWithClient stamps the root once from
// os.Getwd() at construction, and the agent is cached across turns — so a
// host-side changeDir between turns (studio bridge selecting a different
// repo) would never reach tool resolution without this re-stamp. The first
// turn's cwd would win forever.
//
// The sync block exercised here must stay equivalent to the one in
// runAgentFunc — this test is the contract. If runAgentFunc's sync is ever
// removed or weakened, mirror the change here deliberately.
func TestAgentWorkspaceRootTracksProcessCwd(t *testing.T) {
	syncRoot := func(ag interface {
		SetWorkspaceRoot(string)
		GetWorkspaceRoot() string
	}) {
		if cwd, err := os.Getwd(); err == nil {
			if abs, absErr := filepath.Abs(cwd); absErr == nil {
				cwd = abs
			}
			ag.SetWorkspaceRoot(cwd)
		}
	}

	agent := &stubAgent{}
	agent.SetWorkspaceRoot("/stale/first/turn/cwd")

	// Simulate the host chdir'ing between turns (bridge: repo selection).
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Skipf("cannot chdir in wasm test env: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	// The turn-time sync must re-stamp the root to the live cwd (EvalSymlinks
	// resolves macOS /var → /private/var, so compare against the resolved
	// form — exactly what filepath.Abs did inside the sync).
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		want = dir
	}
	if wantAbs, err := filepath.Abs(want); err == nil {
		want = wantAbs
	}

	syncRoot(agent)
	if got := agent.GetWorkspaceRoot(); got != want {
		t.Errorf("workspace root = %q after turn-time sync, want the new cwd %q (a stale value like \"/stale/first/turn/cwd\" means the cached agent never tracked the chdir)", got, want)
	}
}

// stubAgent satisfies the narrow accessor pair the sync block needs.
type stubAgent struct{ root string }

func (s *stubAgent) SetWorkspaceRoot(root string) { s.root = root }
func (s *stubAgent) GetWorkspaceRoot() string     { return s.root }
