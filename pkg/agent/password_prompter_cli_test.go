package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIPasswordPrompter_NoTTY(t *testing.T) {
	cli := NewCLIPasswordPrompter()

	// Under go test, stdin is always a pipe (not a TTY), so Prompt
	// should return ErrNoInteractiveSurface.
	_, err := cli.Prompt(context.Background(), "test reason")
	if !errors.Is(err, tools.ErrNoInteractiveSurface) {
		t.Fatalf("expected ErrNoInteractiveSurface, got: %v", err)
	}
}

func TestErrNoInteractiveSurface_IsExported(t *testing.T) {
	// Verify the sentinel error is exported and usable from pkg/agent.
	err := tools.ErrNoInteractiveSurface
	if err == nil {
		t.Fatal("ErrNoInteractiveSurface should not be nil")
	}
	if !errors.Is(err, tools.ErrNoInteractiveSurface) {
		t.Error("errors.Is should match the exported sentinel")
	}
}

func TestPasswordPrompterBroker_RegisterRespond(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	ch := b.register("req-1")
	if ch == nil {
		t.Fatal("register returned nil channel")
	}

	if !b.respond("req-1", "hunter2") {
		t.Fatal("respond should succeed for registered request")
	}

	got, ok := <-ch
	if !ok {
		t.Fatal("channel was closed")
	}
	if got != "hunter2" {
		t.Fatalf("expected password 'hunter2', got %q", got)
	}
}

func TestPasswordPrompterBroker_RespondUnknownID(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	if b.respond("nonexistent", "password") {
		t.Fatal("respond should return false for unknown request ID")
	}
}

func TestPasswordPrompterBroker_Cleanup(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	b.register("req-1")
	if len(b.pending) != 1 {
		t.Fatalf("expected 1 pending entry after register, got %d", len(b.pending))
	}

	b.cleanup("req-1")
	if len(b.pending) != 0 {
		t.Fatalf("expected 0 pending entries after cleanup, got %d", len(b.pending))
	}
}

func TestPasswordPrompterBroker_DoubleRespond(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	ch := b.register("req-1")

	// First respond should succeed.
	if !b.respond("req-1", "first") {
		t.Fatal("first respond should succeed")
	}

	// Second respond should fail (channel is full with size-1 buffer).
	if b.respond("req-1", "second") {
		t.Fatal("second respond should return false (channel full)")
	}

	// Drain the channel to verify it only has the first password.
	got, ok := <-ch
	if !ok {
		t.Fatal("channel was closed")
	}
	if got != "first" {
		t.Fatalf("expected 'first', got %q", got)
	}
}

func TestPasswordPrompterBroker_ConcurrentSafety(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-req-%d", i)
			ch := b.register(id)
			ok := b.respond(id, "password")
			if !ok {
				t.Errorf("respond failed for goroutine %d", i)
			}
			// Drain channel.
			<-ch
			b.cleanup(id)
		}(i)
	}

	wg.Wait()
}

type fakeCascadePrompter struct {
	password string
	err      error
	called   int
}

func (f *fakeCascadePrompter) Prompt(_ context.Context, _ string) (string, error) {
	f.called++
	return f.password, f.err
}

func TestNewCascadingPasswordPrompter_FirstSuccess(t *testing.T) {
	first := &fakeCascadePrompter{password: "from-first"}
	second := &fakeCascadePrompter{password: "from-second"}

	c := NewCascadingPasswordPrompter(first, second)
	got, err := c.Prompt(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from-first" {
		t.Errorf("expected first prompter's password, got %q", got)
	}
	if second.called != 0 {
		t.Errorf("second prompter should not be called when first succeeds, got called=%d", second.called)
	}
}

