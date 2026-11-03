package agent

import (
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// A run configured with ask_user_ends_run has no one to answer a question
// while it runs: the host (the platform, for a cloud task) asks the person
// later. The question ends the run instead of failing the tool, and the
// run's structured result carries it, with the work done so far.

// askUserEndsRun reports whether the config asks for that behaviour.
func (a *Agent) askUserEndsRun() bool {
	if a == nil || a.configManager == nil {
		return false
	}
	cfg := a.configManager.GetConfig()
	return cfg != nil && cfg.AskUserEndsRun
}

// endRunWithQuestion records req as the run's pending question and stops
// the run.
func (a *Agent) endRunWithQuestion(req tools.AskUserRequest) error {
	q := req
	a.pendingQuestion.Store(&q)
	a.TriggerInterrupt()
	return agenterrors.NewTool("ask_user", "ask_user: the question has been passed to the person; this run ends here and continues with their answer.", nil)
}

// PendingQuestion is the question that ended the run, or nil.
func (a *Agent) PendingQuestion() *tools.AskUserRequest {
	if a == nil {
		return nil
	}
	return a.pendingQuestion.Load()
}
