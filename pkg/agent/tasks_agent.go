package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// taskRole is the role a task's model calls are attributed to, so the spend
// shows up under tasks rather than silently inflating the coder role.
const taskRole = "task"

// newTaskAgent builds a full agent to run one task inside its worktree.
//
// It is deliberately not a subagent: a task needs the primary's system
// prompt (so it carries the workspace conventions and tool guidance), the
// primary's iteration budget, and no depth limit — a task that could not
// delegate would be worse than the primary it replaces. What it does not
// share is the primary's conversation state, change tracker, or todo list.
func newTaskAgent(a *Agent, worktreePath, taskID string) (*Agent, error) {
	if a == nil {
		return nil, agenterrors.NewValidation("task needs a primary agent", nil)
	}
	if strings.TrimSpace(worktreePath) == "" {
		return nil, agenterrors.NewValidation("task needs a worktree path", nil)
	}

	clientType := a.clientType
	model := a.GetModel()
	client, err := factory.CreateProviderClient(clientType, model)
	if err != nil {
		return nil, agenterrors.Wrap(err, "create task client")
	}

	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	stateMgr := NewAgentStateManager(false)
	outputMgr := NewAgentOutputManager()
	securityMgr := NewAgentSecurityManager()
	mcpMgr := NewAgentMCPManager()

	// MCP servers and terminal sessions are process-scoped, so sharing the
	// primary's managers is correct: the task reaches the same integrations
	// and does not start a private server set.
	if a.mcpSub != nil && a.mcpSub.GetManager() != nil {
		mcpMgr.SetManager(a.mcpSub.GetManager())
		mcpMgr.SetInherited(true)
	}

	systemPrompt := a.baseSystemPrompt
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = a.GetSystemPrompt()
	}

	taskAgent := &Agent{
		client:             client,
		systemPrompt:       systemPrompt,
		baseSystemPrompt:   systemPrompt,
		maxIterations:      a.GetMaxIterations(),
		clientType:         clientType,
		debug:              a.debug,
		configManager:      a.configManager,
		inputInjectionChan: make(chan string, inputInjectionBufferSize),
		interruptCtx:       interruptCtx,
		interruptCancel:    interruptCancel,
		parentInterruptCtx: interruptCtx,
		workspaceRoot:      worktreePath,
		state:              stateMgr,
		output:             outputMgr,
		security:           securityMgr,
		mcpSub:             mcpMgr,
		// A fresh todo manager: a task's plan is its own, and a shared one
		// would let a task overwrite the primary's progress list.
		todoMgr:  tools.NewTodoManager(),
		eventBus: a.eventBus,
		role:     taskRole,
	}

	if tm := a.GetTerminalManager(); tm != nil {
		taskAgent.SetTerminalManager(tm)
	}

	// No human answers a task's approval prompt, so run it non-interactive:
	// a risky operation fails fast instead of blocking for the approval
	// timeout. Same flag the automate path sets.
	taskAgent.SetWorkflowRun(true)

	if err := taskAgent.resolveAndApplyContextProfile(); err != nil {
		interruptCancel()
		return nil, err
	}

	taskAgent.EnableChangeTracking("task run")
	taskAgent.toolFuncs = buildAgentToolFuncs(taskAgent)

	// Tag every event with the task's own id so the CLI can route a task's
	// output to its own view instead of interleaving it with the primary's
	// transcript.
	meta := map[string]interface{}{}
	for k, v := range a.output.GetEventMetadata() {
		meta[k] = v
	}
	meta["task_id"] = taskID
	taskAgent.SetEventMetadata(meta)

	return taskAgent, nil
}

// taskCompletionSummary is the text the primary receives when a task
// finishes. It names the branch and the merge command, because "pull it in"
// is the next thing the primary has to do.
func taskCompletionSummary(t *taskRecord, status, agentOutput string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (branch %s) %s after %s.\n", t.ID, t.Branch, status, t.elapsed().Round(time.Second))
	fmt.Fprintf(&b, "Worktree: %s\n", t.Path)
	if t.Commit != "" {
		fmt.Fprintf(&b, "Commit: %s\n", t.Commit)
	}
	if status == "completed" {
		fmt.Fprintf(&b, "To bring the work in: `/task merge %s` (or `git merge --no-ff %s`).\n", t.ID, t.Branch)
	} else {
		b.WriteString("The run did not finish cleanly; the worktree is kept uncommitted for inspection.\n")
	}
	if out := strings.TrimSpace(agentOutput); out != "" {
		b.WriteString("\n--- task output ---\n")
		b.WriteString(truncateTaskOutput(out, 4000))
		b.WriteString("\n")
	}
	return b.String()
}

// truncateTaskOutput keeps the tail of a long output, which is where the
// conclusion lives.
func truncateTaskOutput(out string, limit int) string {
	if len(out) <= limit {
		return out
	}
	return "…(truncated)…\n" + out[len(out)-limit:]
}
