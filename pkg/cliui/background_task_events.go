package cliui

import "github.com/sprout-foundry/sprout/pkg/agent"

// isBackgroundTaskEvent reports whether an event came from a /task agent or
// a subagent it spawned (SP-174d). A task runs alongside the primary on the
// same bus; rendering its tool lines, spinner and todo panel here makes them
// read as the primary's own. /tasks shows a task's progress, and its
// completion reaches the primary through the wakeup queue.
func isBackgroundTaskEvent(data map[string]interface{}) bool {
	id, _ := data[agent.EventMetaBackgroundTaskID].(string)
	return id != ""
}
