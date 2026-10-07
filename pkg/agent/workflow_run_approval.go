package agent

// Workflow-run approval policy.
//
// A workflow/automate run is driven by a config file, not a person sitting at
// the keyboard. Even when such a run is launched from a terminal (so os.Stdin
// is a TTY and the logger reports itself interactive), there is no human
// answering the Caution approval prompt — waiting on it just stalls the run for
// up to the approval-prompt timeout with nobody to respond.
//
// A workflow run therefore declares itself non-interactive for approval
// purposes: Caution results resolve from the configured risk profile
// (permissive-by-default in a non-interactive run) without prompting, while
// hard blocks still block. The flag is set once by the CLI when the run is
// driven by a workflow config and read by every approval surface.

// SetWorkflowRun marks the agent as executing a workflow/automate run, which is
// non-interactive for approval purposes regardless of the console's
// interactivity. Safe to call before any turn runs.
//
// The flag is process-scoped for the life of the run: a workflow run is a
// distinct, direct-mode invocation (cmd/agent_modes.go forces isInteractive
// false when a workflow config drives the run and returns when it ends), so
// the agent instance is not reused for a later interactive session. If that
// ever changes, call SetWorkflowRun(false) when the run ends.
func (a *Agent) SetWorkflowRun(v bool) {
	if a == nil {
		return
	}
	a.workflowRun.Store(v)
}
