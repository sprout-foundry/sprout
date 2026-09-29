package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
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
