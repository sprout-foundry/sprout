// loop.go — the TODO-loop driver: the narrow Agent/Budget interfaces the
// loop runs against, the Result type, and the RunLoop main loop (gate call,
// item processing, build verification, triage on failure, outcome
// classification, budget heartbeat).

package workflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	agent_api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// Agent is the narrow agent surface the loop drives. *agent.Agent satisfies
// it structurally, which is what keeps this package free of an import of
// pkg/agent.
type Agent interface {
	GetProvider() string
	GetModel() string
	GenerateResponse([]agent_api.Message) (string, error)
	ProcessQueryWithContinuity(string) (string, error)
	ClearConversationHistory()
	GetMaxIterations() int
	SetMaxIterations(int)
	FleetBudgetExceeded() bool
}

// Budget is the USD budget surface the heartbeat reports on.
// *agent.FleetUsdBudget satisfies it.
type Budget interface {
	Snapshot() (spent, limit float64)
}

// HeartbeatReporter is the agent-side surface the heartbeat reads cost and
// progress from when no budget cap is attached.
type HeartbeatReporter interface {
	GetTotalCost() float64
	GetCurrentIteration() int
}

// Result is returned when the workflow completes.
type Result struct {
	ItemsProcessed int
	ItemsSkipped   int
	ItemsFailed    int
	Error          error
}

