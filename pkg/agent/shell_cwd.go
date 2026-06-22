package agent

import "sync"

// shellCwdTracker maintains the logical shell working directory and
// its previous value, supporting `cd -` swap semantics. All methods
// are goroutine-safe.
type shellCwdTracker struct {
	mu      sync.RWMutex
	cwd     string
	prevCwd string
}

// Get returns the current logical shell working directory.
func (t *shellCwdTracker) Get() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cwd
}

// Set records the new cwd and stashes the old one as prevCwd.
func (t *shellCwdTracker) Set(dir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prevCwd = t.cwd
	t.cwd = dir
}

// SwapPrevious implements `cd -`: exchanges cwd and prevCwd.
func (t *shellCwdTracker) SwapPrevious() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prevCwd, t.cwd = t.cwd, t.prevCwd
}

// SetWithPrev records the new cwd and lets the caller control what
// gets stored as prev (used by updateShellCwd's cd - branch).
func (t *shellCwdTracker) SetWithPrev(newCwd, newPrev string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cwd = newCwd
	t.prevCwd = newPrev
}

// GetBoth returns cwd and prevCwd atomically (used by updateShellCwd
// which needs to read current, compute resolved, then write both).
func (t *shellCwdTracker) GetBoth() (cwd, prevCwd string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cwd, t.prevCwd
}

// ensureShellCwd returns the agent's shellCwd tracker, allocating it
// lazily on first access. This supports tests that construct bare
// &Agent{} structs without going through the production constructor.
func (a *Agent) ensureShellCwd() *shellCwdTracker {
	if a.shellCwd == nil {
		a.shellCwd = &shellCwdTracker{}
	}
	return a.shellCwd
}
