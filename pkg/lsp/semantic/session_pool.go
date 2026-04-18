package semantic

import (
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
	Healthy() bool
	// Close tears down the session and releases its resources.
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
func (p *SessionPool) acquire(workspaceRoot string) (SessionAdapter, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.sessions[workspaceRoot]; ok {
		if entry.adapter.Healthy() {
			entry.lastUsed = time.Now()
			entry.inUse++
			return entry.adapter, nil
		}
		_ = entry.adapter.Close()
		delete(p.sessions, workspaceRoot)
	}

	adapter, err := p.factory(workspaceRoot)
	if err != nil {
		return nil, err
	}
	p.sessions[workspaceRoot] = &sessionEntry{adapter: adapter, lastUsed: time.Now(), inUse: 1}
	return adapter, nil
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
