package agent

import (
	"context"
	"testing"
)

func TestHandleAskUser(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}

	t.Run("missing question", func(t *testing.T) {
		t.Parallel()
		_, err := handleAskUser(context.Background(), a, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing question")
		}
	})

	t.Run("non-string question", func(t *testing.T) {
		t.Parallel()
		_, err := handleAskUser(context.Background(), a, map[string]interface{}{
			"question": 123,
		})
		if err == nil {
			t.Error("expected error for non-string question")
		}
	})

	t.Run("nil agent fallback", func(t *testing.T) {
		t.Parallel()
		// When agent is nil, it falls back to tools.AskUser which will block
		// on stdin. We can't test that in unit tests (no stdin), but we can
		// verify the code path exists by checking non-nil agent with no event bus
		// The non-nil path with no event bus calls AskUserWithEventBus which
		// returns an error when there's no event bus to send to.
		// For the nil agent case, just verify the parameter validation runs first.
		_, err := handleAskUser(context.Background(), nil, map[string]interface{}{
			"question": 42, // non-string
		})
		if err == nil {
			t.Error("expected error for non-string question even with nil agent")
		}
	})
}
