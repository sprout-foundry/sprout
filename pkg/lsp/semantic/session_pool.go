package semantic

import (
	"fmt"
	"sync"
	"time"
)

// SessionAdapter extends Adapter with lifecycle management.
// Implement this for adapters that maintain persistent state (e.g. a long-lived
// language-server process) and should be reused across requests.
type SessionAdapter interface {
	Adapter
	// Healthy returns true if the session is still usable.
	// A false return causes the pool to close and replace the session.
	// Healthy must be safe for concurrent calls and must never block on a
	// mutex that a concurrent Run holds while the pool mutex is held — the
	// pool calls Healthy outside its own lock for exactly this reason.
	Healthy() bool
	// Close tears down the session and releases its resources.
	// Close must be idempotent and safe for concurrent calls: the pool may
	// close a session from multiple paths (eviction, TTL recycle, unhealthy
	// replacement) even while another goroutine is inside Healthy.
	Close() error
}

// SessionFactory creates a new SessionAdapter for a given workspace root.
// It is called once when no healthy session exists for that root.
type SessionFactory func(workspaceRoot string) (SessionAdapter, error)

// SessionPool manages one SessionAdapter per workspace root.
// When an adapter becomes unhealthy, it is closed and a new one is created on
// the next request. Idle sessions are evicted after idleTTL (0 = never).
//
// SessionPool implements Adapter so it can be registered directly via
// Registry.RegisterSingleton for any number of language IDs.
type SessionPool struct {
	factory  SessionFactory
	idleTTL  time.Duration
	mu       sync.Mutex
	sessions map[string]*sessionEntry
	// creating holds a per-root channel while a goroutine is creating that
	// root's session. Other goroutines wait on the channel, then retry.
	creating map[string]chan struct{}
}

type sessionEntry struct {
	adapter        SessionAdapter
	lastUsed       time.Time
	inUse          int
	evictOnRelease bool
}

// NewSessionPool creates a pool backed by factory.
// idleTTL controls when idle sessions are evicted; pass 0 to disable eviction.
func NewSessionPool(factory SessionFactory, idleTTL time.Duration) *SessionPool {
	return &SessionPool{
		factory:  factory,
		idleTTL:  idleTTL,
		sessions: make(map[string]*sessionEntry),
		creating: make(map[string]chan struct{}),
	}
}

// Run implements Adapter. It routes the request to the pooled session for
// input.WorkspaceRoot, creating one if needed.
func (p *SessionPool) Run(input ToolInput) (ToolResult, error) {
	adapter, err := p.acquire(input.WorkspaceRoot)
	if err != nil {
		return ToolResult{}, err
	}
	defer p.release(input.WorkspaceRoot, adapter)

	result, runErr := adapter.Run(input)
	if runErr != nil {
		// Mark for eviction; release() closes once no goroutine is using it.
		p.requestEvict(input.WorkspaceRoot, adapter)
	}
	return result, runErr
}

// EvictIdle closes sessions that have been idle longer than idleTTL.
// Call this periodically (e.g. from a background goroutine) to reclaim resources.
func (p *SessionPool) EvictIdle() {
	if p.idleTTL == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cutoff := time.Now().Add(-p.idleTTL)
	for root, entry := range p.sessions {
		if entry.lastUsed.Before(cutoff) && entry.inUse == 0 {
			_ = entry.adapter.Close()
			delete(p.sessions, root)
		}
	}
}

// Close shuts down all pooled sessions and empties the pool.
func (p *SessionPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for root, entry := range p.sessions {
		_ = entry.adapter.Close()
		delete(p.sessions, root)
	}
}

