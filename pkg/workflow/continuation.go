//go:build !js

package workflow

// continuation.go — the coordinator continuation loop. A coordinator
// workflow runs one long "initial" agent turn; when that turn returns a
// final answer the process used to exit even though runnable `[ ]` items
// remained. This loop re-reads the TODO file after every turn and, while
// runnable items remain AND the turn made progress, issues another turn
// with a short prompt. Progress means the session produced a new git
// commit or newly ticked a `[ ]` item. A turn that does neither is idle: the
// next turn gets a prompt naming that, and MaxIdleTurns consecutive idle
// turns stop the loop so permanently skipped items cannot spin forever.
//
// This is distinct from the Loop (RunAgentWorkflowLoop, loop.go): Loop
// clears the conversation between items and drives each with a stateless
// gate call; continuation keeps the coordinator context and just asks it
// to carry on. The item runtime deliberately keeps the coordinator
// approach.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	gitpkg "github.com/sprout-foundry/sprout/pkg/git"
)

// ContinuationStopReason describes why the continuation loop stopped. It is
// surfaced in the run record and events so a post-mortem can tell a clean
// finish from a no-progress stop.
type ContinuationStopReason string

const (
	// ContinuationStopNoRunnableItems — no `[ ]` items remain (all done,
	// or only non-item prose remains).
	ContinuationStopNoRunnableItems ContinuationStopReason = "no_runnable_items"
	// ContinuationStopNoProgress — runnable items remain but MaxIdleTurns
	// consecutive turns neither committed nor ticked an item (e.g.
	// permanently skipped items).
	ContinuationStopNoProgress ContinuationStopReason = "no_progress"
	// ContinuationStopContextCancelled — the run was cancelled mid-turn.
	ContinuationStopContextCancelled ContinuationStopReason = "context_cancelled"
	// ContinuationStopBudgetExceeded — the USD budget was hit.
	ContinuationStopBudgetExceeded ContinuationStopReason = "budget_exceeded"
	// ContinuationStopMaxContinuations — the hard turn cap was reached.
	ContinuationStopMaxContinuations ContinuationStopReason = "max_continuations"
	// ContinuationStopTurnError — a continuation turn returned an error.
	ContinuationStopTurnError ContinuationStopReason = "turn_error"
)

// ContinuationResult reports the outcome of a continuation run so the caller
// can record the stop reason in the run record.
type ContinuationResult struct {
	// Continuations is the number of continuation turns issued after the
	// initial turn.
	Continuations int `json:"continuations"`
	// StopReason is the classified stop condition.
	StopReason ContinuationStopReason `json:"stop_reason"`
	// RunnableItems is the number of `[ ]` items observed when the loop
	// stopped (0 for a clean finish).
	RunnableItems int `json:"runnable_items"`
	// Err is set when StopReason is turn_error or context_cancelled.
	Err error `json:"-"`
}

// todoStatus is the parsed state of the TODO file used by the continuation
// gate: how many runnable `[ ]` items remain and how many are checked.
type todoStatus struct {
	// Runnable is the count of `- [ ]` items.
	Runnable int
	// Done is the count of `- [x]`/`- [X]` items.
	Done int
}

var (
	continuationUncheckedRe = regexp.MustCompile(`^\s*- \[ \]`)
	continuationCheckedRe   = regexp.MustCompile(`^\s*- \[[xX]\]`)
)

// ReadTodoStatus reads a TODO file and counts runnable and completed items.
// A missing or unreadable file yields a zero status (nothing runnable) with
// no error — the continuation gate treats that as "nothing left to do"
// rather than failing the run. This is one of the two gates that must agree
// before another turn is issued.
func ReadTodoStatus(todoFile string) todoStatus {
	data, err := os.ReadFile(filepath.Clean(todoFile))
	if err != nil {
		return todoStatus{}
	}
	var status todoStatus
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case continuationUncheckedRe.MatchString(line):
			status.Runnable++
		case continuationCheckedRe.MatchString(line):
			status.Done++
		}
	}
	return status
}