func TestNewCascadingPasswordPrompter_FallsBackOnNoSurface(t *testing.T) {
	first := &fakeCascadePrompter{err: tools.ErrNoInteractiveSurface}
	second := &fakeCascadePrompter{password: "fallback-pw"}

	c := NewCascadingPasswordPrompter(first, second)
	got, err := c.Prompt(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if got != "fallback-pw" {
		t.Errorf("expected fallback password, got %q", got)
	}
	if first.called != 1 || second.called != 1 {
		t.Errorf("both prompters should have been called once, got first=%d second=%d", first.called, second.called)
	}
}

func TestNewCascadingPasswordPrompter_AllFail(t *testing.T) {
	first := &fakeCascadePrompter{err: tools.ErrNoInteractiveSurface}
	second := &fakeCascadePrompter{err: tools.ErrNoInteractiveSurface}

	c := NewCascadingPasswordPrompter(first, second)
	_, err := c.Prompt(context.Background(), "test")
	if !errors.Is(err, tools.ErrNoInteractiveSurface) {
		t.Errorf("expected ErrNoInteractiveSurface, got: %v", err)
	}
}

func TestNewCascadingPasswordPrompter_FatalErrorShortCircuits(t *testing.T) {
	// A non-NoInteractiveSurface error (e.g. context timeout, channel
	// closed) must NOT silently fall through to the next prompter —
	// that would let a stale request block on the wrong UI.
	timeoutErr := &timeoutError{}
	first := &fakeCascadePrompter{err: timeoutErr}
	second := &fakeCascadePrompter{password: "should-not-be-called"}

	c := NewCascadingPasswordPrompter(first, second)
	_, err := c.Prompt(context.Background(), "test")
	if !errors.Is(err, timeoutErr) {
		t.Errorf("expected the fatal error to propagate, got: %v", err)
	}
	if second.called != 0 {
		t.Errorf("second prompter should not be called after fatal error, got called=%d", second.called)
	}
}

func TestNewCascadingPasswordPrompter_CancelledContextShortCircuits(t *testing.T) {
	first := &fakeCascadePrompter{password: "never-called"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCascadingPasswordPrompter(first)
	_, err := c.Prompt(ctx, "test")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if first.called != 0 {
		t.Errorf("first prompter should not be called when context is pre-cancelled, got called=%d", first.called)
	}
}

func TestNewCascadingPasswordPrompter_EmptyList(t *testing.T) {
	c := NewCascadingPasswordPrompter()
	_, err := c.Prompt(context.Background(), "test")
	if !errors.Is(err, tools.ErrNoInteractiveSurface) {
		t.Errorf("expected ErrNoInteractiveSurface for empty list, got: %v", err)
	}
}

// timeoutError is a stand-in for any non-NoInteractiveSurface error that
// should abort the cascade (context.DeadlineExceeded, channel closed, etc).
type timeoutError struct{}

func (e *timeoutError) Error() string { return "timeout" }

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

// TestWebUIPasswordPrompter_NoEventBus verifies that Prompt returns
// ErrNoInteractiveSurface when the agent has no event bus.
func TestWebUIPasswordPrompter_NoEventBus(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	wp := NewWebUIPasswordPrompter(agent)
	_, err := wp.Prompt(context.Background(), "test reason")

	if !errors.Is(err, tools.ErrNoInteractiveSurface) {
		t.Errorf("expected ErrNoInteractiveSurface, got: %v", err)
	}
}

// TestWebUIPasswordPrompter_NoWebUIClients verifies that Prompt returns
// ErrNoInteractiveSurface when there's no active WebUI client.
func TestWebUIPasswordPrompter_NoWebUIClients(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	bus := events.NewEventBus()
	agent.SetEventBus(bus)
	// No WebUI clients set — HasActiveWebUIClients returns false.

	wp := NewWebUIPasswordPrompter(agent)
	_, err := wp.Prompt(context.Background(), "test reason")

	if !errors.Is(err, tools.ErrNoInteractiveSurface) {
		t.Errorf("expected ErrNoInteractiveSurface, got: %v", err)
	}
}

// TestWebUIPasswordPrompter_CancelledContext verifies that a pre-cancelled
// context returns immediately without hanging.
func TestWebUIPasswordPrompter_CancelledContext(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	bus := events.NewEventBus()
	agent.SetEventBus(bus)
	agent.SetHasActiveWebUIClients(func() bool { return true })

	wp := NewWebUIPasswordPrompter(agent)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel

	_, err := wp.Prompt(ctx, "test reason")
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// TestWebUIPasswordPrompter_Timeout verifies that Prompt times out when no
// response arrives within the timeout window.
func TestWebUIPasswordPrompter_Timeout(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	bus := events.NewEventBus()
	agent.SetEventBus(bus)
	agent.SetHasActiveWebUIClients(func() bool { return true })

	// Use a short timeout for the test.
	oldTimeout := passwordPromptTimeout
	passwordPromptTimeout = 100 * time.Millisecond
	defer func() { passwordPromptTimeout = oldTimeout }()

	wp := NewWebUIPasswordPrompter(agent)

	start := time.Now()
	_, err := wp.Prompt(context.Background(), "test reason")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error message, got: %v", err)
	}
	// Should have waited at least the timeout duration.
	if elapsed < 80*time.Millisecond {
		t.Errorf("expected to wait ~100ms, but returned in %v", elapsed)
	}
}

// TestWebUIPasswordPrompter_RespondDeliversPassword verifies the full flow:
// Prompt publishes events, blocks on channel, and receives the password when
// RespondToPasswordRequest is called.
func TestWebUIPasswordPrompter_RespondDeliversPassword(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	bus := events.NewEventBus()
	agent.SetEventBus(bus)
	agent.SetHasActiveWebUIClients(func() bool { return true })

	// Subscribe to verify events are published.
	subCh := bus.Subscribe("test-respond-delivers")

	wp := NewWebUIPasswordPrompter(agent)

	// Start Prompt in a goroutine.
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		pwd, err := wp.Prompt(context.Background(), "sudo password")
		if err != nil {
			errCh <- err
		} else {
			resultCh <- pwd
		}
	}()

	// Wait for the password_request event to be published (confirms registration).
	select {
	case ev := <-subCh:
		if ev.Type != events.EventTypePasswordRequest {
			t.Fatalf("expected password_request event, got: %s", ev.Type)
		}
		data, ok := ev.Data.(map[string]interface{})
		if !ok {
			t.Fatal("event data should be map[string]interface{}")
		}
		requestID, _ := data["request_id"].(string)
		if requestID == "" {
			t.Fatal("request_id should not be empty")
		}

		// Now deliver the password.
		delivered := agent.RespondToPasswordRequest(requestID, "secret123")
		if !delivered {
			t.Fatal("expected RespondToPasswordRequest to return true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for password_request event")
	}

	// Verify the password was received.
	select {
	case pwd := <-resultCh:
		if pwd != "secret123" {
			t.Errorf("expected password 'secret123', got: %q", pwd)
		}
	case err := <-errCh:
		t.Errorf("expected password delivery, got error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for password result")
	}
}

// TestRespondToPasswordRequest_UnknownID verifies that calling
// RespondToPasswordRequest with an unknown ID returns false.
func TestRespondToPasswordRequest_UnknownID(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	delivered := agent.RespondToPasswordRequest("nonexistent_id", "password")
	if delivered {
		t.Error("expected false for unknown request ID")
	}
}

// TestPasswordRequestEventPayload verifies the PasswordRequestEvent helper
// produces the expected fields.
func TestPasswordRequestEventPayload(t *testing.T) {
	payload := events.PasswordRequestEvent("pwd_1", "sudo apt update", "[sudo] password for user:")

	if payload["request_id"] != "pwd_1" {
		t.Errorf("request_id = %v, want 'pwd_1'", payload["request_id"])
	}
	if payload["command"] != "sudo apt update" {
		t.Errorf("command = %v, want 'sudo apt update'", payload["command"])
	}
	if payload["prompt"] != "[sudo] password for user:" {
		t.Errorf("prompt = %v, want '[sudo] password for user:'", payload["prompt"])
	}
	if _, ok := payload["timestamp"]; !ok {
		t.Error("payload should have timestamp field")
	}
}

// fakePrompter is a minimal PasswordPrompter for wiring tests. It returns
// a fixed password and records the reason it was called with.
type fakePrompter struct {
	password string
	called   bool
	reason   string
}

func (f *fakePrompter) Prompt(_ context.Context, reason string) (string, error) {
	f.called = true
	f.reason = reason
	return f.password, nil
}

// =============================================================================
// GetPasswordPrompter / SetPasswordPrompter / HasPasswordPrompter
// =============================================================================

func TestHasPasswordPrompter_DefaultFalse(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	assert.False(t, agent.HasPasswordPrompter(), "prompter should be nil by default")
}

func TestSetGetPasswordPrompter_RoundTrip(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	fp := &fakePrompter{password: "secret"}
	agent.SetPasswordPrompter(fp)

	require.True(t, agent.HasPasswordPrompter(), "prompter should be registered")
	got := agent.GetPasswordPrompter()
	assert.Equal(t, fp, got, "GetPasswordPrompter should return the registered prompter")
}

func TestSetPasswordPrompter_Nil(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	agent.SetPasswordPrompter(&fakePrompter{})
	require.True(t, agent.HasPasswordPrompter())

	agent.SetPasswordPrompter(nil)
	assert.False(t, agent.HasPasswordPrompter(), "setting nil should clear the prompter")
}

// =============================================================================
// ResolveToolRisk — classifier gating
// =============================================================================

// TestResolveToolRisk_PrivilegedDowngradedWithPrompter verifies that a sudo
// command is Medium (CAUTION level from classifier) when a password prompter
// is registered. Since sudo is now CAUTION in the classifier, the level is
// already Medium before the prompter downgrade logic — the downgrade only
// fires at High or above, so RiskSourcePasswordPrompter is NOT added.
func TestResolveToolRisk_PrivilegedDowngradedWithPrompter(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	agent.SetPasswordPrompter(&fakePrompter{password: "pw"})

	args := map[string]interface{}{"command": "sudo apt update"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	assert.True(t, assessment.Level.Rank() <= configuration.RiskLevelMedium.Rank(),
		"sudo with prompter should be Medium or lower, got %s", assessment.Level)
	assert.False(t, assessment.IsHardBlock, "sudo with prompter should not be a hard block")
}

// TestResolveToolRisk_PrivilegedNotDowngradedWithoutPrompter verifies that
// a sudo command is CAUTION (Medium) even without a password prompter.
// The classifier now returns CAUTION for sudo, so no prompter is needed
// to avoid a hard block — sudo simply prompts in the default profile.
func TestResolveToolRisk_PrivilegedNotDowngradedWithoutPrompter(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	// No prompter — sudo is now CAUTION (Medium), not blocked.
	assert.False(t, agent.HasPasswordPrompter())

	args := map[string]interface{}{"command": "sudo apt update"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	assert.True(t, assessment.Level.Rank() <= configuration.RiskLevelMedium.Rank(),
		"sudo without prompter should be Medium or lower (CAUTION), got %s", assessment.Level)
	assert.False(t, assessment.IsHardBlock, "sudo without prompter should not be a hard block")
}

// TestResolveToolRisk_DestructiveNotDowngradedWithPrompter is the safety
// guard: even with a prompter, destructive commands (rm -rf) must NOT be
// downgraded. Only RiskCategoryPrivileged is eligible.
func TestResolveToolRisk_DestructiveNotDowngradedWithPrompter(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	agent.SetPasswordPrompter(&fakePrompter{password: "pw"})

	args := map[string]interface{}{"command": "rm -rf /tmp/sprout_test_dir"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	// rm -rf should remain High or Critical even with a prompter.
	assert.True(t, assessment.Level.Rank() >= configuration.RiskLevelHigh.Rank(),
		"rm -rf with prompter should still be High or Critical, got %s", assessment.Level)
}

// =============================================================================
// executeShellCommandWithTruncation — context wiring
// =============================================================================

// TestExecuteShellCommand_PrompterInContext verifies that the prompter is
// placed into the execution context. We test this by checking that
// PasswordPrompterFromContext returns the registered prompter after the
// wiring function runs. Since executeShellCommandWithTruncation runs a real
// command, we instead verify the wiring logic directly: the agent's
// passwordPrompter field, when set, is what WithPasswordPrompter would
// inject. This is a structural test — the actual stdin plumbing lives in
// the shell tool (a follow-up slice).
func TestExecuteShellCommand_PrompterInContext(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	fp := &fakePrompter{password: "ctx-pw"}
	agent.SetPasswordPrompter(fp)

	// Simulate the wiring that executeShellCommandWithTruncation does.
	ctx := context.Background()
	if agent.passwordPrompter != nil {
		ctx = tools.WithPasswordPrompter(ctx, agent.passwordPrompter)
	}

	got := tools.PasswordPrompterFromContext(ctx)
	require.NotNil(t, got, "prompter should be in context after wiring")

	// Verify it's the same prompter and it works.
	pwd, err := got.Prompt(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, "ctx-pw", pwd)
	assert.True(t, fp.called, "the wired prompter should be the fakePrompter instance")
}

// TestExecuteShellCommand_NoPrompterInContextWhenUnset verifies that when
// no prompter is registered, the context does not carry one (nil-safe).
func TestExecuteShellCommand_NoPrompterInContextWhenUnset(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	assert.False(t, agent.HasPasswordPrompter())

	ctx := context.Background()
	if agent.passwordPrompter != nil {
		ctx = tools.WithPasswordPrompter(ctx, agent.passwordPrompter)
	}

	got := tools.PasswordPrompterFromContext(ctx)
	assert.Nil(t, got, "no prompter should be in context when unset")
}
