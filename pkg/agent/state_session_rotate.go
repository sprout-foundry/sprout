package agent

// state_session_rotate.go — session rotation, forking and ID generation,
// split out of state.go. newSessionID mints a collision-resistant session ID;
// RotateSession closes the current session as a restorable unit and starts a
// fresh one; Breakpoints / ForkAtBreakpoint expose user messages as forkable
// checkpoints.
import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// newSessionID returns a session identifier for a freshly rotated session.
// Format: session_<unix-nano>_<6 random hex bytes> — collision-resistant
// across rapid rotations within the same nanosecond, distinct from
// autoSaveState's session_<unix-seconds> shape so rotated sessions are
// trivially distinguishable from auto-assigned ones.
func newSessionID() string {
	token := make([]byte, 6)
	if _, err := rand.Read(token); err != nil {
		// crypto/rand should not fail on a healthy system; fall back to a
		// timestamp-only ID so we never block rotation on entropy errors.
		return fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("session_%d_%s", time.Now().UnixNano(), hex.EncodeToString(token))
}

// RotateSession closes the current session as a complete, restorable unit
// (writing its final state to disk under the current SessionID), then assigns
// a new SessionID and clears in-memory conversation state. The previous
// session file remains loadable via LoadStateScoped. Returns the new session ID.
//
// If the prior session's SaveStateScoped fails (e.g. invalid session ID or
// unwritable working directory), RotateSession returns that error WITHOUT
// rotating — the prior session must remain intact so the caller can retry.
func (a *Agent) RotateSession() (string, error) {
	if a.state == nil {
		a.state = NewAgentStateManager(false)
	}

	currentID := a.state.GetSessionID()
	if currentID != "" {
		if err := a.SaveStateScoped(currentID, a.currentWorkspaceRoot()); err != nil {
			return "", agenterrors.Wrap(err, "rotate: failed to snapshot prior session")
		}
	}

	// Persist the closing session's tracked changes before resetting the
	// buffer. Without this, the new session's list_changes manifest kept
	// showing (and revert_my_changes kept targeting) the prior session's
	// entries — "/clear" means a fresh session, but the tracker buffer
	// outlived the conversation it belonged to. Best-effort: a commit
	// failure logs but doesn't block rotation (the same tolerance the
	// session-cleanup defer in processQueryWithSeed applies).
	if a.changeTracker != nil && a.changeTracker.IsEnabled() && a.GetChangeCount() > 0 {
		if err := a.CommitChanges("Session rotated via /clear"); err != nil {
			a.Logger().Debug("rotate: commit of pending tracked changes failed (buffer still reset): %v\n", err)
		}
	}

	a.ClearConversationHistory()

	newID := newSessionID()
	a.SetSessionID(newID)

	// Reset the tracker for the new session: fresh revisionID under the
	// new session, empty buffer. Committed entries above were already
	// persisted to the history store, so the timeline tab still shows
	// them; only the live session manifest starts clean. sessionID is
	// written after Reset returns — Reset's commitMu barrier guarantees
	// no in-flight Commit is still reading the old value.
	if a.changeTracker != nil {
		a.changeTracker.Reset("session rotated")
		a.changeTracker.sessionID = newID
	}

	return newID, nil
}

// Breakpoint represents a user message that can be forked from.
type Breakpoint struct {
	Index   int    // 1-based user-facing index
	Content string // First ~80 chars for display
}

// Breakpoints returns all user messages as forkable breakpoints.
func (a *Agent) Breakpoints() []Breakpoint {
	messages := a.state.GetMessages()
	var bps []Breakpoint
	userIdx := 1
	for _, msg := range messages {
		if msg.Role == "user" {
			content := StripUserMessageTimestamp(msg.Content)
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			bps = append(bps, Breakpoint{Index: userIdx, Content: content})
			userIdx++
		}
	}
	return bps
}

// ForkAtBreakpoint saves the current session, then truncates the
// conversation to messages [0..breakpointIndex] (where breakpointIndex
// is 1-based, matching the Breakpoints list). Returns the new session ID.
// The original session is preserved on disk.
func (a *Agent) ForkAtBreakpoint(breakpointIndex int) (string, error) {
	if a.state == nil {
		a.state = NewAgentStateManager(false)
	}

	messages := a.state.GetMessages()
	timestamps := a.state.GetMessageTimestamps()

	// Find the Nth user message (1-based).
	userCount := 0
	cutoffIdx := -1
	for i, msg := range messages {
		if msg.Role == "user" {
			userCount++
			if userCount == breakpointIndex {
				cutoffIdx = i
				break
			}
		}
	}
	if cutoffIdx == -1 {
		if userCount == 0 {
			return "", agenterrors.NewInvalidInputError(fmt.Sprintf("no user messages in conversation (requested breakpoint %d)", breakpointIndex), nil)
		}
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("breakpoint %d out of range (%d user message(s) available)", breakpointIndex, userCount), nil)
	}

	// Save current session to disk before truncating.
	currentID := a.state.GetSessionID()
	if currentID != "" {
		if err := a.SaveStateScoped(currentID, a.currentWorkspaceRoot()); err != nil {
			return "", agenterrors.Wrap(err, "fork: failed to save current session")
		}
	}

	truncated := messages[:cutoffIdx+1]
	truncatedTimestamps := timestamps[:cutoffIdx+1]

	a.ClearConversationHistory()

	for _, msg := range truncated {
		a.state.AddMessage(msg)
	}
	// Restore original timestamps for the truncated messages.
	a.state.SetMessageTimestamps(truncatedTimestamps)

	newID := newSessionID()
	a.SetSessionID(newID)
	return newID, nil
}