// acquire returns a healthy adapter for workspaceRoot, creating one if needed.
// The factory is always called OUTSIDE p.mu: creating a session (e.g. spawning
// a language server process) can take hundreds of milliseconds, and holding the
// pool lock during that would block every other workspace's acquire. Concurrent
// callers for the same root serialize on a per-root creation channel; the
// creator populates the cache, and waiters retry via a recursive acquire call.
func (p *SessionPool) acquire(workspaceRoot string) (SessionAdapter, error) {
	// Fast path: a healthy cached session is ready to use.
	if adapter := p.tryTakeHealthy(workspaceRoot); adapter != nil {
		return adapter, nil
	}

	// Claim (or join) the single in-flight creation for this root.
	p.mu.Lock()
	// A creator may have finished (installed its session and closed the claim
	// channel) while we were blocked on this lock; its entry is already in
	// p.sessions. Prefer it over claiming a new creation — otherwise a caller
	// that missed the fast path by microseconds becomes a second creator and
	// spawns a redundant factory call.
	if _, ok := p.sessions[workspaceRoot]; ok {
		p.mu.Unlock()
		if adapter := p.tryTakeHealthy(workspaceRoot); adapter != nil {
			return adapter, nil
		}
		// The installed session is unhealthy (re-validated by tryTakeHealthy);
		// fall through and claim creation to replace it.
		p.mu.Lock()
	}
	creator, hadCreator := p.creating[workspaceRoot]
	if !hadCreator {
		creator = make(chan struct{})
		p.creating[workspaceRoot] = creator
	}
	p.mu.Unlock()

	if hadCreator {
		// Wait for the creator to finish, then retry: it either populated the
		// cache (we take the fast path) or failed (we become the new creator).
		<-creator
		return p.acquire(workspaceRoot)
	}

	// Creator path: build the session outside the pool lock. A factory panic
	// must not strand waiters on an unclosed creator channel, so convert it to
	// an error and let the normal error path clean up the claim.
	adapter, err := func() (a SessionAdapter, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("session factory panicked: %v", r)
			}
		}()
		return p.factory(workspaceRoot)
	}()

	p.mu.Lock()
	delete(p.creating, workspaceRoot)
	if err != nil {
		close(creator)
		p.mu.Unlock()
		return nil, err
	}
	// Install BEFORE close(creator): every woken waiter finds the entry in the
	// fast path and never re-enters the factory for this root.
	if _, ok := p.sessions[workspaceRoot]; !ok {
		p.sessions[workspaceRoot] = &sessionEntry{adapter: adapter, lastUsed: time.Now(), inUse: 1}
		close(creator)
		p.mu.Unlock()
		return adapter, nil
	}
	// Lost the install race to another creator. Close the claim channel so
	// waiters can retry, then take the winner's session (or fall through).
	close(creator)
	p.mu.Unlock()
	if existing := p.tryTakeHealthy(workspaceRoot); existing != nil {
		_ = adapter.Close()
		return existing, nil
	}
	_ = adapter.Close()
	return p.acquire(workspaceRoot)
}

// tryTakeHealthy returns the cached healthy session for workspaceRoot, marked
// in use, or nil if none is available. It must NOT be called while holding
// p.mu: Healthy() can block behind an adapter's in-flight round-trip, and
// holding the pool lock across that wait would deadlock against that
// adapter's release(). The entry is re-validated under the lock afterwards so
// a concurrently-replaced session is never handed out.
func (p *SessionPool) tryTakeHealthy(workspaceRoot string) SessionAdapter {
	p.mu.Lock()
	entry, ok := p.sessions[workspaceRoot]
	if !ok {
		p.mu.Unlock()
		return nil
	}
	adapter := entry.adapter
	p.mu.Unlock()

	if !adapter.Healthy() {
		// Unhealthy cached session: close it outside the lock and let the
		// caller fall through to creation.
		_ = adapter.Close()
		p.mu.Lock()
		if cur, ok := p.sessions[workspaceRoot]; ok && cur.adapter == adapter {
			delete(p.sessions, workspaceRoot)
		}
		p.mu.Unlock()
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if cur, ok := p.sessions[workspaceRoot]; ok && cur.adapter == adapter {
		// Recycle a session that sat idle past the TTL. Checked here (while
		// the entry is not in use) so release() recycles based on true idle
		// time, not on how long the just-finished run held the session.
		if p.idleTTL > 0 && cur.inUse == 0 && time.Since(cur.lastUsed) > p.idleTTL {
			cur.evictOnRelease = true
		}
		cur.lastUsed = time.Now()
		cur.inUse++
		return adapter
	}
	return nil
}

// requestEvict marks the specific session for eviction once it is no longer in use.
func (p *SessionPool) requestEvict(workspaceRoot string, adapter SessionAdapter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.sessions[workspaceRoot]; ok {
		if entry.adapter != adapter {
			return
		}
		if entry.inUse == 0 {
			_ = entry.adapter.Close()
			delete(p.sessions, workspaceRoot)
			return
		}
		entry.evictOnRelease = true
	}
}

// release decrements the in-use reference and applies deferred eviction if requested.
func (p *SessionPool) release(workspaceRoot string, adapter SessionAdapter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.sessions[workspaceRoot]; ok {
		if entry.adapter != adapter {
			return
		}
		if entry.inUse > 0 {
			entry.inUse--
		}
		entry.lastUsed = time.Now()
		if entry.inUse == 0 && entry.evictOnRelease {
			_ = entry.adapter.Close()
			delete(p.sessions, workspaceRoot)
		}
	}
}
