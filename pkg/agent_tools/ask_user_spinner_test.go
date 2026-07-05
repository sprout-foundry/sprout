package tools

import (
	"context"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestAskUserHandler_NoSelfPublishedEvents verifies the fix for the
// spinner-blocks-input bug. The ask_user handler must NOT self-publish
// ToolStart/ToolEnd events — the core tool executor already publishes
// them with the correct "tool_name" key. The handler's self-publish used
// the wrong key ("tool") and caused IsInteractiveTool("") to return false
// in the CLI terminal subscriber, which started the spinner and blocked
// text entry.
func TestAskUserHandler_NoSelfPublishedEvents(t *testing.T) {
	t.Parallel()

	h := &askUserHandler{}
	bus := events.NewEventBus()
	ch := bus.Subscribe("test-no-events")
	defer bus.Unsubscribe("test-no-events")

	// Set up an AskUserService that returns immediately so Execute
	// completes without blocking on stdin.
	env := ToolEnv{
		EventBus: bus,
		AskUser:  &immediateAskUserService{},
	}

	_, err := h.Execute(context.Background(), env, map[string]any{
		"question": "test question",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Give any would-be published events time to arrive.
	time.Sleep(50 * time.Millisecond)

	// Drain the channel — there should be ZERO events.
	select {
	case ev := <-ch:
		t.Errorf("ask_user handler self-published event %q — should NOT publish; the core executor handles ToolStart/ToolEnd", ev.Type)
	default:
		// Good — no events published.
	}
}

// TestRequestClarificationHandler_NoSelfPublishedEvents — same fix applies
// to request_clarification, the other Interactive tool.
func TestRequestClarificationHandler_NoSelfPublishedEvents(t *testing.T) {
	t.Parallel()

	// Save and restore the func pointer.
	saved := RequestClarificationFunc
	defer func() { RequestClarificationFunc = saved }()
	RequestClarificationFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return "clarified", nil
	}

	h := &requestClarificationHandler{}
	bus := events.NewEventBus()
	ch := bus.Subscribe("test-no-events-rc")
	defer bus.Unsubscribe("test-no-events-rc")

	env := ToolEnv{EventBus: bus}

	_, err := h.Execute(context.Background(), env, map[string]any{
		"question": "test clarification",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	select {
	case ev := <-ch:
		t.Errorf("request_clarification handler self-published event %q — should NOT publish", ev.Type)
	default:
		// Good — no events published.
	}
}

// TestAskUserHandler_StillInteractive ensures the Interactive flag is still
// true so the CLI subscriber's IsInteractiveTool check works.
func TestAskUserHandler_StillInteractive(t *testing.T) {
	t.Parallel()
	h := &askUserHandler{}
	if !h.Interactive() {
		t.Error("ask_user handler must report Interactive() = true")
	}
}

func TestRequestClarificationHandler_StillInteractive(t *testing.T) {
	t.Parallel()
	h := &requestClarificationHandler{}
	if !h.Interactive() {
		t.Error("request_clarification handler must report Interactive() = true")
	}
}

// immediateAskUserService implements AskUserService and returns a fixed
// answer immediately, so Execute completes without blocking on stdin.
type immediateAskUserService struct{}

func (s *immediateAskUserService) Ask(ctx context.Context, req AskUserRequest) (string, error) {
	return "test answer", nil
}
