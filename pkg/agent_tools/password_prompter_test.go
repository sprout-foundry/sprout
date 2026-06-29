package tools

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// fakePrompter is a minimal PasswordPrompter for testing context plumbing.
type fakePrompter struct {
	fixed string
}

func (f *fakePrompter) Prompt(_ context.Context, _ string) (string, error) {
	return f.fixed, nil
}

func TestPasswordPrompterFromContext_Nil(t *testing.T) {
	ctx := context.Background()
	got := PasswordPrompterFromContext(ctx)
	if got != nil {
		t.Fatalf("expected nil when no prompter in context, got %T", got)
	}
}

func TestPasswordPrompterFromContext_Set(t *testing.T) {
	want := &fakePrompter{fixed: "secret123"}
	ctx := WithPasswordPrompter(context.Background(), want)
	got := PasswordPrompterFromContext(ctx)
	if got != want {
		t.Fatalf("expected round-trip to return same prompter, got %v", got)
	}

	// Verify the prompter actually works.
	pwd, err := got.Prompt(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pwd != "secret123" {
		t.Fatalf("expected password 'secret123', got %q", pwd)
	}
}

func TestPasswordPrompterFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), passwordPrompterKey{}, "not a prompter")
	got := PasswordPrompterFromContext(ctx)
	if got != nil {
		t.Fatalf("expected nil when context value is wrong type, got %T", got)
	}
}

func TestWithPasswordPrompter_Nil(t *testing.T) {
	// Passing nil should not panic.
	ctx := WithPasswordPrompter(context.Background(), nil)
	got := PasswordPrompterFromContext(ctx)
	// nil stored as interface{} does not assert to PasswordPrompter, so
	// FromContext returns nil.
	if got != nil {
		t.Fatalf("expected nil when nil prompter was stored, got %T", got)
	}
}

func TestErrNoInteractiveSurface_Is(t *testing.T) {
	// errors.Is must match the sentinel against itself and against a
	// wrapped variant (so callers can use fmt.Errorf("...: %w", err)).
	if !errors.Is(ErrNoInteractiveSurface, ErrNoInteractiveSurface) {
		t.Error("expected errors.Is(sentinel, sentinel) == true")
	}
	wrapped := fmt.Errorf("wrap: %w", ErrNoInteractiveSurface)
	if !errors.Is(wrapped, ErrNoInteractiveSurface) {
		t.Error("expected errors.Is(wrapped, sentinel) == true")
	}
}
