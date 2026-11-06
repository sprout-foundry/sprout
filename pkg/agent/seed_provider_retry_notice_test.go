package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func retryableFailure() error {
	return agenterrors.NewRateLimitError("simulated transient failure", nil, "test")
}

// TestPublishRetryEventEmitsVisibleAgentMessage verifies the per-attempt
// visible notice: an agent_message event with category "provider_retry"
// must be published on every failed attempt (including the final one,
// flagged "giving up"), so retry loops are never silent during subagent
// runs.
func TestPublishRetryEventEmitsVisibleAgentMessage(t *testing.T) {
	eventBus := events.NewEventBus()
	subscriberID := "retry-notice-test"
	eventCh := eventBus.Subscribe(subscriberID)
	defer eventBus.Unsubscribe(subscriberID)

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.initSubManagers()
	a.SetEventBus(eventBus)

	retryableErr := retryableFailure()

	a.publishRetryEvent(retryableErr, 0, 3, "test", true)
	a.publishRetryEvent(retryableErr, 3, 3, "test", false)

	var notices []map[string]interface{}
	deadline := time.After(2 * time.Second)
collect:
	for {
		select {
		case ev := <-eventCh:
			if ev.Type != events.EventTypeAgentMessage {
				continue
			}
			data, _ := ev.Data.(map[string]interface{})
			if cat, _ := data["category"].(string); cat == "provider_retry" {
				notices = append(notices, data)
			}
		case <-deadline:
			break collect
		}
	}

	if len(notices) != 2 {
		t.Fatalf("expected 2 provider_retry agent_message events, got %d", len(notices))
	}
	first, _ := notices[0]["message"].(string)
	if !strings.Contains(first, "retrying attempt 1/4") {
		t.Errorf("first notice should say retrying attempt 1/4, got: %q", first)
	}
	second, _ := notices[1]["message"].(string)
	if !strings.Contains(second, "giving up attempt 4/4") {
		t.Errorf("final notice should say giving up attempt 4/4, got: %q", second)
	}
}

// TestDoChatWithRetryStreamingPublishesNotices verifies the retry loop
// itself drives publishRetryEvent (integration-level): a client that
// always fails with a retryable error yields one notice per attempt, plus the
// plain-words wait notice on each retry. A fake clock keeps it instant.
func TestDoChatWithRetryStreamingPublishesNotices(t *testing.T) {
	eventBus := events.NewEventBus()
	subscriberID := "retry-notice-integration"
	eventCh := eventBus.Subscribe(subscriberID)
	defer eventBus.Unsubscribe(subscriberID)

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.initSubManagers()
	a.SetEventBus(eventBus)

	// Always the same failure: the loop retries it until the budget is spent.
	client := NewScriptedClient()
	for i := 0; i < 64; i++ {
		client.AddResponse(NewScriptedResponseBuilder().Error(retryableFailure()).Build())
	}

	sp := &sproutProvider{
		agent:  a,
		client: client,
	}

	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	_, err := sp.doChatStreamingRetrying(context.Background(), clock.clock(), nil, nil, "", false, func(string, string) {}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error from always-failing client")
	}

	var retryNotices, waitNotices, stopNotices []string
	for {
		var ev events.UIEvent
		select {
		case ev = <-eventCh:
		default:
			goto drained
		}
		if ev.Type != events.EventTypeAgentMessage {
			continue
		}
		data, _ := ev.Data.(map[string]interface{})
		category, _ := data["category"].(string)
		message, _ := data["message"].(string)
		switch category {
		case "provider_retry":
			retryNotices = append(retryNotices, message)
		case "provider_retry_wait":
			switch {
			case strings.Contains(message, "retrying in"):
				waitNotices = append(waitNotices, message)
			default:
				stopNotices = append(stopNotices, message)
			}
		}
	}
drained:

	// One provider notice per failed attempt, and the last one says the loop
	// gave up rather than trailing off.
	if len(retryNotices) < 2 {
		t.Fatalf("expected a provider_retry notice per attempt, got %d", len(retryNotices))
	}
	if last := retryNotices[len(retryNotices)-1]; !strings.Contains(last, "giving up") {
		t.Errorf("last provider notice should say giving up, got: %q", last)
	}
	// One plain-words wait notice per retry, so the run visibly stays alive.
	if len(waitNotices) != len(retryNotices)-1 {
		t.Errorf("wait notices = %d, want one per retry (%d)", len(waitNotices), len(retryNotices)-1)
	}
	if len(stopNotices) != 1 {
		t.Errorf("expected one stop notice explaining the budget, got %d: %v", len(stopNotices), stopNotices)
	}
}

