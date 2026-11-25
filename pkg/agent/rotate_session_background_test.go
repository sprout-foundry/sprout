package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GH #148: /clear (RotateSession) must end the cleared conversation's
// background work and drop its queued completions — follow-ups about a
// previous session's background processes must not surface in the new
// session.

// TestRotateSessionDropsQueuedNotifications pins the delivery gate: a
// completion that arrived before the rotation is discarded with the
// conversation it belonged to, not prepended to the new session's first
// message or auto-resumed.
func TestRotateSessionDropsQueuedNotifications(t *testing.T) {
	isolateStateAndIndexForTest(t)
	disableWakeupBatching(t)

	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	a.QueueNotification(Notification{
		Content:   "Background session bg-old completed.",
		SessionID: "bg-old",
		Kind:      NotifShellBg,
	})
	require.True(t, a.HasPendingNotifications())

	_, err := a.RotateSession()
	require.NoError(t, err)

	assert.False(t, a.HasPendingNotifications(),
		"queued completions must not survive /clear into the new session")
	assert.Empty(t, a.DrainNotifications())
}

// TestRotateSessionBumpsBackgroundGeneration pins the producer gate: work
// started before the rotation carries the old generation, and its
// completion is stale after the rotation. Work started in the new session
// is unaffected.
func TestRotateSessionBumpsBackgroundGeneration(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	staleGen := a.BackgroundGeneration()
	_, err := a.RotateSession()
	require.NoError(t, err)

	assert.NotEqual(t, staleGen, a.BackgroundGeneration(),
		"rotation must invalidate in-flight completions")
	assert.False(t, a.generationUnchanged(staleGen),
		"a completion captured before rotation must be recognized as stale")
	assert.True(t, a.generationUnchanged(a.BackgroundGeneration()),
		"a completion captured after rotation is live")
}

// TestRotateSessionBetweenDrainAndCaptureDropsResume pins the ordering the
// gate depends on: the generation must be captured BEFORE the notification
// drain, so a rotation landing in that window tags the batch as stale
// (captured after, the batch would carry the NEW generation and post the
// old session's completions into the new session).
func TestRotateSessionBetweenDrainAndCaptureDropsResume(t *testing.T) {
	isolateStateAndIndexForTest(t)
	disableWakeupBatching(t)

	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	// Simulate the interleaving directly: queue a completion, capture what
	// TryAutoResume would capture (pre-drain), then rotate (bump + drain),
	// and check the gate against the pre-drain generation.
	a.QueueNotification(Notification{
		Content:   "Background session bg-old completed.",
		SessionID: "bg-old",
		Kind:      NotifShellBg,
	})
	preDrainGen := a.BackgroundGeneration()

	_, err := a.RotateSession() // bump + drain happen inside
	require.NoError(t, err)

	assert.False(t, a.generationUnchanged(preDrainGen),
		"a batch captured pre-drain must be stale after a rotation that drained it")
	assert.False(t, a.HasPendingNotifications(),
		"the rotation's own drain must have consumed the batch")
}

// TestRotateSessionCancelsBackgroundTasks verifies the runner half of the
// stop: registered background tasks are cancelled and their completions
// suppressed, so a task that was mid-run when the user cleared the chat
// neither keeps the work alive nor wakes the new session.
func TestRotateSessionCancelsBackgroundTasks(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	runner := a.GetSubagentRunner()
	bgCtx, cancel := context.WithCancel(context.Background())
	task := runner.registerBackground("subagent", "coder", "long build", cancel)
	done := make(chan struct{})
	go func() {
		<-bgCtx.Done()
		close(done)
	}()

	_, err := a.RotateSession()
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("rotation did not cancel the running background task")
	}

	// The run goroutine's terminal write: cancelled tasks stay silent.
	a.finishBackground(task, "cancelled", "")

	assert.False(t, a.HasPendingNotifications(),
		"a cancelled task must not queue a completion into the new session")
	assert.Equal(t, "cancelled", task.status)
}

// TestRotateSessionCancelsBackgroundTasks_SuppressesLateCompletion covers
// the race the generation gate exists for: the task finishes on its own
// (not via cancellation) in the rotation window. Its notification is
// stale and must be dropped.
func TestRotateSessionCancelsBackgroundTasks_SuppressesLateCompletion(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	runner := a.GetSubagentRunner()
	_, cancel := context.WithCancel(context.Background())
	task := runner.registerBackground("subagent", "coder", "long build", cancel)

	_, err := a.RotateSession()
	require.NoError(t, err)

	// The task's run goroutine completes after the rotation, unaware.
	a.finishBackground(task, "completed", "build finished")

	assert.False(t, a.HasPendingNotifications(),
		"a completion from a rotated-away session must be dropped")
}

