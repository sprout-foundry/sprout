// Package tools provides the interface-based tool system for the Sprout AI agent.
//
// This file implements the heartbeat monitor for long-running jobs (SP-046-4).
// The browser sends periodic heartbeats over the WebSocket; the container
// monitors them and terminates abandoned sessions after the threshold.
// See roadmap/SP-046-workspace-sync-model.md §4 for the full specification.

package tools

import (
	"fmt"
	"sync"
	"time"
)

// eventWorkspaceHeartbeatLost mirrors events.EventTypeWorkspaceHeartbeatLost.
// Defined locally to avoid an import-cycle dependency.
const eventWorkspaceHeartbeatLost = "workspace.heartbeat_lost"

// SessionHeartbeat tracks the heartbeat state for a single session.
type SessionHeartbeat struct {
	// SessionID is the unique identifier for this session.
	SessionID string
	// LastHeartbeat is the timestamp of the most recent heartbeat received.
	LastHeartbeat time.Time
	// JobTerminated is called when the heartbeat threshold is exceeded.
	// It receives the sessionID so the caller can clean up resources.
	// If nil, no action is taken on timeout.
	JobTerminated func(sessionID string)
}

// HeartbeatMonitor tracks heartbeat pings from browser sessions and
// automatically terminates abandoned jobs after a configurable threshold.
//
// The monitor runs a background goroutine (started via StartMonitor) that
// periodically checks all registered sessions. When a session's last
// heartbeat exceeds the threshold, the monitor:
//   1. Publishes an EventTypeWorkspaceHeartbeatLost event (if publisher is set)
//   2. Calls the session's JobTerminated callback (if registered)
//   3. Removes the session from the map
type HeartbeatMonitor struct {
	mu         sync.Mutex
	sessions   map[string]*SessionHeartbeat
	publisher  EventPublisher
	done       chan struct{}
	stopped    bool
	monitoring bool
}

// NewHeartbeatMonitor creates a new HeartbeatMonitor with the given event
// publisher. The publisher can be nil (the monitor will still track
// sessions but won't emit events on timeout).
func NewHeartbeatMonitor(publisher EventPublisher) *HeartbeatMonitor {
	return &HeartbeatMonitor{
		sessions:  make(map[string]*SessionHeartbeat),
		publisher: publisher,
		done:      make(chan struct{}),
	}
}

// RecordHeartbeat records a heartbeat timestamp for the given session.
// If the session doesn't exist yet, it is created with JobTerminated set to
// nil. Thread-safe via mutex.
func (m *HeartbeatMonitor) RecordHeartbeat(sessionID string, ts time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionID]
	if !ok {
		m.sessions[sessionID] = &SessionHeartbeat{
			SessionID:     sessionID,
			LastHeartbeat: ts,
		}
		return
	}
	s.LastHeartbeat = ts
}

// RegisterJob registers a job termination callback for a session. When the
// heartbeat threshold is exceeded for this session, the callback will be
// invoked with the sessionID. If the session doesn't exist yet, it is
// created with LastHeartbeat set to the current time. Thread-safe.
func (m *HeartbeatMonitor) RegisterJob(sessionID string, terminate func(sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionID]
	if !ok {
		m.sessions[sessionID] = &SessionHeartbeat{
			SessionID:     sessionID,
			LastHeartbeat: time.Now(),
			JobTerminated: terminate,
		}
		return
	}
	s.JobTerminated = terminate
}

// StartMonitor begins the background goroutine that periodically checks
// all registered sessions for missed heartbeats. The interval parameter
// controls how often the check runs (e.g. 15s in production), and the
// threshold defines how long without a heartbeat before a session is
// considered abandoned (e.g. 60s).
//
// The goroutine runs until Stop() is called. Calling StartMonitor multiple
// times is safe — only one monitor goroutine runs at a time.
func (m *HeartbeatMonitor) StartMonitor(interval, threshold time.Duration) {
	m.mu.Lock()
	if m.monitoring {
		m.mu.Unlock()
		return
	}
	m.monitoring = true
	m.mu.Unlock()

	ticker := time.NewTicker(interval)
	go func() {
		defer func() {
			ticker.Stop()
			m.mu.Lock()
			m.monitoring = false
			m.mu.Unlock()
		}()
		for {
			select {
			case <-m.done:
				return
			case <-ticker.C:
				m.checkSessions(threshold)
			}
		}
	}()
}

// checkSessions iterates all tracked sessions and terminates any whose
// last heartbeat exceeds the threshold. This method manages its own locking.
func (m *HeartbeatMonitor) checkSessions(threshold time.Duration) {
	m.mu.Lock()

	now := time.Now()
	type terminationAction struct {
		sessionID     string
		lastHeartbeat time.Time
		cb            func(string)
	}
	var actions []terminationAction
	publisher := m.publisher

	expired := make([]string, 0)
	for id, s := range m.sessions {
		if now.Sub(s.LastHeartbeat) > threshold {
			expired = append(expired, id)
			actions = append(actions, terminationAction{
				sessionID:     id,
				lastHeartbeat: s.LastHeartbeat,
				cb:            s.JobTerminated,
			})
		}
	}
	m.mu.Unlock()

	// Execute outside the lock to avoid holding mutex during I/O and callbacks.
	for _, a := range actions {
		if publisher != nil {
			publisher.Publish(eventWorkspaceHeartbeatLost, map[string]interface{}{
				"session_id":     a.sessionID,
				"last_heartbeat": a.lastHeartbeat.Format(time.RFC3339),
			})
		}
		if a.cb != nil {
			func() {
				defer func() { _ = recover() }()
				a.cb(a.sessionID)
			}()
		}
	}

	// Re-acquire to delete from map.
	m.mu.Lock()
	for _, id := range expired {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
}

// Stop signals the monitor goroutine to shut down. Safe to call multiple
// times — subsequent calls after the first are no-ops.
func (m *HeartbeatMonitor) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	done := m.done
	m.mu.Unlock()
	close(done)
}

// GetSession returns a copy of the heartbeat state for a session, or nil
// if the session is not tracked. Thread-safe.
func (m *HeartbeatMonitor) GetSession(sessionID string) *SessionHeartbeat {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	cp := *s
	return &cp
}

// GetActiveCount returns the number of currently tracked sessions.
// Thread-safe.
func (m *HeartbeatMonitor) GetActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// RemoveSession removes a session from the monitor without emitting an
// event or calling the termination callback. Use this for normal session
// teardown (e.g., job completes successfully). Thread-safe.
func (m *HeartbeatMonitor) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// GetSessionIDs returns a snapshot of all tracked session IDs. Thread-safe.
func (m *HeartbeatMonitor) GetSessionIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// HeartbeatLostError is returned when a heartbeat has been missed for the
// configured threshold. Used by callers to detect abandonment without
// relying solely on the event bus.
type HeartbeatLostError struct {
	SessionID     string
	LastHeartbeat time.Time
}

func (e *HeartbeatLostError) Error() string {
	return fmt.Sprintf(
		"heartbeat lost for session %s (last heartbeat %s, elapsed %s)",
		e.SessionID,
		e.LastHeartbeat.Format(time.RFC3339),
		time.Since(e.LastHeartbeat).Round(time.Second),
	)
}
