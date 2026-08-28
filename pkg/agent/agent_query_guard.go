package agent

import (
	"errors"
	"time"
)

// QueryGuardOwner identifies what currently holds the agent's query guard.
type QueryGuardOwner struct {
	Source    string    // one of the QuerySource* constants
	StartedAt time.Time // when the holder acquired the guard
}

// Query source constants used for QueryGuardOwner.Source.
const (
	QuerySourceCLI        = "cli"
	QuerySourceWebUI      = "webui"
	QuerySourceAutoResume = "auto-resume"
	QuerySourceUnknown    = "unknown"
)

// TryBeginQuery attempts to mark this Agent as "query in progress." Returns
// ErrQueryInProgress if a query is already running on this Agent instance.
// The caller MUST call EndQuery when done (typically via defer) to release
// the flag.
//
// This is the concurrency guard for shared-agent mode: when the CLI REPL and
// the WebUI use the same *Agent (non-daemon interactive mode), only one
// ProcessQuery can execute at a time. The losing caller gets the error and
// must either retry or present a "busy" message to the user.
//
// For standalone daemon mode (separate agents per chat session) this flag is
// never contended because each chat has its own Agent, so it's effectively
// a no-op.
func (a *Agent) TryBeginQuery() error {
	return a.TryBeginQueryAs(QuerySourceUnknown)
}

// TryBeginQueryAs marks this Agent as "query in progress" and records the
// caller source so busy-state messages can name the actual holder.
func (a *Agent) TryBeginQueryAs(source string) error {
	if a == nil {
		return errors.New("agent is nil")
	}
	if !a.queryInProgress.CompareAndSwap(false, true) {
		return ErrQueryInProgress
	}
	a.queryOwnerMu.Lock()
	a.queryOwner = QueryGuardOwner{Source: source, StartedAt: time.Now()}
	a.queryOwnerMu.Unlock()
	return nil
}

// EndQuery releases the "query in progress" flag set by TryBeginQuery.
// Safe to call multiple times and safe to call when the flag is already
// clear (idempotent).
func (a *Agent) EndQuery() {
	if a == nil {
		return
	}
	// Clear the owner BEFORE releasing the flag: a new acquirer's CAS can
	// only succeed after the Store(false) below, so this ordering guarantees
	// a late owner-clear can never clobber the new holder's owner record.
	a.queryOwnerMu.Lock()
	a.queryOwner = QueryGuardOwner{}
	a.queryOwnerMu.Unlock()
	a.queryInProgress.Store(false)
}

// QueryGuardOwner reports which source currently holds the query guard and
// since when, for accurate busy-state messaging.
func (a *Agent) QueryGuardOwner() QueryGuardOwner {
	if a == nil {
		return QueryGuardOwner{}
	}
	a.queryOwnerMu.Lock()
	defer a.queryOwnerMu.Unlock()
	return a.queryOwner
}

// IsQueryInProgress reports whether a query is currently executing on this
// Agent. Used by the WebUI to report busy state and by the CLI to check
// before starting a new query.
func (a *Agent) IsQueryInProgress() bool {
	if a == nil {
		return false
	}
	return a.queryInProgress.Load()
}
