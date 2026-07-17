//go:build !js

package cmd

import (
	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/training"
)

// wireTrainingHooks connects the agent's training data collection callback
// to the actual training.PushSession implementation. This is called from
// cmd/ (which imports both pkg/agent and pkg/training) to bridge the
// circular-import boundary — pkg/agent cannot import pkg/training directly.
//
// The config is read from the config manager; if training is not enabled
// or no endpoint is set, the callback is still wired but isTrainingEnabled
// will return false and no pushes will occur.
func wireTrainingHooks(a *agent.Agent, cm *configuration.Manager) {
	if a == nil || cm == nil {
		return
	}
	cfg := cm.GetConfig()
	if cfg != nil {
		a.SetTrainingConfig(cfg.Training)
	}
	// Wire the push callback regardless of enabled state — the agent
	// checks isTrainingEnabled() before calling it, so the wiring is
	// safe even when training is off.
	a.SetTrainingPushFunc(training.PushSession)
}
