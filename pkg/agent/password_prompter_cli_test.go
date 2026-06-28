package agent

import (
	"context"
	"errors"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
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
