package tools

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// Fakes for startWakeupWatcher tests
// =============================================================================

// fakeWakeupTerminal implements TerminalAccess with a set of sessions that can
// be flipped from active to inactive on demand. IsSessionActive and
// BackgroundDoneChan carry state; the other methods are no-ops because
// startWakeupWatcher never calls them.
type fakeWakeupTerminal struct {
	mu      sync.Mutex
	active  map[string]bool
	session string
	done    map[string]chan struct{}
	code    map[string]int
}

func newFakeWakeupTerminal(sessionID string) *fakeWakeupTerminal {
	return &fakeWakeupTerminal{
		active:  map[string]bool{sessionID: true},
		session: sessionID,
		done:    map[string]chan struct{}{sessionID: make(chan struct{})},
		code:    map[string]int{sessionID: 0},
	}
}

// markInactive flips the tracked session to inactive and closes its done
// channel, simulating both the PTY death and the command-completion signal.
func (f *fakeWakeupTerminal) markInactive() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active[f.session] = false
	if ch, ok := f.done[f.session]; ok {
		close(ch)
	}
}

func (f *fakeWakeupTerminal) IsSessionActive(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active[sessionID]
}

func (f *fakeWakeupTerminal) BackgroundDoneChan(sessionID string) (<-chan struct{}, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch, ok := f.done[sessionID]
	if !ok {
		return nil, false
	}
	return ch, true
}

func (f *fakeWakeupTerminal) BackgroundExitCode(sessionID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.code[sessionID]
}

func (f *fakeWakeupTerminal) ExecuteCommandInHidden(_ context.Context, _ string, _ string) (string, int, error) {
	return "", 0, fmt.Errorf("not implemented in fake")
}

func (f *fakeWakeupTerminal) GetOrCreateHiddenSessionForChat(_ context.Context, _ string) (string, error) {
	return f.session, nil
}

func (f *fakeWakeupTerminal) ExecuteCommandInBackground(_ context.Context, _, _ string) (string, error) {
	return f.session, nil
}

func (f *fakeWakeupTerminal) GetBackgroundOutput(_ string) (string, error) {
	return "", nil
}

func (f *fakeWakeupTerminal) StopBackgroundSession(_ string) error {
	return nil
}

// wakeupNotification is one recorded NotifyCompletion call. seq is a
// monotonically increasing counter so tests can assert notification ordering.
type wakeupNotification struct {
	kind    string
	content string
	seq     int64
}

// fakeWakeupNotifier implements BackgroundNotifier and records every call in
// arrival order.
type fakeWakeupNotifier struct {
	mu    sync.Mutex
	calls []wakeupNotification
	seq   int64
}

func (n *fakeWakeupNotifier) NotifyCompletion(_ string, kind, content string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.seq++
	n.calls = append(n.calls, wakeupNotification{kind: kind, content: content, seq: n.seq})
}

func (n *fakeWakeupNotifier) of(kind string) []wakeupNotification {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]wakeupNotification, 0, len(n.calls))
	for _, c := range n.calls {
		if c.kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func (n *fakeWakeupNotifier) all() []wakeupNotification {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]wakeupNotification(nil), n.calls...)
}

// startWakeupWatcherForTest wires up the fake terminal and notifier and starts
// the watcher under test. The returned cancelFunc tears the watch down.
func startWakeupWatcherForTest(t *testing.T, sessionID string, timeoutSec int) (*fakeWakeupTerminal, *fakeWakeupNotifier, context.CancelFunc) {
	t.Helper()
	watchCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel) // guarantee the poller is torn down even if an assertion fails
	tm := newFakeWakeupTerminal(sessionID)
	notifier := &fakeWakeupNotifier{}
	env := ToolEnv{Notifier: notifier, LifetimeCtx: watchCtx}
	resultJSON := fmt.Sprintf(`{"session_id":%q,"status":"running"}`, sessionID)
	new(shellCommandHandler).startWakeupWatcher(WithTerminalManager(watchCtx, tm), env, resultJSON, timeoutSec)
	return tm, notifier, cancel
}