// RunLoop runs the TODO loop against an already-constructed workflow agent
// (the one built by RunWorkflowLoopInProcess in pkg/agent) in the calling
// goroutine (blocking). ctx is the caller's context: cancellation is
// checked between items and the build command runs under it.
//
// loop must have ApplyDefaults called on it. gatePromptText is the gate
// prompt file content (the agent side reads and validates it). todoFile is
// the resolved TODO file path. budget may be nil (no cap; the heartbeat
// then reports the agent's total cost via reporter). heartbeatInterval is
// only used when budget is non-nil.
func RunLoop(ctx context.Context, agent Agent, budget Budget, reporter HeartbeatReporter, loop *LoopConfig, gatePromptText, todoFile string, heartbeatInterval time.Duration) (*Result, error) {
	var stopBudget func()
	if budget != nil {
		stopBudget = startHeartbeat(agent, budget, reporter, heartbeatInterval)
	} else {
		stopBudget = func() {}
	}

	// -----------------------------------------------------------------------
	// Run the TODO loop
	// -----------------------------------------------------------------------
	result := &Result{}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "TODO loop: provider=%s model=%s todo=%s\n",
		agent.GetProvider(), agent.GetModel(), todoFile)

	startAfter := 0 // 0-based line index for scan start

	for {
		// Check context cancellation.
		if err := ctx.Err(); err != nil {
			stopBudget()
			result.Error = agenterrors.NewAgent("workflow_runner", "workflow cancelled", err)
			return result, nil
		}

		// Check budget exceeded.
		if agent.FleetBudgetExceeded() {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintf(os.Stderr, "Budget exceeded — stopping workflow loop\n")
			stopBudget()
			return result, nil
		}

		// Find next unchecked item.
		lineNum, sectionText, findErr := findNextTodoItemInFile(todoFile, startAfter)
		startAfter = 0 // Reset after first scan so subsequent iterations start from the beginning.
		if findErr != nil {
			if strings.Contains(findErr.Error(), "no unchecked") {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "TODO loop complete: processed=%d skipped=%d failed=%d\n",
					result.ItemsProcessed, result.ItemsSkipped, result.ItemsFailed)
				stopBudget()
				return result, nil
			}
			stopBudget()
			return nil, agenterrors.NewAgent("workflow_runner", "failed to find next TODO item", findErr)
		}

		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "TODO item at line %d\n", lineNum)

		// --- Gate call ---
		gateText, gateErr := agent.GenerateResponse([]agent_api.Message{
			{Role: "system", Content: gatePromptText},
			{Role: "user", Content: sectionText},
		})
		if gateErr != nil {
			fmt.Fprintf(os.Stderr, "Gate call failed: %v\n", gateErr)
			result.ItemsFailed++
			continue
		}

		gateRes, parseErr := ParseGateResponse(gateText)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "Gate parse failed: %v\n", parseErr)
			result.ItemsFailed++
			continue
		}

		fmt.Fprintf(os.Stderr, "Gate: title=%q skip=%v\n", gateRes.Title, gateRes.Skip)

		// Skip?
		if gateRes.Skip {
			reason := gateRes.SkipReason
			if reason == "" {
				reason = "no reason given"
			}
			fmt.Fprintf(os.Stderr, "Skipping: %s\n", reason)
			if mErr := MarkTodoDone(todoFile, lineNum); mErr != nil {
				fmt.Fprintf(os.Stderr, "Failed to mark item done: %v\n", mErr)
			}
			result.ItemsSkipped++
			continue
		}

		if gateRes.Prompt == "" {
			fmt.Fprintf(os.Stderr, "Gate returned empty prompt, skipping item\n")
			result.ItemsFailed++
			continue
		}

		// --- Process the item ---
		fmt.Fprintf(os.Stderr, "Processing: %s\n", gateRes.Title)

		// Save original max iterations, override with loop config.
		prevMaxIter := agent.GetMaxIterations()
		agent.SetMaxIterations(loop.MaxIterations)

		_, processErr := agent.ProcessQueryWithContinuity(gateRes.Prompt)

		// Restore max iterations.
		agent.SetMaxIterations(prevMaxIter)

		if processErr != nil {
			fmt.Fprintf(os.Stderr, "Agent processing failed: %v\n", processErr)
		}

		// --- Build verification ---
		buildFailed := false
		buildCmd := strings.TrimSpace(loop.BuildCommand)
		if buildCmd != "" {
			fmt.Fprintf(os.Stderr, "%s\n", buildCmd)
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "/bin/sh"
			}
			cmd := exec.CommandContext(ctx, shell, "-c", buildCmd)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if bErr := cmd.Run(); bErr != nil {
				fmt.Fprintf(os.Stderr, "Build failed: %v\n", bErr)
				buildFailed = true
			} else {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "Build passed\n")
			}
		}

		// --- Triage on failure ---
		retries := 0
		retrySucceeded := false
		triageSkipped := false
		for buildFailed && retries < loop.MaxRetries {
			retries++
			fmt.Fprintln(os.Stderr)
			fmt.Fprintf(os.Stderr, "Build failed — triaging (attempt %d/%d)\n", retries, loop.MaxRetries)

			triageText, triageErr := agent.GenerateResponse([]agent_api.Message{
				{Role: "system", Content: "You are a build error triage agent. Given a task title and context, decide: retry (transient/fixable) or skip (fundamental/blocking). Return ONLY JSON: {\"action\": \"retry\"|\"skip\", \"reason\": \"...\"}"},
				{Role: "user", Content: fmt.Sprintf("Task: %s\n\nPrevious attempt failed. Decide whether to retry or skip.", gateRes.Title)},
			})
			if triageErr != nil {
				fmt.Fprintf(os.Stderr, "Triage gate call failed: %v — defaulting to retry\n", triageErr)
				triageText = `{"action": "retry", "reason": "triage failed"}`
			}

			triageRes, pErr := ParseTriageResponse(triageText)
			if pErr != nil {
				fmt.Fprintf(os.Stderr, "Triage parse failed: %v — defaulting to retry\n", pErr)
				triageRes = TriageResult{Action: "retry", Reason: "parse failed"}
			}

			fmt.Fprintf(os.Stderr, "Triage: action=%s reason=%s\n", triageRes.Action, triageRes.Reason)

			if strings.EqualFold(triageRes.Action, "skip") {
				triageSkipped = true
				result.ItemsSkipped++
				fmt.Fprintf(os.Stderr, "Triage skipped: %s\n", gateRes.Title)
				break
			}

			// Retry: clear conversation history and re-run with a fix prompt.
			agent.ClearConversationHistory()

			retryPrompt := fmt.Sprintf(
				"Previous attempt failed. Fix the issue and ensure the build passes.\n\nOriginal task:\n%s",
				gateRes.Prompt)

			retryMaxIter := loop.MaxIterations / 2
			if retryMaxIter < 5 {
				retryMaxIter = 5
			}
			agent.SetMaxIterations(retryMaxIter)

			_, retryErr := agent.ProcessQueryWithContinuity(retryPrompt)
			agent.SetMaxIterations(prevMaxIter)

			if retryErr != nil {
				fmt.Fprintf(os.Stderr, "Retry agent processing failed: %v\n", retryErr)
			}

			// Re-check build.
			if buildCmd != "" {
				shell := os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
				cmd := exec.CommandContext(ctx, shell, "-c", buildCmd)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if bErr := cmd.Run(); bErr != nil {
					fmt.Fprintf(os.Stderr, "Build still fails after retry: %v\n", bErr)
				} else {
					buildFailed = false
					retrySucceeded = retryErr == nil
					fmt.Fprintln(os.Stderr)
					fmt.Fprintf(os.Stderr, "Build passed after retry\n")
				}
			}
		}

		// --- Classify outcome ---
		switch ClassifyOutcome(buildFailed, processErr, retrySucceeded, triageSkipped) {
		case OutcomeSkipped:
			// Already counted.
		case OutcomeFailed:
			result.ItemsFailed++
			fmt.Fprintf(os.Stderr, "Item failed after retries: %s\n", gateRes.Title)
		case OutcomeIncomplete:
			result.ItemsFailed++
			fmt.Fprintf(os.Stderr, "Build passes but agent didn't complete: %v\n", processErr)
		case OutcomeProcessed:
			if mErr := MarkTodoDone(todoFile, lineNum); mErr != nil {
				fmt.Fprintf(os.Stderr, "Failed to mark item done: %v\n", mErr)
			} else {
				result.ItemsProcessed++
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "Item complete: %s\n", gateRes.Title)
			}
		}

		// Clear conversation context for the next item.
		agent.ClearConversationHistory()
	}
}
