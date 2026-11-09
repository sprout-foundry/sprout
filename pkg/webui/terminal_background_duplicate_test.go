//go:build !js

package webui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// The WebUI (shared-agent daemon) side of the duplicate-command guard: a
// command already running as a background session in the same chat is not
// started again, the refusal names the session, a finished session does not
// block, and concurrent identical starts admit one.

func requirePTY(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("requires PTY")
	}
}

// isDuplicateRefusal reports whether err is the guard's refusal (not a
// start failure) and returns the session it named.
// isDuplicateRefusal reports whether err is the guard refusing a command
// that is already running or still starting.
func isDuplicateRefusal(err error) bool {
	if err == nil {
		return false
	}
	msg, ok := strings.CutPrefix(err.Error(), agenttools.DuplicateCommandPrefix)
	return ok && strings.Contains(msg, "Not started")
}

func duplicateRefusalSessionID(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	msg, ok := strings.CutPrefix(err.Error(), agenttools.DuplicateCommandPrefix)
	if !ok {
		return "", false
	}
	if !strings.Contains(msg, "Not started") {
		return "", false
	}
	// The message names the session right after "background session ".
	idx := strings.Index(msg, "in background session ")
	if idx < 0 {
		return "", false
	}
	rest := msg[idx+len("in background session "):]
	end := strings.IndexAny(rest, " \n")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func TestExecuteCommandInBackground_RefusesDuplicate(t *testing.T) {
	requirePTY(t)
	tm := newTestTerminalManager(t, t.TempDir())

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "sleep 30")
	if err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	// Same command, different whitespace: refused, and the result names the
	// running session.
	_, err = tm.ExecuteCommandInBackground(context.Background(), "chat-1", "  sleep  30 ")
	gotID, refused := duplicateRefusalSessionID(err)
	if !refused {
		t.Fatalf("expected a duplicate refusal, got %v", err)
	}
	if gotID != sessionID {
		t.Fatalf("refusal named session %q, want %q", gotID, sessionID)
	}

	// A different command in the same chat still starts.
	if _, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "sleep 31"); err != nil {
		t.Fatalf("a different command must start: %v", err)
	}
}

// A finished command does not block the next run of the same command.
func TestExecuteCommandInBackground_StartsAfterFinished(t *testing.T) {
	requirePTY(t)
	tm := newTestTerminalManager(t, t.TempDir())

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "true")
	if err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	done, ok := tm.BackgroundDoneChan(sessionID)
	if !ok {
		t.Fatal("background session should expose a completion channel")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("first command did not finish")
	}

	if _, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "true"); err != nil {
		t.Fatalf("a finished session must not block the command: %v", err)
	}
}

// A foreground command promoted at its tool deadline counts as a running copy
// of the command. Drives the real promotion path: a command outliving its
// context is promoted by ExecuteCommandAndWait, and the guard must then refuse
// an identical background start. The promotion state is never hand-built here
// — a fixture that sets bgDone itself would pass even if the promotion path
// forgot to allocate it, which is the bug this covers.
func TestExecuteCommandInBackground_PromotedSessionCountsAsRunning(t *testing.T) {
	requirePTY(t)
	tm := newTestTerminalManager(t, t.TempDir())

	sessionID, err := tm.GetOrCreateHiddenSessionForChat(context.Background(), "chat-1")
	if err != nil {
		t.Fatalf("hidden session: %v", err)
	}
	t.Cleanup(func() { _ = tm.CloseSession(sessionID) })
	tm.mutex.Lock()
	session := tm.sessions[sessionID]
	tm.mutex.Unlock()
	if session == nil {
		t.Fatalf("hidden session %q is not registered", sessionID)
	}

	// The tool deadline expires while the command is still running, so the
	// executor promotes it rather than killing it.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _, err = tm.ExecuteCommandAndWait(ctx, session, "sleep 30")
	if err == nil {
		t.Fatalf("a command outliving its deadline must be promoted, got no error")
	}
	if !strings.Contains(err.Error(), "COMMAND_PROMOTED_TO_BACKGROUND") {
		t.Fatalf("want a promotion, got %v", err)
	}

	// The promotion left a running background copy the guard can see.
	session.mutex.RLock()
	isBackground, active, command, done := session.IsBackground, session.Active, session.bgCommand, session.bgDone
	session.mutex.RUnlock()
	if !isBackground || !active {
		t.Fatalf("promoted session: IsBackground=%v Active=%v, want both true", isBackground, active)
	}
	if command != "sleep 30" {
		t.Fatalf("promoted session recorded command %q, want %q", command, "sleep 30")
	}
	if done == nil {
		t.Fatalf("the promotion did not allocate bgDone, so the duplicate guard cannot see it")
	}

	_, err = tm.ExecuteCommandInBackground(context.Background(), "chat-1", "sleep 30")
	gotID, refused := duplicateRefusalSessionID(err)
	if !refused {
		t.Fatalf("a promoted session must count as running, got %v", err)
	}
	if gotID != sessionID {
		t.Fatalf("refusal named %q, want the promoted session %q", gotID, sessionID)
	}

	// Once the promoted command is closed, the same command starts.
	if err := tm.CloseSession(sessionID); err != nil {
		t.Fatalf("close promoted session: %v", err)
	}
	if _, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "sleep 30"); err != nil {
		t.Fatalf("after the promoted command ends the command must start: %v", err)
	}
}

// A refusal that lands while a start is still in flight names the reservation,
// never an empty id: the agent is always told what it collided with.
func TestExecuteCommandInBackground_RefusalDuringReservationNamesIt(t *testing.T) {
	tm := newTestTerminalManager(t, t.TempDir())

	promise, firstBusyID, _, busy := tm.reserveDuplicateCommand("chat-1", "sleep 30")
	if busy {
		t.Fatalf("the first reservation must be admitted")
	}
	t.Cleanup(promise.release)
	if firstBusyID != "" {
		t.Fatalf("an admitted reservation reports no busy id, got %q", firstBusyID)
	}
	if promise.sessionID == "" {
		t.Fatalf("a reservation must carry an id before its session exists")
	}

	_, secondID, _, secondBusy := tm.reserveDuplicateCommand("chat-1", "sleep 30")
	if !secondBusy {
		t.Fatalf("an identical reservation must be refused")
	}
	if secondID == "" {
		t.Fatalf("the refusal named an empty id; it must name the in-flight reservation")
	}
	if secondID != promise.sessionID {
		t.Fatalf("refusal named %q, want the reservation %q", secondID, promise.sessionID)
	}
}

// The check and the reservation are one step, so parallel identical calls
// admit one session.
func TestExecuteCommandInBackground_ConcurrentIdenticalStartsAdmitOne(t *testing.T) {
	tm := newTestTerminalManager(t, t.TempDir())

	const callers = 8
	ids := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ids[i], errs[i] = tm.ExecuteCommandInBackground(context.Background(), "chat-1", "sleep 30")
		}(i)
	}
	close(start)
	wg.Wait()

	started, refused := 0, 0
	for i := range ids {
		if errs[i] == nil {
			started++
			continue
		}
		// A refusal names the running session, or says the first copy is
		// still starting when it has no session yet; both are refusals.
		if isDuplicateRefusal(errs[i]) {
			refused++
			continue
		}
		t.Fatalf("call %d failed unexpectedly: %v", i, errs[i])
	}
	if started != 1 || refused != callers-1 {
		t.Fatalf("expected 1 start and %d refusals, got %d starts and %d refusals", callers-1, started, refused)
	}
}