// waitForKind polls the notifier until at least count notifications of the
// given kind have arrived, returning whether the count was reached.
func waitForKind(t *testing.T, n *fakeWakeupNotifier, kind string, count int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(n.of(kind)) >= count {
			return true
		}
		time.Sleep(15 * time.Millisecond)
	}
	return len(n.of(kind)) >= count
}

// requireNoMoreNotifications gives stragglers a grace window to arrive and
// then asserts the total recorded call count has not grown.
func requireNoMoreNotifications(t *testing.T, n *fakeWakeupNotifier, grace time.Duration) {
	t.Helper()
	before := len(n.all())
	time.Sleep(grace)
	require.Len(t, n.all(), before, "unexpected extra notification(s) after grace period: %+v", n.all())
}

// =============================================================================
// Tests
// =============================================================================

// Session completes shortly after the watcher starts, no wakeup_timeout set:
// exactly one shell_bg notification with exit code 0 (TerminalManager never
// reports a real exit code) and no shell_bg_timeout.
func TestWakeupWatcher_TerminalCompletion(t *testing.T) {
	const sessionID = "bg-test-1"
	tm, notifier, cancel := startWakeupWatcherForTest(t, sessionID, 0)
	defer cancel()

	// Session exits ~200ms in; the poller notices within one 500ms tick.
	go func() {
		time.Sleep(200 * time.Millisecond)
		tm.markInactive()
	}()

	require.True(t, waitForKind(t, notifier, "shell_bg", 1, 3*time.Second),
		"expected a shell_bg completion notification")
	requireNoMoreNotifications(t, notifier, 400*time.Millisecond)

	completions := notifier.of("shell_bg")
	require.Len(t, completions, 1)
	require.Equal(t,
		fmt.Sprintf("Background session %s completed with exit code %d.\nUse shell_command(check_background=%q) to see full output.", sessionID, 0, sessionID),
		completions[0].content)
	require.Empty(t, notifier.of("shell_bg_timeout"))
}

// The wakeup deadline is a heads-up, not a give-up: with timeoutSec=1 and a
// session that runs ~3s, the watcher must send one shell_bg_timeout at ~1s,
// keep watching, and then send exactly one shell_bg at ~3s — in that order.
func TestWakeupWatcher_TerminalDeadlineThenCompletion(t *testing.T) {
	const sessionID = "bg-test-2"
	tm, notifier, cancel := startWakeupWatcherForTest(t, sessionID, 1)
	defer cancel()

	go func() {
		time.Sleep(3 * time.Second)
		tm.markInactive()
	}()

	// Deadline heads-up fires at ~1s (allow scheduling slack).
	require.True(t, waitForKind(t, notifier, "shell_bg_timeout", 1, 3*time.Second),
		"expected a shell_bg_timeout heads-up notification")
	require.Empty(t, notifier.of("shell_bg"),
		"deadline must not stop the watch — no completion before the session exits")

	// Completion arrives at ~3s (poller tick ≤500ms later). Budget is
	// generous because the wait clock starts when the heads-up was
	// observed (~1s in), and tickers drift under CI load.
	require.True(t, waitForKind(t, notifier, "shell_bg", 1, 5*time.Second),
		"expected a shell_bg completion notification after the deadline heads-up")
	requireNoMoreNotifications(t, notifier, 400*time.Millisecond)

	timeouts := notifier.of("shell_bg_timeout")
	require.Len(t, timeouts, 1, "exactly one heads-up expected")
	require.Equal(t,
		fmt.Sprintf("Background session %s still running after %ds (wakeup deadline reached).\nIt will be notified again when it completes.", sessionID, 1),
		timeouts[0].content)

	completions := notifier.of("shell_bg")
	require.Len(t, completions, 1, "exactly one completion expected")
	require.Equal(t,
		fmt.Sprintf("Background session %s completed with exit code %d.\nUse shell_command(check_background=%q) to see full output.", sessionID, 0, sessionID),
		completions[0].content)

	// Ordering: the heads-up was recorded strictly before the completion.
	require.Less(t, timeouts[0].seq, completions[0].seq,
		"shell_bg_timeout must be notified before shell_bg")
}

