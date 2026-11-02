package commands

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func TestTaskCommand_Registered(t *testing.T) {
	registry := DefaultRegistry()
	cmd, ok := registry.GetCommand("task")
	if !ok {
		t.Fatal("/task is not registered")
	}
	if got := cmd.Name(); got != "task" {
		t.Errorf("Name() = %q, want task", got)
	}
	if _, ok := cmd.(UsageProvider); !ok {
		t.Error("/task should implement UsageProvider so /help task works")
	}
}

func TestTaskCommand_NoArgsPrintsUsage(t *testing.T) {
	var out bytes.Buffer
	cmd := &TaskCommand{}
	cmd.SetOutput(&out)

	if err := cmd.Execute(nil, &agent.Agent{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"/task <description>", "/task merge <id>", "AGENTS.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("usage missing %q:\n%s", want, got)
		}
	}
}

func TestTaskCommand_NilAgent(t *testing.T) {
	cmd := &TaskCommand{}
	cmd.SetOutput(&bytes.Buffer{})
	if err := cmd.Execute([]string{"do work"}, nil); err == nil {
		t.Fatal("expected an error without an agent")
	}
}

func TestTaskCommand_MergeRequiresID(t *testing.T) {
	cmd := &TaskCommand{}
	cmd.SetOutput(&bytes.Buffer{})
	if err := cmd.Execute([]string{"merge"}, &agent.Agent{}); err == nil {
		t.Fatal("expected a usage error for /task merge with no id")
	}
}

func TestFormatTaskDetail(t *testing.T) {
	got := formatTaskDetail(agent.TaskInfo{
		ID:       "bg-task-1",
		Branch:   "feat/add-login",
		Status:   "completed",
		Elapsed:  90 * time.Second,
		Commit:   "abc1234",
		Worktree: "/ws/.sprout/tasks/add-login",
		Summary:  "Implemented the login page.",
	})
	for _, want := range []string{"bg-task-1", "completed", "feat/add-login", "1m30s", "abc1234", "Implemented the login page."} {
		if !strings.Contains(got, want) {
			t.Errorf("detail missing %q:\n%s", want, got)
		}
	}
}

func TestBuildTaskPickerItems(t *testing.T) {
	// The picker reads the process-wide task registry, which is empty in a
	// fresh test process: the contract under test is that an agent with no
	// tasks yields no rows (so /tasks falls back to its printed list).
	items := buildTaskPickerItems(&agent.Agent{})
	if len(items) != 0 {
		t.Errorf("expected no picker rows for an agent with no tasks, got %d", len(items))
	}
}

func TestTasksCommand_NonInteractivePrintsList(t *testing.T) {
	// A bytes.Buffer is not a terminal, so /tasks must not open the picker
	// (which would block reading stdin in a test or a WebUI command run).
	var out bytes.Buffer
	cmd := &TasksCommand{}
	cmd.SetOutput(&out)

	if err := cmd.Execute(nil, &agent.Agent{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "No background tasks.") {
		t.Errorf("output = %q, want the plain list", out.String())
	}
}
