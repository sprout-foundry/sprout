package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestPublishSubagentActivity_NoBatchDrop verifies that every subagent output
// line is published across batch boundaries. Regression test for the bug where
// a full BATCH_SIZE batch was reset to length 0 before flushing, so each full
// batch published nothing and was silently dropped.
func TestPublishSubagentActivity_NoBatchDrop(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("subagent_batch_test")

	const total = BATCH_SIZE*2 + 3 // spans two full batches + a remainder
	details := map[string]interface{}{"task_id": "task-1", "persona": "coder"}

	for i := 0; i < total; i++ {
		publishSubagentActivity(context.Background(), agent, "output", fmt.Sprintf("line-%d", i), details)
	}
	// Completion is a milestone — flushes the remaining buffered lines.
	publishSubagentActivity(context.Background(), agent, "complete", "done", details)

	got := map[string]bool{}
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != events.EventTypeSubagentActivity {
				continue
			}
			data, _ := ev.Data.(map[string]interface{})
			if data["phase"] != "output" {
				if data["phase"] == "complete" {
					goto done
				}
				continue
			}
			for _, line := range strings.Split(data["message"].(string), "\n") {
				got[strings.TrimSpace(line)] = true
			}
		case <-timeout:
			goto done
		}
	}
done:
	for i := 0; i < total; i++ {
		want := fmt.Sprintf("line-%d", i)
		if !got[want] {
			t.Errorf("subagent output line %q was dropped (never published)", want)
		}
	}
	t.Logf("published %d/%d distinct output lines", len(got), total)
}
