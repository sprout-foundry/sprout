package agent

import (
	"context"
	"strings"
	"testing"
	"time"

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
// always fails with a retryable error yields one notice per attempt.
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

	client := NewScriptedClient(
		NewScriptedResponseBuilder().Error(retryableFailure()).Build(),
		NewScriptedResponseBuilder().Error(retryableFailure()).Build(),
		NewScriptedResponseBuilder().Error(retryableFailure()).Build(),
		NewScriptedResponseBuilder().Error(retryableFailure()).Build(),
		NewScriptedResponseBuilder().Error(retryableFailure()).Build(),
	)

	sp := &sproutProvider{
		agent:  a,
		client: client,
	}

	_, err := sp.doChatWithRetryStreaming(context.Background(), nil, nil, "", func(string, string) {})
	if err == nil {
		t.Fatal("expected error from always-failing client")
	}

	noticeCount := 0
	deadline := time.After(2 * time.Second)
collect2:
	for {
		select {
		case ev := <-eventCh:
			if ev.Type == events.EventTypeAgentMessage {
				if data, ok := ev.Data.(map[string]interface{}); ok {
					if cat, _ := data["category"].(string); cat == "provider_retry" {
						noticeCount++
					}
				}
			}
		case <-deadline:
			break collect2
		}
	}

	// maxRetries=3 → attempts 0..3, each publishes one notice.
	if noticeCount != 4 {
		t.Fatalf("expected 4 provider_retry notices (one per attempt), got %d", noticeCount)
	}
}
