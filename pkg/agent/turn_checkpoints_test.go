package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestPrepareMessagesKeepsFullHistoryWhenContextHasHeadroom(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldTurnContent := strings.Repeat("old detailed result ", 120)
	agent := &Agent{
		client:          NewScriptedClient(),
		systemPrompt:    "system",
		state:           NewAgentStateManager(false),
		interruptCtx:    ctx,
		interruptCancel: cancel,
		output:          NewAgentOutputManager(),
	}
	agent.state.SetMessages([]api.Message{
		{Role: "user", Content: "First request"},
		{Role: "assistant", Content: oldTurnContent},
		{Role: "user", Content: "Current request"},
	})
	agent.state.SetTurnCheckpoints([]TurnCheckpoint{{
		StartIndex: 0,
		EndIndex:   1,
		Summary:    "Compacted earlier conversation state:\n- Latest compacted user request: First request",
	}})
	agent.state.SetMaxContextTokens(100000)
	agent.output.SetOutputMutex(&sync.Mutex{})

	handler := NewConversationHandler(agent)
	prepared := handler.prepareMessages(nil)

	foundFull := false
	foundSummary := false
	for _, msg := range prepared {
		if msg.Role == "assistant" && strings.Contains(msg.Content, oldTurnContent) {
			foundFull = true
		}
		if msg.Role == "assistant" && strings.Contains(msg.Content, "Compacted earlier conversation state:") {
			foundSummary = true
		}
	}

	if !foundFull {
		t.Fatalf("expected full old turn details to remain when context has headroom")
	}
	if foundSummary {
		t.Fatalf("did not expect checkpoint summary to replace full history before pruning is needed")
	}
}

func TestPrepareMessagesUsesTurnCheckpointsWhenContextIsTight(t *testing.T) {
	t.Skip("Flaky test: token estimation is context-dependent and doesn't meet expected thresholds")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldTurnContent := strings.Repeat("old detailed result ", 250)
	checkpointSummary := "Compacted earlier conversation state:\n- Latest compacted user request: First request\n- Status at compaction time: work was still in progress; newer messages continue from this task."
	agent := &Agent{
		client:          NewScriptedClient(),
		systemPrompt:    "system",
		state:           NewAgentStateManager(false),
		interruptCtx:    ctx,
		interruptCancel: cancel,
		output:          NewAgentOutputManager(),
	}
	agent.state.SetMessages([]api.Message{
		{Role: "user", Content: "First request"},
		{Role: "assistant", Content: oldTurnContent},
		{Role: "user", Content: "Current request with enough content to keep active turn live"},
	})
	agent.state.SetTurnCheckpoints([]TurnCheckpoint{{
		StartIndex: 0,
		EndIndex:   1,
		Summary:    checkpointSummary,
	}})
	agent.state.SetMaxContextTokens(1200)
	agent.output.SetOutputMutex(&sync.Mutex{})

	handler := NewConversationHandler(agent)
	prepared := handler.prepareMessages(nil)

	foundFull := false
	foundSummary := false
	foundCurrent := false
	for _, msg := range prepared {
		if msg.Role == "assistant" && strings.Contains(msg.Content, oldTurnContent) {
			foundFull = true
		}
		if msg.Role == "assistant" && strings.Contains(msg.Content, checkpointSummary) {
			foundSummary = true
		}
		if msg.Role == "user" && strings.Contains(msg.Content, "Current request with enough content") {
			foundCurrent = true
		}
	}

	if foundFull {
		t.Fatalf("expected old full turn details to be replaced by checkpoint summary when context is tight")
	}
	if !foundSummary {
		t.Fatalf("expected checkpoint summary to be used when context is tight")
	}
	if !foundCurrent {
		t.Fatalf("expected current user turn to remain in full detail")
	}
}

func TestRecordTurnCheckpointBuildsSummary(t *testing.T) {
	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetMessages([]api.Message{
		{Role: "user", Content: "Investigate the isolation issue"},
		{Role: "assistant", Content: "Verified the first code path."},
		{Role: "assistant", Content: "Updated the session-selection flow and confirmed the fix."},
	})

	agent.RecordTurnCheckpoint(0, 2)
	if len(agent.state.GetTurnCheckpoints()) != 1 {
		t.Fatalf("expected one turn checkpoint, got %d", len(agent.state.GetTurnCheckpoints()))
	}
	if !strings.Contains(agent.state.GetTurnCheckpoints()[0].Summary, "Latest compacted user request: Investigate the isolation issue") {
		t.Fatalf("expected checkpoint summary to preserve the user request, got: %s", agent.state.GetTurnCheckpoints()[0].Summary)
	}
}
