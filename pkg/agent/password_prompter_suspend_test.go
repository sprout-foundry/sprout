package agent

// Regression test for the CLIPasswordPrompter spinner-coordination pattern.
//
// Background — see the package-level comment in edit_approval_suspend_test.go
// for the full rationale. In short: the CLI has a multi-line activity
// indicator (spinner) that runs in raw mode during a turn. Interactive
// prompts must suspend it before reading stdin or rendering to stderr, or
// the spinner overwrites the prompt text. The canonical pattern is:
//
//	clihooks.SuspendIndicator()
//	clihooks.PauseSteer()
//	clihooks.SuspendStreaming()
//	defer clihooks.ResumeIndicator()
//	defer clihooks.ResumeSteer()
//	defer clihooks.ResumeStreaming()
//
// CLIPasswordPrompter.Prompt in pkg/agent/password_prompter_cli.go does
// exactly this dance. The regression this test guards against is the
// Suspend calls being dropped from Prompt.
//
// ---------------------------------------------------------------------------
// WHY THIS TEST IS NOT A LIVE-DRIVE TEST
// ---------------------------------------------------------------------------
//
// Prompt has an early return at the top:
//
//	if !term.IsTerminal(int(os.Stdin.Fd())) {
//	    return "", tools.ErrNoInteractiveSurface
//	}
//
// `go test` always runs with stdin connected to a pipe, never a real TTY.
// term.IsTerminal reads from the kernel via ioctl(2); we cannot patch it
// from a unit test without either:
//
//   - Refactoring Prompt to take an "isTerminal bool" parameter (out of
//     scope — production code is final).
//   - Mocking the unexported term.IsTerminal function (impossible — it
//     lives in golang.org/x/term and is not a variable in our codebase).
//   - Running the test under a pty via something like creack/pty (large
//     dependency for a unit-test regression guard; also fragile).
//
// Per the task instructions, when bypassing TTY is impossible we document
// the limitation and use an alternative assertion strategy. We do two
// things here:
//
//  1. A source-presence test that reads password_prompter_cli.go and
//     asserts every Suspend / Resume call is still present. This is the
//     actual regression catcher: if a future change removes any of the
//     six required calls (Suspend×3, Resume×3), the string-match
//     assertion fails. The strings used as assertions are the exact
//     function-call syntax from the production source so a rename of
//     e.g. clihooks.SuspendIndicator → clihooks.StopSpinner would also
//     be caught (which is itself useful — the hooks API is the
//     documented contract).
//
//  2. A live-drive test that verifies the existing TTY early-return path
//     still returns ErrNoInteractiveSurface and does NOT call the hooks.
//     A regression that *removes* the early-return guard (e.g. by
//     mistake) would now reach the hook-call section under non-TTY
//     conditions, but with a closed-stdin input the function would
//     still not hang — so this test mostly documents the negative path
//     and ensures the documented "non-interactive" sentinel error keeps
//     flowing through. It is a smoke test, not the regression catcher.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// requiredCLIPasswordPrompterCalls is the canonical spinner-coordination
// call sequence that Prompt must perform when stdin is a TTY. The
// source-presence test below verifies each of these strings appears in
// the production source file. The ordering reflects the contract
// described in the production doc comment ("All three Suspend calls
// fire first, then the deferred Resumes run on return") and is the
// contract other tools (pkg/agent_tools/ask_user.go, pkg/utils/logger.go,
// pkg/agent_tools/shell_native_password.go, etc.) follow.
var requiredCLIPasswordPrompterCalls = []string{
	// Three Suspend calls fire in this order, all BEFORE the prompt text
	// is rendered to stderr.
	"clihooks.SuspendIndicator()",
	"clihooks.PauseSteer()",
	"clihooks.SuspendStreaming()",
	// Three deferred Resume calls restore the hooks on return. These
	// must use defer so they fire even if ReadPassword errors out.
	"defer clihooks.ResumeIndicator()",
	"defer clihooks.ResumeSteer()",
	"defer clihooks.ResumeStreaming()",
}

