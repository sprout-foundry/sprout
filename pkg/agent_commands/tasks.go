package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// TasksCommand implements /tasks: inspect and stop background subagent tasks
// and /task worktree tasks.
type TasksCommand struct {
	outputSink
}

func (c *TasksCommand) Name() string { return "tasks" }

// SafeDuringSteer returns true - /tasks only reads task state or stops a task.
func (c *TasksCommand) SafeDuringSteer() bool { return true }

func (c *TasksCommand) Description() string {
	return "List, inspect, or stop background tasks"
}

func (c *TasksCommand) Usage() string {
	return strings.Join([]string{
		"/tasks              List background tasks (picker when interactive).",
		"/tasks <id>         Show a task's progress, or its result once finished.",
		"/tasks stop <id>    Stop a running task.",
		"",
		"Tasks started with /task are listed here alongside background",
		"subagents. Selecting one in the picker shows its detail; Esc returns",
		"to the prompt.",
	}, "\n")
}

func (c *TasksCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}
	switch {
	case len(args) == 0:
		if c.pickTask(chatAgent) {
			return nil
		}
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

// pickTask opens the task picker when the terminal can drive it, and reports
// whether it handled the invocation. A non-interactive session (WebUI, piped
// input) falls back to the printed list, so scripted use is unchanged.
func (c *TasksCommand) pickTask(chatAgent *agent.Agent) bool {
	if !console.Interactive(c.out()) {
		return false
	}
	items := buildTaskPickerItems(chatAgent)
	if len(items) == 0 {
		c.println("No background tasks.")
		return true
	}

	picker := console.NewSelectList(console.SelectListOptions{
		Title:      "Background Tasks",
		Items:      items,
		PageSize:   12,
		Searchable: true,
		Footer:     "enter: show detail · esc: back to prompt",
	})
	chosen, ok, err := picker.Run(context.Background())
	if err != nil || !ok || chosen == "" {
		return true
	}
	detail, err := chatAgent.BackgroundTaskDetail(chosen)
	if err != nil {
		c.println(err.Error())
		return true
	}
	c.println(detail)
	return true
}

// buildTaskPickerItems renders one row per background task, newest first.
// Tasks started with /task carry their branch in the detail line so the
// picker distinguishes them from subagent runs.
func buildTaskPickerItems(chatAgent *agent.Agent) []console.SelectItem {
	tasks := chatAgent.Tasks()
	items := make([]console.SelectItem, 0, len(tasks))
	for _, t := range tasks {
		status := t.Status
		if status == "" {
			status = "running"
		}
		detail := fmt.Sprintf("%s · %s · %s", status, t.Branch, t.Elapsed.Round(time.Second))
		if t.Commit != "" {
			detail += " · " + t.Commit
		}
		items = append(items, console.SelectItem{
			Label:  t.ID,
			Detail: detail,
			Value:  t.ID,
		})
	}
	return items
}
