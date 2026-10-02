package commands

import (
	"errors"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// TasksCommand implements /tasks: inspect and stop background subagent tasks
// (review_changes and read-only subagents started with background runs).
type TasksCommand struct {
	outputSink
}

func (c *TasksCommand) Name() string { return "tasks" }

// SafeDuringSteer returns true - /tasks only reads task state or stops a task.
func (c *TasksCommand) SafeDuringSteer() bool { return true }

func (c *TasksCommand) Description() string {
	return "List, inspect, or stop background subagent tasks"
}

func (c *TasksCommand) Usage() string {
	return strings.Join([]string{
		"/tasks              List background subagent tasks and their status.",
		"/tasks <id>         Show a task's progress, or its result once finished.",
		"/tasks stop <id>    Stop a running task.",
	}, "\n")
}

func (c *TasksCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}
	switch {
	case len(args) == 0:
		c.println(chatAgent.BackgroundTasksSummary())
		return nil
	case args[0] == "stop":
		if len(args) < 2 {
			return errors.New("usage: /tasks stop <id>")
		}
		msg, err := chatAgent.StopBackgroundTask(args[1])
		if err != nil {
			return err
		}
		c.println(msg)
		return nil
	default:
		detail, err := chatAgent.BackgroundTaskDetail(args[0])
		if err != nil {
			return err
		}
		c.println(detail)
		return nil
	}
}