// TestCLIPasswordPrompter_SuspendCallsPresentInSource is the primary
// regression guard for the password prompter. It reads the production
// source file and asserts that every required clihooks call is still
// present. If a future change removes any of the six required calls,
// this test fails loudly with a clear message linking the missing call
// to the regression class.
//
// Note: this is a string-presence check, not a behavioral one. It will
// catch a deleted call but NOT a call that is wrapped behind a flag
// like `if debug { clihooks.SuspendIndicator() }`. That's an
// acceptable trade-off — the documented contract is unconditional
// (the production doc comment says "All three hooks no-op when no
// implementation is registered") — and the live test below covers
// the actual runtime behavior under non-TTY conditions.
func TestCLIPasswordPrompter_SuspendCallsPresentInSource(t *testing.T) {
	const srcPath = "password_prompter_cli.go"

	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("could not read %s: %v (run from pkg/agent/)", srcPath, err)
	}
	src := string(raw)

	for _, want := range requiredCLIPasswordPrompterCalls {
		if !strings.Contains(src, want) {
			t.Errorf("regression: %s is missing the call %q — the spinner would clobber the password prompt",
				srcPath, want)
		}
	}
}

// TestCLIPasswordPrompter_NonTTYReturnsEarlyWithoutHooks documents and
// pins the documented non-TTY behaviour: under `go test` stdin is a
// pipe, Prompt returns ErrNoInteractiveSurface immediately, and none of
// the spinner-coordination hooks fire. This is the inverse of the
// regression we're guarding against — if the early-return guard is
// ever removed, the hooks would now run before the (still-broken)
// password read, which could surface as the spinner clobbering the
// (non-existent) prompt. The test catches that case by failing when
// the hooks DO fire under non-TTY conditions.
//
// Note: this test installs recorder hooks on the global clihooks
// registry. It MUST register a cleanup to uninstall them, otherwise
// subsequent tests in the same package will see phantom hook
// invocations. The cleanup is wired via t.Cleanup so it runs even on
// subtest failure.
func TestCLIPasswordPrompter_NonTTYReturnsEarlyWithoutHooks(t *testing.T) {
	cli := NewCLIPasswordPrompter()

	recorder := newHookRecorder()
	cleanup := installHooks(t, recorder)
	defer cleanup()

	// Sanity: under `go test` stdin is a pipe → term.IsTerminal is false
	// → Prompt must return ErrNoInteractiveSurface. The existing
	// TestCLIPasswordPrompter_NoTTY in password_prompter_cli_test.go
	// already pins this assertion; we re-pin it here alongside the
	// hooks assertion so a regression in either direction is caught by
	// the same test.
	_, err := cli.Prompt(context.Background(), "test reason")
	if !errors.Is(err, tools.ErrNoInteractiveSurface) {
		t.Fatalf("expected ErrNoInteractiveSurface under non-TTY stdin, got: %v", err)
	}

	suspend, resume, pause, resumeSteer, sawStreaming := recorder.snapshot()
	if suspend != 0 || resume != 0 || pause != 0 || resumeSteer != 0 {
		t.Errorf("regression: hooks fired on non-TTY stdin (suspend=%d, resume=%d, "+
			"pauseSteer=%d, resumeSteer=%d) — the TTY early-return guard was bypassed",
			suspend, resume, pause, resumeSteer)
	}
	if sawStreaming {
		t.Error("regression: streaming suspension flag was set on non-TTY stdin — " +
			"the TTY early-return guard was bypassed")
	}
}

// TestCLIPasswordPrompter_ContextCancelledBeforeTTYCheck pins the
// short-circuit order: context cancellation is checked before the TTY
// guard. This is documented behaviour in the production code:
//
//	if ctx.Err() != nil {
//	    return "", ctx.Err()
//	}
//	if !term.IsTerminal(int(os.Stdin.Fd())) {
//	    return "", tools.ErrNoInteractiveSurface
//	}
//
// A regression that re-orders these (or removes the ctx check entirely)
// would change the error returned to the caller. We assert the existing
// contract here so a future refactor is forced to think about it.
func TestCLIPasswordPrompter_ContextCancelledBeforeTTYCheck(t *testing.T) {
	cli := NewCLIPasswordPrompter()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := cli.Prompt(ctx, "test reason")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled when ctx is pre-cancelled, got: %v", err)
	}
}
