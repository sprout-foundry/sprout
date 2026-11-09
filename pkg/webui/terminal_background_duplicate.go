//go:build !js

package webui

import (
	"fmt"
	"strings"
	"time"

	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// Duplicate background-command guard for hidden PTY sessions.
//
// The agent can issue parallel shell calls, so the check and the start have
// to be one step. reserveDuplicateCommand runs the match and the reservation
// under tm.mutex, and the caller holds the reservation until its session is
// registered. Two identical calls at the same moment therefore admit one
// session, and the other is refused with a message naming what is running.
//
// Matching is agent_tools' rule (equal after trimming and collapsing
// whitespace), scoped to one chat.

// normalizeBackgroundCommand is the duplicate-detection form of a command.
func normalizeBackgroundCommand(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

// reserveDuplicateCommand reserves command for chatID unless an equivalent
// command is already running as a background session there (started with
// background=true or promoted at the tool deadline) or already reserved by a
// concurrent start.
//
// busy reports why not to start: busyID is the running session's ID, or the
// in-flight start's session ID when one has been created already. The caller
// must call promise.release exactly once, after its session is registered or
// its start failed; promise is nil when busy.
func (tm *TerminalManager) reserveDuplicateCommand(chatID, command string) (promise *bgReservation, busyID string, elapsed time.Duration, busy bool) {
	want := normalizeBackgroundCommand(command)
	if want == "" {
		return nil, "", 0, false
	}

	now := time.Now()
	tm.mutex.Lock()
	if id, age, found := tm.findRunningBackgroundLocked(chatID, want, now); found {
		tm.mutex.Unlock()
		return nil, id, age, true
	}
	// A real id from the start: a refusal that lands between this
	// reservation and the session's creation names this, so the agent is
	// never told about a command running under an empty id. The session's
	// own id replaces it once it exists (publish).
	tm.nextPendingCommandSeq++
	promise = &bgReservation{
		tm:        tm,
		chatID:    chatID,
		command:   want,
		startedAt: now,
		sessionID: fmt.Sprintf("%s%d", pendingCommandIDPrefix, tm.nextPendingCommandSeq),
	}
	tm.pendingCommands = append(tm.pendingCommands, promise)
	if len(tm.pendingCommands) > maxPendingCommandReservations {
		// A reservation is held only for one start. A leak here is a bug,
		// not a state to honour: drop the oldest rather than block a
		// command forever.
		tm.pendingCommands = tm.pendingCommands[len(tm.pendingCommands)-maxPendingCommandReservations:]
	}
	tm.mutex.Unlock()
	return promise, "", 0, false
}

// publish records the session the reserved start created, so a concurrent
// identical start refused while it is in flight names a real session instead
// of the reservation's placeholder id. Takes the manager lock because the
// guard reads it under that lock.
func (r *bgReservation) publish(sessionID string) {
	if r == nil || sessionID == "" {
		return
	}
	r.tm.mutex.Lock()
	defer r.tm.mutex.Unlock()
	r.sessionID = sessionID
}

// release drops the reservation. It is safe to call more than once, and a
// no-op on nil.
func (r *bgReservation) release() {
	if r == nil {
		return
	}
	r.tm.mutex.Lock()
	defer r.tm.mutex.Unlock()
	// By identity: two reservations can share a timestamp, and a value
	// match would let one caller's release drop another's.
	for i, pending := range r.tm.pendingCommands {
		if pending == r {
			r.tm.pendingCommands = append(r.tm.pendingCommands[:i], r.tm.pendingCommands[i+1:]...)
			return
		}
	}
}

// findRunningBackgroundLocked reports a session of chatID whose background
// command is want and whose command has not finished. "Running" is the
// session's command sentinel: an unclosed bgDone means the command is still
// in flight, whether it was started in the background or promoted from the
// foreground. A finished or stopped session does not match.
//
// Caller holds tm.mutex.
func (tm *TerminalManager) findRunningBackgroundLocked(chatID, want string, now time.Time) (string, time.Duration, bool) {
	for _, session := range tm.sessions {
		session.mutex.RLock()
		isBackground := session.IsBackground
		active := session.Active
		sessionChat := session.bgChatID
		command := session.bgCommand
		done := session.bgDone
		startedAt := session.StartedAt
		id := session.ID
		session.mutex.RUnlock()

		if !isBackground || !active || sessionChat != chatID || done == nil {
			continue
		}
		if normalizeBackgroundCommand(command) != want {
			continue
		}
		select {
		case <-done:
			continue // command finished; the session is only awaiting cleanup
		default:
		}
		return id, now.Sub(startedAt), true
	}
	for _, r := range tm.pendingCommands {
		if r.chatID == chatID && r.command == want {
			return r.sessionID, now.Sub(r.startedAt), true
		}
	}
	return "", 0, false
}

// maxPendingCommandReservations bounds the reservation list; see
// reserveDuplicateCommand.
const maxPendingCommandReservations = 64

// pendingCommandIDPrefix names a reservation that has no session yet, so a
// refusal during a start in flight still names the command it collided with.
// The session's own id replaces it once the session exists. It is
// agent_tools' prefix, so the refusal message knows the id is not yet
// addressable and does not invite check_background or stop_background on it.
const pendingCommandIDPrefix = agenttools.PendingSessionIDPrefix

// bgReservation is a command this manager has admitted but not yet
// registered as a session. Compared by pointer identity, never by value.
type bgReservation struct {
	tm        *TerminalManager
	chatID    string
	command   string
	startedAt time.Time

	// sessionID is the session the reserved start created, published as
	// soon as the session exists so a concurrent refusal can name it
	// instead of pointing at nothing.
	sessionID string
}