// hasRunnableTodoItem reports whether the TODO file still holds at least one
// runnable `[ ]` item. It reuses the loop's findNextTodoItem scanner so the
// notion of a runnable item stays identical to the gate loop, and so an
// unreadable file is treated the same way (no runnable item).
func hasRunnableTodoItem(todoFile string) bool {
	_, _, err := findNextTodoItem(todoFile, 0)
	return err == nil
}

// headCommit returns the current git HEAD sha for dir, or "" when dir is not
// inside a git repository or HEAD cannot be resolved (e.g. a repo with no
// commits yet). Callers must treat "" as unknown and fall back to the tick
// count for progress detection.
func headCommit(dir string) string {
	out, err := gitpkg.SafeGitCmd(dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// continuationProgress is the progress snapshot taken at the start of a turn
// and compared after the turn returns: a new HEAD or a higher done-count
// means the turn did something and another turn is worth issuing.
type continuationProgress struct {
	head     string
	todoDone int
}

// RunInitialContinuation runs the coordinator continuation loop after the
// initial agent turn has completed. It issues continuation turns via
// queryExecutor until no runnable `[ ]` items remain, the session stops
// making progress, the budget is hit, the context is cancelled, or the
// configured MaxContinuations cap is reached. The returned ContinuationResult
// carries the stop reason for the run record.
//
// queryExecutor is the same executor the initial turn used, so the
// coordinator keeps its context (continuation does NOT clear the
// conversation between turns — that is the whole point).
func RunInitialContinuation(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, cfg *AgentWorkflowConfig, state *WorkflowExecutionState, queryExecutor QueryExecutor) (ContinuationResult, error) {
	result := ContinuationResult{StopReason: ContinuationStopNoRunnableItems}
	if cfg == nil || cfg.Continuation == nil || !cfg.Continuation.IsEnabled() {
		return result, nil
	}
	if state == nil {
		state = NewWorkflowExecutionState()
	}

	cont := cfg.Continuation
	todoFile := cont.TodoFile
	if strings.TrimSpace(todoFile) == "" {
		todoFile = DefaultContinuationTodoFile
	}
	prompt := strings.TrimSpace(cont.Prompt)
	if prompt == "" {
		prompt = DefaultContinuationPrompt
	}
	maxContinuations := cont.MaxContinuations
	if maxContinuations <= 0 {
		maxContinuations = DefaultMaxContinuations
	}
	maxIdleTurns := cont.MaxIdleTurns
	if maxIdleTurns <= 0 {
		maxIdleTurns = DefaultMaxIdleTurns
	}
	idleTurns := 0
	// The git-HEAD probe needs a directory inside the repository. Prefer the
	// TODO file's location (correct under test, where CWD is the package
	// dir) and fall back to the agent's workspace root.
	gitDir := filepath.Dir(todoFile)
	if chatAgent != nil {
		if root := strings.TrimSpace(chatAgent.GetWorkspaceRoot()); root != "" {
			gitDir = root
		}
	}

	result.RunnableItems = ReadTodoStatus(todoFile).Runnable
	if result.RunnableItems == 0 {
		return result, nil
	}

	fmt.Println()
	console.GlyphAction.Printf("Coordinator continuation: %d runnable TODO item(s) remain — continuing until done", result.RunnableItems)

	for {
		if result.Continuations >= maxContinuations {
			result.StopReason = ContinuationStopMaxContinuations
			break
		}
		if err := ctx.Err(); err != nil {
			result.StopReason = ContinuationStopContextCancelled
			result.Err = err
			break
		}
		if chatAgent != nil && chatAgent.FleetBudgetExceeded() {
			result.StopReason = ContinuationStopBudgetExceeded
			break
		}

		before := snapshotContinuationProgress(gitDir, todoFile)
		if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_continuation_turn_started", map[string]interface{}{
			"turn":           result.Continuations + 1,
			"runnable_items": result.RunnableItems,
		}); err != nil {
			console.GlyphWarning.Printf("Failed to emit continuation event: %v", err)
		}

		turnPrompt := prompt
		if idleTurns > 0 {
			turnPrompt = DefaultContinuationIdlePrompt
		}
		turnErr := queryExecutor(ctx, chatAgent, eventBus, turnPrompt)
		result.Continuations++

		after := snapshotContinuationProgress(gitDir, todoFile)
		progressed := madeContinuationProgress(before, after)
		runnable := ReadTodoStatus(todoFile).Runnable
		result.RunnableItems = runnable

		if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_continuation_turn_completed", map[string]interface{}{
			"turn":           result.Continuations,
			"progressed":     progressed,
			"idle_turns":     nextIdleTurns(idleTurns, progressed),
			"runnable_items": runnable,
			"has_error":      turnErr != nil,
		}); err != nil {
			console.GlyphWarning.Printf("Failed to emit continuation event: %v", err)
		}

		if turnErr != nil {
			result.StopReason = ContinuationStopTurnError
			result.Err = turnErr
			if !hasRunnableTodoItem(todoFile) {
				result.StopReason = ContinuationStopNoRunnableItems
				result.Err = nil
			}
			break
		}

		if runnable == 0 {
			result.StopReason = ContinuationStopNoRunnableItems
			break
		}
		idleTurns = nextIdleTurns(idleTurns, progressed)
		if idleTurns >= maxIdleTurns {
			result.StopReason = ContinuationStopNoProgress
			break
		}
	}

	// Persist a terminal marker on the execution state so a resumed run can
	// distinguish "continuation finished cleanly" from "process died".
	state.LastProvider = continuationLastProvider(chatAgent, state)
	if result.StopReason == ContinuationStopNoRunnableItems {
		state.Complete = true
	}
	if err := PersistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
		console.GlyphWarning.Printf("Failed to persist continuation state: %v", err)
	}

	fmt.Println()
	switch result.StopReason {
	case ContinuationStopNoRunnableItems:
		console.GlyphSuccess.Printf("Coordinator continuation complete: no runnable TODO items remain (turns=%d)", result.Continuations)
	default:
		console.GlyphInfo.Printf("Coordinator continuation stopped (%s): runnable=%d turns=%d", result.StopReason, result.RunnableItems, result.Continuations)
	}
	return result, result.Err
}

