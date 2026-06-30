package agent

import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/agent_tools/computer_use"
)

var (
	computerUseActiveMu    sync.RWMutex
	computerUseActiveAgent *Agent
)

// SetActiveComputerUseAgent marks a as the agent currently driving
// computer_use actions. Called from agent creation / ApplyPersona when
// the computer_user persona activates. Cleared when the persona is
// deactivated or the agent is shut down.
func SetActiveComputerUseAgent(a *Agent) {
	computerUseActiveMu.Lock()
	defer computerUseActiveMu.Unlock()
	computerUseActiveAgent = a
}

// getActiveComputerUseAgent returns the current agent driving computer-use
// actions, or nil if no agent is bound.
func getActiveComputerUseAgent() *Agent {
	computerUseActiveMu.RLock()
	defer computerUseActiveMu.RUnlock()
	return computerUseActiveAgent
}

// computerUseDestructiveAppGateFn is the PreActionHook bound to the
// auditing backend in RegisterComputerUseTools. It looks up the active
// agent and delegates to its checkComputerUseDestructiveAppGate method.
func computerUseDestructiveAppGateFn(action string, args map[string]any) error {
	agent := getActiveComputerUseAgent()
	if agent == nil {
		return nil // no agent bound — skip gate
	}
	fg, err := computer_use.GetForegroundApp()
	if err != nil {
		if agent.debug {
			agent.debugLog("[computer-use] foreground detection unavailable, skipping destructive-app gate: %v\n", err)
		}
		return nil
	}
	return agent.checkComputerUseDestructiveAppGate(action, args, fg)
}
