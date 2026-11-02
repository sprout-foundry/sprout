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
		// Depth 1: the per-turn hooks that assume one turn per agent at a
		// time are keyed on depth. A task runs concurrently with the
		// primary's own turn, so it must not run the turn-end quality and
		// verification hooks (they read per-agent turn state the primary is
		// using) or publish progress-completion events under an empty run id
		// (which a consumer reads as an extra "run finished" for the
		// primary's run). Delegation still works: the default depth limit is
		// 1, so a depth-1 task can spawn subagents.
		subagentDepth: 1,
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

	// A stable session id keeps the task's own conversation state in its own
	// file: autoSaveState would otherwise mint a timestamp id on the first
	// save, and two tasks starting in the same second would share one.
	taskAgent.SetSessionID("task_" + taskID)

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
func taskCompletionSummary(id, branch, path, commit, status string, elapsed time.Duration, agentOutput string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (branch %s) %s after %s.\n", id, branch, status, elapsed.Round(time.Second))
	fmt.Fprintf(&b, "Worktree: %s\n", path)
	if commit != "" {
		fmt.Fprintf(&b, "Commit: %s\n", commit)
	}
	if status == "completed" {
		fmt.Fprintf(&b, "To bring the work in: `/task merge %s` (or `git merge --no-ff %s`).\n", id, branch)
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