// snapshotContinuationProgress captures HEAD and the done-count at a point in
// time so a turn can be judged on whether it changed either.
func snapshotContinuationProgress(gitDir, todoFile string) continuationProgress {
	return continuationProgress{
		head:     headCommit(gitDir),
		todoDone: ReadTodoStatus(todoFile).Done,
	}
}

// madeContinuationProgress reports whether a turn advanced the session: a new
// git commit (HEAD changed) or a newly ticked item (done-count increased).
// When git is unavailable (head == "" before and after) it falls back to the
// tick count alone — a no-git workspace still gets a working progress gate.
func madeContinuationProgress(before, after continuationProgress) bool {
	if before.head != "" && after.head != "" && before.head != after.head {
		return true
	}
	return after.todoDone > before.todoDone
}

// nextIdleTurns returns the consecutive idle-turn count after a turn:
// progress resets it, an idle turn extends it.
func nextIdleTurns(idleTurns int, progressed bool) int {
	if progressed {
		return 0
	}
	return idleTurns + 1
}

// continuationLastProvider returns the agent's current provider for the
// checkpoint, preserving any provider already recorded when the agent is
// unavailable.
func continuationLastProvider(chatAgent *agent.Agent, state *WorkflowExecutionState) string {
	if chatAgent != nil {
		if p := strings.TrimSpace(chatAgent.GetProvider()); p != "" {
			return p
		}
	}
	return state.LastProvider
}