// TestDoChatRetriesPastEmptyCompletionNotice pins the notice text for the
// outage a platform deploy produces: retries say in plain words that the model
// service did not answer and how long the wait is, so the run reads as alive
// rather than stalled. A fake clock keeps it instant.
func TestDoChatRetriesPastEmptyCompletionNotice(t *testing.T) {
	eventBus := events.NewEventBus()
	subscriberID := "retry-wait-notice"
	eventCh := eventBus.Subscribe(subscriberID)
	defer eventBus.Unsubscribe(subscriberID)

	a := &Agent{state: NewAgentStateManager(false)}
	a.initSubManagers()
	a.SetEventBus(eventBus)

	client := NewScriptedClient(
		NewScriptedResponseBuilder().Error(fmt.Errorf("%w: provider returned empty response", core.ErrZeroChoices)).Build(),
		NewScriptedResponseBuilder().Error(errors.New("connection reset by peer")).Build(),
		NewScriptedResponseBuilder().Content("recovered").Build(),
	)
	sp := &sproutProvider{agent: a, client: client}

	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: "hi"}}}
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	resp, err := sp.doChatRetrying(context.Background(), clock.clock(), req)
	if err != nil {
		t.Fatalf("expected the outage to be ridden out, got: %v", err)
	}
	if content := resp.Choices[0].Message.Content; content != "recovered" {
		t.Errorf("content = %q, want %q", content, "recovered")
	}

	var waits []string
	for {
		select {
		case ev := <-eventCh:
			if ev.Type != events.EventTypeAgentMessage {
				continue
			}
			data, _ := ev.Data.(map[string]interface{})
			if category, _ := data["category"].(string); category != "provider_retry_wait" {
				continue
			}
			message, _ := data["message"].(string)
			if strings.Contains(message, "retrying in") {
				waits = append(waits, message)
			}
		default:
			goto drained
		}
	}
drained:
	if len(waits) != 2 {
		t.Fatalf("wait notices = %d, want one per retry (2): %v", len(waits), waits)
	}
	if waits[0] != "The model service did not answer; retrying in 1 s." {
		t.Errorf("unexpected first wait notice: %q", waits[0])
	}
	if waits[1] != "The model service did not answer; retrying in 2 s." {
		t.Errorf("unexpected second wait notice: %q", waits[1])
	}
}

// TestDoChatStoppingRetriesAtContextDeadline: the non-streaming path stops
// retrying when the context's deadline passes, rather than waiting out the
// budget, and returns the provider error of the last attempt.
func TestDoChatStoppingRetriesAtContextDeadline(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The context ends during the first wait.
	clock.onSleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	outerr := errors.New("Post \"https://model.example/v1/chat/completions\": read tcp: connection reset by peer")
	client := NewScriptedClient(
		NewScriptedResponseBuilder().Error(outerr).Build(),
		NewScriptedResponseBuilder().Content("should not run").Build(),
	)
	sp := &sproutProvider{client: client}

	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: "hi"}}}
	_, err := sp.doChatRetrying(ctx, clock.clock(), req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := client.GetIndex(); n != 1 {
		t.Errorf("client saw %d attempts, want 1 (no attempt after the deadline)", n)
	}
}