// TestRotateSessionStopsChatBackgroundSessions verifies the WebUI half:
// the chat's background PTY sessions are stopped through the terminal
// manager when its conversation is rotated. The stop runs off the
// rotation's critical path, so the test polls for it (bounded — the
// flaky-test policy forbids a bare sleep).
func TestRotateSessionStopsChatBackgroundSessions(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	var stoppedFor string
	done := make(chan struct{})
	mock := &mockTerminalAccess{
		stopForChatFunc: func(chatID string) []string {
			stoppedFor = chatID
			close(done)
			return []string{"bg-echo-deadbeef"}
		},
	}
	a.SetTerminalManager(mock)
	a.SetEventMetadata(map[string]interface{}{"chat_id": "chat-42"})

	_, err := a.RotateSession()
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("rotation did not stop the chat's background PTY sessions")
	}
	assert.Equal(t, "chat-42", stoppedFor,
		"rotation must stop the chat's background PTY sessions")
}

// TestRotateSessionWithoutChatIDSkipsTerminalStop: a CLI agent has no
// chat binding; the terminal-manager stop must not fire with an empty
// chat ID (the real manager would scan every session).
func TestRotateSessionWithoutChatIDSkipsTerminalStop(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	called := false
	a.SetTerminalManager(&mockTerminalAccess{
		stopForChatFunc: func(chatID string) []string {
			called = true
			return nil
		},
	})

	_, err := a.RotateSession()
	require.NoError(t, err)

	assert.False(t, called, "no chat ID — nothing to stop in the terminal manager")
}

// TestRotateSessionClearsStashedWakeupResume covers the REPL path: a
// wakeup batch stashed for the REPL must not run as a turn in the new
// session. Rotation clears the stash outright; a batch that survived to
// the drain anyway (stashed between the clear and the drain) is dropped
// there by its generation — both paths are exercised.
func TestRotateSessionClearsStashedWakeupResume(t *testing.T) {
	isolateStateAndIndexForTest(t)
	disableWakeupBatching(t)

	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	// A batch stashed in the old session (gen 0) that survives until after
	// the rotation: the stop's stash clear drops it.
	a.wakeupMu.Lock()
	a.pendingWakeupResume = append(a.pendingWakeupResume, wakeupResumeBatch{
		gen: a.BackgroundGeneration(),
		msg: "[wakeup] Background command completed\n\nold session's build",
	})
	a.wakeupMu.Unlock()

	_, err := a.RotateSession()
	require.NoError(t, err)

	assert.Empty(t, a.DrainWakeupForREPL(),
		"stashed wakeup batches belong to the cleared conversation")

	// A batch stashed after the rotation (in the new session's window but
	// carrying the old generation — e.g. a poller that stashed mid-rotation)
	// must be dropped by the drain's generation check, not delivered.
	a.wakeupMu.Lock()
	a.pendingWakeupResume = append(a.pendingWakeupResume, wakeupResumeBatch{
		gen: a.BackgroundGeneration() - 1,
		msg: "[wakeup] Background command completed\n\nstale batch",
	})
	a.wakeupMu.Unlock()

	assert.Empty(t, a.DrainWakeupForREPL(),
		"a batch whose generation moved must be dropped by the drain's check")
}

// TestRotateSessionCancelsInProcessWorkflow covers the in-process automate
// half: a workflow running through RunWorkflowLoopInProcess derives its
// cancellation from the run context registered at launch, so rotation must
// cancel it — otherwise it keeps burning LLM tokens for a conversation
// nobody is reading.
func TestRotateSessionCancelsInProcessWorkflow(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	_, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	cancelled := make(chan struct{})
	wrapped := func() {
		runCancel()
		close(cancelled)
	}
	a.trackInProcessWorkflow("wf-session-1", wrapped)

	_, err := a.RotateSession()
	require.NoError(t, err)

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("rotation did not cancel the in-process workflow's run context")
	}

	// The workflow's own goroutine deregisters on completion; after that a
	// second rotation must not double-cancel a nil entry.
	a.untrackInProcessWorkflow("wf-session-1")
	_, err = a.RotateSession()
	require.NoError(t, err)
}
