package commands

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// TaskCommand implements /task: start a background task in its own worktree,
// inspect one, or merge its branch back.
type TaskCommand struct {
	outputSink
}

// taskIDPattern matches the background-task IDs the registry mints
// (bg-task-<n>). /task uses it to tell "show this task" from "start a task
// with this one-word description".
var taskIDPattern = regexp.MustCompile(`^bg-task-\d+$`)

func (c *TaskCommand) Name() string { return "task" }

// SafeDuringSteer returns false - a task creates a worktree and a branch,
// which is repository state the in-flight turn may be reading.
func (c *TaskCommand) SafeDuringSteer() bool { return false }

func (c *TaskCommand) Description() string {
	return "Run a task in the background in its own worktree"
}

func (c *TaskCommand) Usage() string {
	return strings.Join([]string{
		"/task <description>   Start a task in the background: names a branch,",
		"                      creates a worktree under .sprout/tasks/, and runs",
		"                      a full agent there. You are notified when it ends.",
		"/task list            List tasks (same as /tasks).",
		"/task show <id>       Show a task's state and output.",
		"/task <id>            Same as /task show <id>.",
		"/task merge <id>      Merge a finished task's branch into the current",
		"                      branch (--no-ff) and remove its worktree.",
		"",
		"The branch name follows the workspace's AGENTS.md conventions, chosen",
		"by one call to the default model. Tasks run non-interactively, so a",
		"task that needs a decision fails instead of waiting for an answer.",
		"",
		"Stop a running task with /tasks stop <id>.",
	}, "\n")
}

func (c *TaskCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}
	if len(args) == 0 {
		c.println(c.Usage())
		return nil
	}

	switch args[0] {
	case "list":
		c.println(chatAgent.BackgroundTasksSummary())
		return nil
	case "show":
		if len(args) < 2 {
			return errors.New("usage: /task show <id>")
		}
		detail, err := chatAgent.TaskDetail(args[1])
		if err != nil {
			return err
		}
		c.println(formatTaskDetail(detail))
		return nil
	case "merge":
		if len(args) < 2 {
			return errors.New("usage: /task merge <id>")
		}
		msg, err := chatAgent.MergeTask(context.Background(), args[1])
		if err != nil {
			return err
		}
		c.println(msg)
		return nil
	}

	// A single argument that is a task ID shows that task. The ID shape is
	// checked explicitly rather than "does this resolve to a task", so a
	// one-word description can never be swallowed as a detail request.
	if len(args) == 1 && taskIDPattern.MatchString(args[0]) {
		if detail, err := chatAgent.TaskDetail(args[0]); err == nil {
			c.println(formatTaskDetail(detail))
			return nil
		}
	}

	rec, err := chatAgent.StartTask(context.Background(), agent.StartTaskOptions{
		Description: strings.Join(args, " "),
	})
	if err != nil {
		return err
	}
	c.printf("Started %s on branch %s\n", rec.ID, rec.Branch)
	c.printf("Worktree: %s\n", rec.Path)
	c.printf("You will be notified when it finishes. `/tasks` lists tasks, `/tasks %s` shows its progress.\n", rec.ID)
	return nil
}

// formatTaskDetail renders one task for display.
func formatTaskDetail(t agent.TaskInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s · %s after %s\n", t.ID, t.Status, t.Branch, t.Elapsed.Round(time.Second))
	fmt.Fprintf(&b, "Worktree: %s\n", t.Worktree)
	if t.Commit != "" {
		fmt.Fprintf(&b, "Commit: %s\n", t.Commit)
	}
	if out := strings.TrimSpace(t.Summary); out != "" {
		b.WriteString("\n")
		b.WriteString(out)
		b.WriteString("\n")
	}
	return b.String()
}
