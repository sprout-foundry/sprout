package agent

import (
	"context"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// With ask_user_ends_run, a question in a non-interactive run ends the run
// and is kept as its pending question.
func TestAskUserEndsRunKeepsTheQuestion(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()
	if err := a.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.AskUserEndsRun = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.workflowRun.Store(true) // no human at this run

	_, err := newAgentAskUserService(a).Ask(context.Background(), tools.AskUserRequest{Question: "Green or blue?"})
	if err == nil {
		t.Fatal("the ask should end the run with an error to the model")
	}
	q := a.PendingQuestion()
	if q == nil || q.Question != "Green or blue?" {
		t.Fatalf("pending question = %+v", q)
	}
}

// Without the setting the question is not kept.
func TestAskUserWithoutEndsRunKeepsNoQuestion(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()
	a.workflowRun.Store(true)
	_, _ = newAgentAskUserService(a).Ask(context.Background(), tools.AskUserRequest{Question: "x"})
	if a.PendingQuestion() != nil {
		t.Fatal("a question was kept without ask_user_ends_run")
	}
}
