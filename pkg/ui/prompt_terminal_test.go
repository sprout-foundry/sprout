package ui

import (
	"context"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// /rollback's prompts take the terminal like every other question: streamed
// prose is held off them, and a second question waits its turn.
func TestTakeTerminalForPrompt_HoldsStreamingAndTheQuestionSlot(t *testing.T) {
	release := takeTerminalForPrompt()
	if !clihooks.IsStreamingSuspended() {
		t.Fatal("streamed prose would draw over the prompt")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		r, err := console.AcquireTerminalQuestion(ctx)
		if err == nil {
			r()
		}
		done <- err
	}()
	if err := <-done; err == nil {
		t.Fatal("another question took the terminal while the prompt was open")
	}

	release()
	if clihooks.IsStreamingSuspended() {
		t.Fatal("streaming stays suspended after the prompt")
	}
	r, err := console.AcquireTerminalQuestion(context.Background())
	if err != nil {
		t.Fatalf("the slot was not released: %v", err)
	}
	r()
}
