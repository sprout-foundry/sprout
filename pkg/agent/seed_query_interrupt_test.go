package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestInterruptedQueryReportsStopNotFailure(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	client := NewScriptedClient(&ScriptedResponse{Content: "never arrives", Delay: 10 * time.Second})
	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	defer ag.Shutdown()

	done := make(chan error, 1)
	go func() {
		_, runErr := ag.ProcessQuery("count to forty")
		done <- runErr
	}()
	time.Sleep(200 * time.Millisecond)
	ag.TriggerInterrupt()

	select {
	case runErr := <-done:
		if !errors.Is(runErr, ErrRunInterrupted) {
			t.Fatalf("error = %v, want ErrRunInterrupted", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("query did not stop after the interrupt")
	}
	for _, m := range ag.GetMessages() {
		if m.Role == "assistant" && strings.Contains(m.Content, "temporary error") {
			t.Errorf("a stop left an error answer in the conversation: %q", m.Content)
		}
	}
}

func TestInterruptedQueryStillReportsSpentTokens(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	notes := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notes, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstStep := &ScriptedResponse{
		ToolCalls: []api.ToolCall{{ID: "call_1", Type: "function"}},
		Usage:     ScriptedTokenUsage{PromptTokens: 900, CompletionTokens: 100, TotalTokens: 1000},
	}
	firstStep.ToolCalls[0].Function.Name = "read_file"
	firstStep.ToolCalls[0].Function.Arguments = `{"path":"` + notes + `"}`
	client := NewScriptedClient(firstStep, &ScriptedResponse{Content: "never arrives", Delay: 10 * time.Second})
	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	defer ag.Shutdown()
	bus := events.NewEventBus()
	ag.SetEventBus(bus)
	updates := bus.Subscribe("test-metrics")

	done := make(chan error, 1)
	go func() {
		_, runErr := ag.ProcessQuery("read the notes, then summarize")
		done <- runErr
	}()
	// Stop once the first step has finished: its tool has run.
	firstStepDone := time.After(5 * time.Second)
waitTool:
	for {
		select {
		case ev := <-updates:
			if ev.Type == events.EventTypeToolEnd {
				break waitTool
			}
		case <-firstStepDone:
			t.Fatal("the first step never finished")
		}
	}
	ag.TriggerInterrupt()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, ErrRunInterrupted) {
			t.Fatalf("error = %v, want ErrRunInterrupted", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("query did not stop after the interrupt")
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-updates:
			if ev.Type != events.EventTypeMetricsUpdate {
				continue
			}
			if data, ok := ev.Data.(map[string]interface{}); ok {
				if total, _ := data["total_tokens"].(int); total >= 1000 {
					return
				}
			}
		case <-timeout:
			t.Fatalf("no metrics_update with the spent tokens after the stop (agent total %d)", ag.GetTotalTokens())
		}
	}
}
