//go:build !js

package cmd

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// startCLIWakeupPoller is the CLI-mode counterpart of the WebUI wakeup
// poller (pkg/webui/wakeup_poller.go). It periodically checks whether any
// background-task completion notifications are pending and, if so,
// triggers Agent.TryAutoResume to re-invoke the agent.
//
// This is what makes the "launch a background task, check back later"
// pattern work in the CLI: without it, notifications from completed
// background shell commands sit dormant until the user types another
// message. With it, the agent wakes up automatically and acts on the
// completed task — printing its output and deciding what to do next.
//
// The poller runs on a ticker (every `interval`) and is cancelled when
// the REPL exits via `ctx`.
func startCLIWakeupPoller(ctx context.Context, chatAgent *agent.Agent, indicator *console.ActivityIndicator, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			chatAgent.TryAutoResume()
		}
	}
}
