//go:build !js

package cmd

import "github.com/sprout-foundry/sprout/pkg/agent"

// backgroundStopper stops a's background sessions (startTaskRun). The
// manager is attached to the agent at startup, but the agent may also have
// created one lazily; it stops whichever exists when it is called.
func backgroundStopper(a *agent.Agent) func() {
	if a == nil {
		return nil
	}
	return func() {
		if bpm := a.GetBackgroundProcessManager(); bpm != nil {
			bpm.StopAll()
		}
	}
}