// Deadline fires, the session keeps running, and the agent shuts down before
// completion: exactly one shell_bg_timeout and no completion notification.
func TestWakeupWatcher_TerminalDeadlineStillRunning(t *testing.T) {
	const sessionID = "bg-test-3"
	_, notifier, cancel := startWakeupWatcherForTest(t, sessionID, 1)

	require.True(t, waitForKind(t, notifier, "shell_bg_timeout", 1, 3*time.Second),
		"expected a shell_bg_timeout heads-up notification")

	// Session still running; agent shuts down at ~2.5s.
	time.Sleep(1500 * time.Millisecond)
	cancel()

	requireNoMoreNotifications(t, notifier, 400*time.Millisecond)
	require.Len(t, notifier.of("shell_bg_timeout"), 1)
	require.Empty(t, notifier.of("shell_bg"),
		"cancelled watch must not emit a completion notification")
}

// Cancelling watchCtx while the session is still running (no deadline set)
// produces zero notifications of any kind.
func TestWakeupWatcher_WatchCtxCancelBeforeCompletion(t *testing.T) {
	const sessionID = "bg-test-4"
	_, notifier, cancel := startWakeupWatcherForTest(t, sessionID, 0)

	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	// Nothing is ever expected, so give the watcher ample time to misbehave.
	time.Sleep(500 * time.Millisecond)
	require.Empty(t, notifier.all(), "cancelled watch must stay silent: %+v", notifier.all())
	// Session never completes; make sure nothing fires late either.
	requireNoMoreNotifications(t, notifier, 250*time.Millisecond)
}

// The session exits BEFORE the wakeup deadline: the deadline goroutine's
// <-done branch must win and no shell_bg_timeout heads-up may be sent.
func TestWakeupWatcher_CompletionBeforeDeadline(t *testing.T) {
	const sessionID = "bg-test-5"
	tm, notifier, _ := startWakeupWatcherForTest(t, sessionID, 5)

	go func() {
		time.Sleep(200 * time.Millisecond)
		tm.markInactive()
	}()

	require.True(t, waitForKind(t, notifier, "shell_bg", 1, 3*time.Second),
		"expected a shell_bg completion notification")
	requireNoMoreNotifications(t, notifier, 400*time.Millisecond)
	require.Empty(t, notifier.of("shell_bg_timeout"),
		"completion before the deadline must not send a heads-up")
}

// BPM path (CLI mode) — the path that previously ignored wakeup_timeout
// entirely. A 2s sleep with a 1s deadline must produce one shell_bg_timeout
// at ~1s, then one shell_bg (exit code 0) at ~2s, in that order.
func TestWakeupWatcher_BPMDeadlineThenCompletion(t *testing.T) {
	bpm := NewBackgroundProcessManager()
	t.Cleanup(bpm.Close)
	watchCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sessionID, err := bpm.Start(watchCtx, "sleep 2", "")
	if err != nil {
		t.Skipf("background processes not supported in this build: %v", err)
	}

	notifier := &fakeWakeupNotifier{}
	env := ToolEnv{Notifier: notifier, LifetimeCtx: watchCtx}
	resultJSON := fmt.Sprintf(`{"session_id":%q,"status":"running"}`, sessionID)
	new(shellCommandHandler).startWakeupWatcher(WithBackgroundProcessManager(watchCtx, bpm), env, resultJSON, 1)

	// Deadline heads-up fires at ~1s while the sleep is still running.
	require.True(t, waitForKind(t, notifier, "shell_bg_timeout", 1, 3*time.Second),
		"BPM path must send a heads-up at the wakeup deadline")

	// Completion arrives at ~2s.
	require.True(t, waitForKind(t, notifier, "shell_bg", 1, 5*time.Second),
		"BPM path must send a completion notification after the deadline")
	requireNoMoreNotifications(t, notifier, 300*time.Millisecond)

	require.Len(t, notifier.of("shell_bg_timeout"), 1, "exactly one heads-up expected")
	completions := notifier.of("shell_bg")
	require.Len(t, completions, 1, "exactly one completion expected")
	require.Contains(t, completions[0].content, "completed with exit code 0",
		"BPM completion must carry the real exit code, got: %q", completions[0].content)
	require.Less(t, notifier.of("shell_bg_timeout")[0].seq, completions[0].seq,
		"shell_bg_timeout must be notified before shell_bg")
}
