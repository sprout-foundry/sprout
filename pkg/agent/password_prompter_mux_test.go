package agent

import (
	"context"
	"errors"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

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