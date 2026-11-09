//go:build !js

package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// This file stops an agent from starting a second copy of a command that is
// already running as a background session. Both kinds of session are found:
// one started with background=true and one promoted from the foreground when
// it hit the tool deadline.
//
// Two properties matter besides matching:
//
//   - Only a session that is still running blocks a start. Once it has
//     exited, or was stopped (which drops it from the manager), the same
//     command starts normally.
//   - Checking and starting are one step. A plain check followed by a start
//     lets two identical concurrent tool calls both pass the check before
//     either registers its session. CommandPromise closes that window: the
//     admitted caller reserves the command under the manager lock, and a
//     concurrent identical caller sees either the reservation or the
//     session it became.

// normalizeCommandText is the "same command" comparison: surrounding
// whitespace trimmed and every run of whitespace collapsed to one space.
// Nothing else is normalized — an extra argument, flag or quote is a
// different command and still runs.
func normalizeCommandText(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

// RunningSessionMatching returns the still-running background session whose
// command and working directory match command in dir exactly (after
// normalizeCommandText). It is the read-only query; paths that then start a
// process must use CommandPromise / ClaimCommandPromise /
// ReleaseCommandPromise instead, so concurrent identical calls cannot both
// be admitted.
func (m *BackgroundProcessManager) RunningSessionMatching(command, dir string) (string, time.Duration, bool) {
	want := normalizeCommandText(command)
	if want == "" {
		return "", 0, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.findRunningMatchingLocked(want, normalizeSessionDir(dir), time.Now())
}

// findRunningMatchingLocked scans running sessions and pending
// reservations for want in wantDir. The caller holds m.mu.
func (m *BackgroundProcessManager) findRunningMatchingLocked(want, wantDir string, now time.Time) (string, time.Duration, bool) {
	for _, proc := range m.processes {
		proc.mu.Lock()
		running := proc.Process != nil
		procCommand := normalizeCommandText(proc.Command)
		procDir := normalizeSessionDir(proc.Dir)
		startedAt := proc.StartedAt
		id := proc.ID
		proc.mu.Unlock()
		if !running || procCommand != want || procDir != wantDir {
			continue
		}
		return id, now.Sub(startedAt), true
	}
	for _, promise := range m.promises {
		if promise.command == want && promise.dir == wantDir {
			return promise.ID, now.Sub(promise.startedAt), true
		}
	}
	return "", 0, false
}

// CommandPromise reserves a command for a caller that is about to start it:
//
//  1. CommandPromise(command, dir) — admitted, and the command is reserved
//     so an identical concurrent call is refused;
//  2. start the process;
//  3. ClaimCommandPromise(promise, sessionID) — the reservation becomes the
//     session, which a later identical call sees as running.
//
// A caller whose start failed calls ReleaseCommandPromise instead. Every
// admitted caller must do exactly one of the two, so a reservation never
// outlives the attempt it guards.
//
// busy reports an existing session (busySessionID) for the same command.
func (m *BackgroundProcessManager) CommandPromise(command, dir string) (promise *CommandPromise, busySessionID string, elapsed time.Duration, busy bool) {
	want := normalizeCommandText(command)
	if want == "" {
		// Empty commands are rejected by the start paths; nothing to guard.
		return nil, "", 0, false
	}
	wantDir := normalizeSessionDir(dir)
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	if id, age, found := m.findRunningMatchingLocked(want, wantDir, now); found {
		return nil, id, age, true
	}
	if m.promises == nil {
		// Managers built without the constructor (tests) have no map yet.
		m.promises = make(map[string]*CommandPromise)
	}
	promise = &CommandPromise{
		ID:        fmt.Sprintf("%s%d", PendingSessionIDPrefix, m.nextPromiseSeq),
		command:   want,
		dir:       wantDir,
		startedAt: now,
	}
	m.nextPromiseSeq++
	m.promises[promise.ID] = promise
	if len(m.promises) > bgPromiseMax {
		// A promise is held only for one start. A leak here is a bug, not a
		// state to honour: drop the oldest rather than block a command
		// forever.
		m.dropOldestPromiseLocked()
	}
	return promise, "", 0, false
}

// dropOldestPromiseLocked evicts the longest-held reservation, so a leaked
// one cannot accumulate. Caller holds m.mu.
func (m *BackgroundProcessManager) dropOldestPromiseLocked() {
	var oldestID string
	var oldestAt time.Time
	for id, p := range m.promises {
		if oldestID == "" || p.startedAt.Before(oldestAt) {
			oldestID, oldestAt = id, p.startedAt
		}
	}
	if oldestID != "" {
		delete(m.promises, oldestID)
	}
}

// bgPromiseMax bounds the reservation map; see CommandPromise.
const bgPromiseMax = 64

// ClaimCommandPromise turns a reservation into the started session, so later
// identical calls find it as a running session. sessionID "" keeps the
// reservation id, which is what the caller reports.
func (m *BackgroundProcessManager) ClaimCommandPromise(promise *CommandPromise, sessionID string) {
	if promise == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.promises, promise.ID)
	if sessionID != "" {
		promise.ID = sessionID
	}
}

// ReleaseCommandPromise abandons a reservation whose start failed.
func (m *BackgroundProcessManager) ReleaseCommandPromise(promise *CommandPromise) {
	if promise == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.promises, promise.ID)
}

// CommandPromise is a reserved slot for a command that is being started.
// Its ID is the session ID to name in a refusal until the reservation is
// claimed.
type CommandPromise struct {
	ID string

	command   string
	dir       string
	startedAt time.Time
}

// normalizeSessionDir reduces a working directory to the form sessions store,
// so a caller passing "." matches a session recorded with the real path.
func normalizeSessionDir(dir string) string {
	if abs, err := filepath.Abs(strings.TrimSpace(dir)); err == nil {
		return abs
	}
	return strings.TrimSpace(dir)
}
