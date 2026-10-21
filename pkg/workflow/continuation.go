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
	"errors"
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

// ContinuationStallSummary is the workflow-layer view of a stalled turn's
// summary (the repeated tool + args + an output excerpt). It is kept small
// and self-contained in this package so the run record and the
// continuation loop do not depend on the agent package's internals; the
// agent's StallSummary is mapped onto it by recordContinuationStall.
type ContinuationStallSummary struct {
	// Tool is the repeated tool name (e.g. "shell_command").
	Tool string `json:"tool"`
	// Args is the normalised argument string for the repeated call.
	Args string `json:"args,omitempty"`
	// Output is a short excerpt of the repeated call's last output.
	Output string `json:"output,omitempty"`
	// Repeats is how many times the call ran (or the idle-call count).
	Repeats int `json:"repeats"`
	// Reason classifies the stall ("identical_calls" or "idle_calls").
	Reason string `json:"reason,omitempty"`
}

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
	// StalledTurns is how many continuation turns ended on the repeat
	// guard's stall stop (a turn that kept re-issuing the same tool call
	// or made no progress). Each stalled turn counts as a no-progress turn.
	StalledTurns int `json:"stalled_turns,omitempty"`
	// SkippedItems is the set of TODO items the coordinator was stuck on
	// when two consecutive stalled turns both failed to advance them. A
	// run records them so a post-mortem (or the user) knows which items the
	// coordinator could not make progress on and moved past.
	SkippedItems []string `json:"skipped_items,omitempty"`
	// LastStallSummary is the summary of the most recent stalled turn (the
	// repeated call + its last output), carried in the run record. Nil when
	// no turn stalled.
	LastStallSummary *ContinuationStallSummary `json:"last_stall_summary,omitempty"`
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
	// Stall tracking: lastTurnStalled / lastStallSummary steer the NEXT
	// turn's prompt to the stall prompt (naming the stalled call).
	// prevWasStall / prevStallLeadItem implement the "two consecutive
	// stalls on the same item" skip contract: a stall is attributed to the
	// first-runnable `[ ]` item the coordinator was probing, and a second
	// consecutive stall on that same item marks it skipped for the run.
	var lastTurnStalled bool
	var lastStallSummary *ContinuationStallSummary
	var prevWasStall bool
	var prevStallLeadItem string
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
		switch {
		case lastTurnStalled:
			// The previous turn stalled: steer it with the stall prompt,
			// which names the stalled call and tells the coordinator to
			// record the blocker and move on rather than re-probe.
			turnPrompt = stallContinuationPrompt(lastStallSummary)
		case idleTurns > 0:
			turnPrompt = DefaultContinuationIdlePrompt
		}
		turnErr := queryExecutor(ctx, chatAgent, eventBus, turnPrompt)
		result.Continuations++

		// Detect a stalled turn: the agent records a stall summary on the
		// turn (via the repeat guard's stall stop) OR the executor returned
		// the typed stall error. Either way, a stalled turn counts as a
		// no-progress turn (handled by the idle-turn gate below) and drives
		// the next turn's prompt. A stall is attributed to the first
		// runnable `[ ]` item the coordinator was probing (the leading item);
		// two CONSECUTIVE stalls on the same leading item mark that item
		// skipped for the run.
		stalled, stallSummary := continuationTurnStalled(chatAgent, turnErr)
		if stalled {
			// Consume the summary so it is turn-scoped: a later turn that
			// ends cleanly must not be read as another stall off the stale
			// summary (the agent clears it at its own turn open, but the
			// loop must not depend on that).
			chatAgent.ClearStallSummary()
			result.StalledTurns++
			result.LastStallSummary = stallSummary
			lastStallSummary = stallSummary
			leadItem := firstRunnableTodoItem(todoFile)
			// A second consecutive stall on the same leading item means the
			// coordinator is stuck on it: mark it skipped so a post-mortem
			// knows which item could not be made to progress.
			if prevWasStall && prevStallLeadItem == leadItem && !containsString(result.SkippedItems, leadItem) {
				result.SkippedItems = append(result.SkippedItems, leadItem)
			}
			prevWasStall = true
			prevStallLeadItem = leadItem
		} else {
			// A turn that did not stall resets the "consecutive stalls"
			// chain: the skip contract requires the stalls to be consecutive.
			prevWasStall = false
			prevStallLeadItem = ""
		}
		lastTurnStalled = stalled

		after := snapshotContinuationProgress(gitDir, todoFile)
		progressed := madeContinuationProgress(before, after)
		runnable := ReadTodoStatus(todoFile).Runnable
		result.RunnableItems = runnable

		if err := EmitWorkflowOrchestrationEvent(cfg, "workflow_continuation_turn_completed", map[string]interface{}{
			"turn":           result.Continuations,
			"progressed":     progressed,
			"idle_turns":     nextIdleTurns(idleTurns, progressed),
			"runnable_items": runnable,
			"has_error":      turnErr != nil && !stalled,
			"stalled":        stalled,
		}); err != nil {
			console.GlyphWarning.Printf("Failed to emit continuation event: %v", err)
		}

		if turnErr != nil && !stalled {
			// A stalled turn returns a typed stall error but is a no-progress
			// turn, not a fatal turn error — it is handled by the idle gate
			// below (it counts as idle) and the next turn gets the stall
			// prompt. Only a non-stall error stops the loop as turn_error.
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

// continuationTurnStalled reports whether a continuation turn ended on the
// repeat guard's stall stop. It checks the agent's stall summary (set by the
// in-turn guard when a call crossed a threshold) and falls back to the typed
// stall error from the executor. It returns whether the turn stalled and the
// stall summary (the workflow-layer view) to carry into the run record and
// the next turn's prompt. A nil agent yields (stalled, nil).
func continuationTurnStalled(chatAgent *agent.Agent, turnErr error) (bool, *ContinuationStallSummary) {
	if chatAgent != nil {
		if s := chatAgent.GetStallSummary(); s != nil {
			return true, &ContinuationStallSummary{
				Tool:    s.Tool,
				Args:    s.Args,
				Output:  s.Output,
				Repeats: s.Repeats,
				Reason:  s.Reason,
			}
		}
	}
	// The executor surfaced a typed stall error without the agent recording a
	// summary (e.g. a test executor that returns the error directly).
	if turnErr != nil && errors.Is(turnErr, agent.ErrTurnStalled) {
		return true, nil
	}
	return false, nil
}

// stallContinuationPrompt builds the continuation prompt that follows a
// stalled turn. It starts from the stall prompt (which tells the coordinator
// to record the blocker and move on) and, when the stall summary is
// available, names the stalled call and its last output so the coordinator
// knows exactly which probe to stop.
func stallContinuationPrompt(summary *ContinuationStallSummary) string {
	if summary == nil {
		return DefaultContinuationStallPrompt
	}
	extra := ""
	if strings.TrimSpace(summary.Tool) != "" {
		extra = fmt.Sprintf(" (stalled call: %s", summary.Tool)
		if strings.TrimSpace(summary.Args) != "" {
			extra += " " + snippetForPrompt(summary.Args)
		}
		if strings.TrimSpace(summary.Output) != "" {
			extra += "; last output: " + snippetForPrompt(summary.Output)
		}
		extra += ")"
	}
	return DefaultContinuationStallPrompt + extra
}

// snippetForPrompt shortens a string for embedding in a prompt: whitespace is
// collapsed and it is capped so a huge argument or output does not bloat the
// prompt.
func snippetForPrompt(s string) string {
	s = strings.TrimSpace(s)
	const max = 120
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// firstRunnableTodoItem returns the text of the first `[ ]` item in the TODO
// file (the one the coordinator is probing) — e.g. "Item one" for the line
// "- [ ] Item one" — or "" when the file is missing, unreadable, or has no
// runnable items. It is the attribution key for "two consecutive stalls on
// the same item" (a stall is attributed to the leading item; a second
// consecutive stall on it marks it skipped).
func firstRunnableTodoItem(todoFile string) string {
	data, err := os.ReadFile(filepath.Clean(todoFile))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !continuationUncheckedRe.MatchString(line) {
			continue
		}
		// Strip the "- [ ]" checkbox to expose the item's own text.
		idx := strings.Index(line, "- [ ]")
		if idx < 0 {
			return strings.TrimSpace(line)
		}
		return strings.TrimSpace(line[idx+len("- [ "):])
	}
	return ""
}

// containsString reports whether slice contains s.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
