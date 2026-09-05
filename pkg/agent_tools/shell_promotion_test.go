package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A synchronous shell command promoted past the 2-minute deadline returns a
// promotion message. ParsePromotedBackgroundSession must extract the session
// ID so the shell handler can attach a wakeup watcher to it.
func TestParsePromotedBackgroundSession(t *testing.T) {
	msg := formatBackgroundPromotionMessage("bg-build-ab12", "make all", "compiling...")
	sid, ok := ParsePromotedBackgroundSession(msg)
	require.True(t, ok, "promotion message should parse: %q", msg)
	require.Equal(t, "bg-build-ab12", sid)
}

// Ordinary (non-promoted) results must not parse.
func TestParsePromotedBackgroundSession_PlainOutput(t *testing.T) {
	for _, out := range []string{
		"done\n",
		"Command exceeded nothing. Session bg-foo unrelated sentence.",
		"",
	} {
		_, ok := ParsePromotedBackgroundSession(out)
		require.False(t, ok, "must not parse as promotion: %q", out)
	}
}

// handleSync attaches a wakeup watcher when the result is a promotion
// message: the completion notification must fire when the session ends.
func TestHandleSync_AttachesWatcherOnPromotion(t *testing.T) {
	tm := newFakeWakeupTerminal("bg-promoted-1")

	// Seed a pending "promotion" result by running handleSync through the
	// PTY path: simulate by calling the parser + watcher directly, since
	// forcing a real 2-minute timeout in a unit test is impractical.
	msg := formatBackgroundPromotionMessage("bg-promoted-1", "long-build", "partial output")
	sid, ok := ParsePromotedBackgroundSession(msg)
	require.True(t, ok)

	notifier := &fakeWakeupNotifier{}
	watchCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	env := ToolEnv{
		Notifier:    notifier,
		LifetimeCtx: watchCtx,
	}
	new(shellCommandHandler).startWakeupWatcher(
		WithTerminalManager(watchCtx, tm),
		env,
		fmt.Sprintf(`{"session_id":%q,"status":"running"}`, sid),
		0,
		"long-build",
	)

	// Session completes (sentinel observed — the fake closes done when
	// marked inactive).
	go func() {
		time.Sleep(100 * time.Millisecond)
		tm.markInactive()
	}()

	require.True(t, waitForKind(t, notifier, "shell_bg", 1, 3*time.Second),
		"promotion watcher must notify on completion")

	// The completion notice must carry the sentinel exit code (0 here), not
	// the legacy hardcoded lie.
	require.Contains(t, notifier.of("shell_bg")[0].content,
		fmt.Sprintf("Background session %s completed with exit code 0.", sid),
		"completion notice: %q", notifier.of("shell_bg")[0].content)
	require.False(t, strings.Contains(notifier.of("shell_bg")[0].content, "exit code -1"),
		"sentinel path must not report BgExitNone for a completing command")
}
