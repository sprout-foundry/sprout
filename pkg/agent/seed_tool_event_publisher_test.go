package agent

import (
	"strings"
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestRichEventPublisherSuppressesSeedQueryEvents verifies the seed-side
// duplicate-event suppressions: seed's own query_started (sprout's
// prepareQueryRun already published one with provider/model metadata) and
// seed's retry-loop "chat failed" error events (sprout's publishRetryEvent
// and runChatQuery provide strictly richer signals for the same failures).
// Without the suppressions the WebUI sidebar log shows duplicate "Query:"
// lines and its fatal error handler fires mid-retry.
func TestRichEventPublisherSuppressesSeedQueryEvents(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("rich-publisher-test")
	defer bus.Unsubscribe("rich-publisher-test")

	a := &Agent{state: NewAgentStateManager(false)}
	a.initSubManagers()
	a.SetEventBus(bus)

	pub := newRichEventPublisher(bus, a)

	pub.Publish(core.EventTypeQueryStarted, map[string]interface{}{"query": "hi"})
	pub.Publish(core.EventTypeError, map[string]interface{}{"message": "chat failed", "error": "boom"})
	pub.Publish(core.EventTypeError, map[string]interface{}{"message": "other failure"})

	var forwarded []string
	for len(forwarded) < 1 {
		select {
		case ev := <-ch:
			forwarded = append(forwarded, ev.Type)
		default:
			t.Fatal("expected the non-suppressed error event to be forwarded")
		}
	}

	if forwarded[0] != events.EventTypeError {
		t.Fatalf("expected forwarded event to be error, got %s", forwarded[0])
	}
	// Drain any remainder — there must be no query_started and no second
	// error (the "chat failed" one was suppressed).
	for {
		select {
		case ev := <-ch:
			forwarded = append(forwarded, ev.Type)
		default:
			goto drained
		}
	}
drained:
	for _, typ := range forwarded {
		if typ == events.EventTypeQueryStarted {
			t.Error("seed's query_started should be suppressed")
		}
	}
	if len(forwarded) != 1 {
		t.Errorf("expected exactly 1 forwarded event (other error only), got %d: %v", len(forwarded), forwarded)
	}
}

// TestRichEventPublisherFillsMetricsProviderModel verifies that seed-origin
// metrics events (which lack provider/model) get both fields filled from
// the agent so the WebUI log doesn't render "Model: ? | Provider: ?".
func TestRichEventPublisherFillsMetricsProviderModel(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("rich-publisher-metrics-test")
	defer bus.Unsubscribe("rich-publisher-metrics-test")

	a := &Agent{state: NewAgentStateManager(false)}
	a.initSubManagers()
	a.SetEventBus(bus)

	pub := newRichEventPublisher(bus, a)

	pub.Publish(core.EventTypeMetricsUpdate, map[string]interface{}{"total_tokens": 5})

	select {
	case ev := <-ch:
		data, ok := ev.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map payload, got %T", ev.Data)
		}
		if _, has := data["provider"]; !has {
			t.Error("metrics payload missing provider after richEventPublisher fill")
		}
		if _, has := data["model"]; !has {
			t.Error("metrics payload missing model after richEventPublisher fill")
		}
	default:
		t.Fatal("expected metrics_update event to be forwarded")
	}
}

// TestRichEventPublisherTruncatesOversizedToolStartArgs verifies that
// seed-origin tool_start events with whole-file arguments (write_file /
// edit_file) are capped before they reach the bus — the arguments would
// otherwise fan out to every connected tab and sit in the reattach replay
// buffer. The head of the arguments string survives so consumers can still
// extract the target path.
func TestRichEventPublisherTruncatesOversizedToolStartArgs(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("rich-publisher-trunc-test")
	defer bus.Unsubscribe("rich-publisher-trunc-test")

	a := &Agent{state: NewAgentStateManager(false)}
	a.initSubManagers()
	a.SetEventBus(bus)

	pub := newRichEventPublisher(bus, a)

	bigArgs := `{"path":"/tmp/big.txt","content":"` + strings.Repeat("x", events.MaxToolEventArgsLength*2) + `"}`
	pub.Publish(core.EventTypeToolStart, map[string]interface{}{
		"tool_name":    "write_file",
		"tool_call_id": "call-1",
		"arguments":    bigArgs,
		"tool_index":   0,
	})

	// enrichEventData also emits a tool_log agent_message alongside each
	// tool_start — drain it and wait for the tool_start itself.
	data := waitForToolStart(t, ch)

	args, _ := data["arguments"].(string)
	if len(args) > events.MaxToolEventArgsLength+len("\n... (truncated)") {
		t.Errorf("arguments not truncated: %d bytes", len(args))
	}
	if data["arguments_truncated"] != true {
		t.Error("arguments_truncated marker missing")
	}
	if !strings.HasPrefix(args, `{"path":"/tmp/big.txt"`) {
		t.Error("truncation must keep the head of the arguments (path extraction)")
	}

	// Under the cap: passed through unchanged, no marker.
	pub.Publish(core.EventTypeToolStart, map[string]interface{}{
		"tool_name":    "read_file",
		"tool_call_id": "call-2",
		"arguments":    `{"path":"/tmp/small.txt"}`,
		"tool_index":   1,
	})
	data = waitForToolStart(t, ch)
	if data["arguments"] != `{"path":"/tmp/small.txt"}` {
		t.Errorf("small arguments modified: %v", data["arguments"])
	}
	if _, has := data["arguments_truncated"]; has {
		t.Error("arguments_truncated must be absent for small arguments")
	}
}

// waitForToolStart reads from ch, discarding the CLI tool_log agent_message
// side-emissions, until the next tool_start arrives.
func waitForToolStart(t *testing.T, ch <-chan events.UIEvent) map[string]interface{} {
	t.Helper()
	for {
		select {
		case ev := <-ch:
			if ev.Type != events.EventTypeToolStart {
				continue
			}
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				t.Fatalf("expected map payload, got %T", ev.Data)
			}
			return data
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for tool_start event")
		}
	}
}
