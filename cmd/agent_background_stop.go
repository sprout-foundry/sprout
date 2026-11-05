//go:build !js

package cmd

import "github.com/sprout-foundry/sprout/pkg/agent"

// stopBackgroundSessionsAtRunEnd makes a platform run with a task deadline end
// on a quiet machine. When SPROUT_TASK_DEADLINE is set (the run is stopped at
// that time anyway), the run's background sessions are stopped at the handoff
// and again when it ends — see stopBackgroundProcesses in agent_deadline.go.
//
// The returned function undoes the wiring; call it when the run returns. A run
// without a deadline is not a platform run: nothing is wired, and the
// background sessions it may legitimately leave running are left alone.
func stopBackgroundSessionsAtRunEnd(a *agent.Agent) func() {
	if !hasTaskDeadline() {
		return func() {}
	}
	stop := func() {
		if a == nil {
			return
		}
		// The manager is attached to the agent at startup, but the agent
		// may also have created one lazily; stop whichever exists.
		if bpm := a.GetBackgroundProcessManager(); bpm != nil {
			bpm.StopAll()
		}
	}
	previous := stopBackgroundProcesses
	stopBackgroundProcesses = stop
	return func() { stopBackgroundProcesses = previous }
}
