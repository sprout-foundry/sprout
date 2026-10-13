package agent

// subagent_worker_verify.go — pre-reliance verification for worker-role
// subagent results.
//
// A worker subagent runs on a cheaper model for routine, well-scoped tasks.
// Before the primary agent relies on a worker's result, that result is
// checked against evidence the worker actually produced, not against the
// worker's own claim of success:
//
//   - an edit applies cleanly: a task that asked for file changes must have
//     a non-empty change-tracker manifest;
//   - the stated tests pass: a test command the worker ran must have exited
//     clean (its recorded history entry carries no error);
//   - the output matches the requested format: when the caller named an
//     output format, the result must conform to it.
//
// On failure the task is retried ONCE on the coder model (the escalation),
// and that escalation is recorded so it is never silent. A failed worker
// result is never handed to the primary as an unqualified success.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// workerVerifyResult is the outcome of verifying a worker's result.
type workerVerifyResult struct {
	// Verified is true when every applicable check passed.
	Verified bool
	// Reason explains a failure (empty when Verified).
	Reason string
	// Escalated reports that the task was retried once on the coder model
	// after the first verification failed.
	Escalated bool
}

// isWorkerRoleTask reports whether a subagent task runs on the worker role,
// and so is subject to pre-reliance verification. A task is a worker task
// when its resolved role is the worker role (the worker persona resolves
// through it).
func isWorkerRoleTask(role string) bool {
	return role == configuration.RoleWorker
}

// mutationVerbs are the imperative verbs that mark a task prompt as asking
// for a file change. Matched as whole words so "address" does not match
// "add" and "prefix" does not match "fix". A worker task whose prompt asks
// for a change must produce a change-tracker entry; an empty manifest then
// means the edit did not actually land.
var mutationVerbs = map[string]bool{
	"edit": true, "modify": true, "refactor": true, "rename": true,
	"implement": true, "insert": true, "append": true, "replace": true,
	"patch": true, "write": true, "delete": true, "remove": true,
	"add": true, "fix": true, "update": true, "create": true,
}

// promptRequestsMutation reports whether the task prompt asks for a file
// change. It is deliberately conservative: only a clear mutating verb makes
// the edit check applicable, so a read/summarise task is never failed for
// producing no changes. Verbs are matched as whole words (case-insensitive).
func promptRequestsMutation(prompt string) bool {
	for _, word := range strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if mutationVerbs[word] {
			return true
		}
	}
	return false
}

// verifyWorkerResult checks a worker subagent's result against the evidence
// it produced. Checks that do not apply (a read-only task, no stated tests,
// no requested format) are skipped rather than failed, so verification never
// fails a task for something the caller did not ask for.
func verifyWorkerResult(a *Agent, spec *subagentLaunchSpec, result *SubagentResult) workerVerifyResult {
	if result == nil {
		return workerVerifyResult{Reason: "worker produced no result"}
	}
	if result.Error != nil {
		return workerVerifyResult{Reason: "worker run failed: " + result.Error.Error()}
	}
	if result.Cancelled || result.BudgetExceeded || result.Truncated {
		return workerVerifyResult{Reason: "worker run did not complete"}
	}

	// Check 1: an edit applies cleanly. Only applicable when the task asked
	// for a file change; then the change-tracker manifest must be non-empty.
	if spec != nil && promptRequestsMutation(spec.prompt) && len(result.FileChanges) == 0 {
		return workerVerifyResult{Reason: "task asked for a file change but the worker reported no modified files"}
	}

	// Check 2: the stated tests pass. If the worker ran a test command, the
	// recorded history entry must have exited clean; a claimed pass the
	// record contradicts fails verification.
	if reason := verifyStatedTests(a, result); reason != "" {
		return workerVerifyResult{Reason: reason}
	}

	// Check 3: the output matches the requested format. Only applicable when
	// the caller named a format.
	if spec != nil && spec.outputFormat != "" {
		if reason := verifyOutputFormat(result.Output, spec.outputFormat); reason != "" {
			return workerVerifyResult{Reason: reason}
		}
	}

	return workerVerifyResult{Verified: true}
}

// testCommandMarkers identify a shell command as a test run.
var testCommandMarkers = []string{
	"go test", "npm test", "npm run test", "yarn test", "pnpm test",
	"pytest", "cargo test", "make test", "vitest", "jest", "mocha", "rspec",
}

// verifyStatedTests checks the worker's recorded test commands. When the
// worker ran a test command, every such command must have exited clean; a
// recorded failure fails verification. Returns "" when the check passes or
// does not apply (no test command was run).
func verifyStatedTests(a *Agent, result *SubagentResult) string {
	if a == nil {
		return ""
	}
	// Prefer the worker's own recorded history when the result carries it;
	// otherwise fall back to the parent's history, which the runner does not
	// merge for subagents. The result is the authoritative source when set.
	history := result.ShellHistory
	if history == nil {
		history = shellHistoryRecords(a)
	}
	for command, entry := range history {
		if !isTestCommand(command) {
			continue
		}
		if entry.Failed {
			return "stated test command failed: " + command
		}
	}
	return ""
}

// shellHistoryRecords converts the parent agent's shell history into the
// minimal record form the verifier consumes. Used as a fallback when the
// result does not carry its own history.
func shellHistoryRecords(a *Agent) map[string]subagents.ShellCommandRecord {
	raw := a.GetAllShellCommandHistory()
	out := make(map[string]subagents.ShellCommandRecord, len(raw))
	for command, entry := range raw {
		if entry == nil {
			continue
		}
		out[command] = subagents.ShellCommandRecord{Command: command, Failed: entry.Error != nil}
	}
	return out
}

// isTestCommand reports whether a shell command looks like a test run.
func isTestCommand(command string) bool {
	lower := strings.ToLower(command)
	for _, marker := range testCommandMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// verifyOutputFormat checks that output conforms to the requested format.
// The check is conservative — it only fails on a clear structural mismatch —
// so a fuzzy format request never produces false failures. Returns "" when
// the output conforms.
func verifyOutputFormat(output, format string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "output is empty but a format was requested"
	}
	lowerFormat := strings.ToLower(format)

	// JSON: the output must contain a JSON object or array.
	if strings.Contains(lowerFormat, "json") {
		if !containsJSON(trimmed) {
			return "requested JSON output but the result contains no JSON object or array"
		}
	}

	// A single line / one-line format: the output must be a single line.
	if strings.Contains(lowerFormat, "one line") || strings.Contains(lowerFormat, "one-line") || strings.Contains(lowerFormat, "single line") {
		if strings.Contains(strings.TrimSpace(trimmed), "\n") {
			return "requested a single-line output but the result has multiple lines"
		}
	}

	return ""
}

// containsJSON reports whether s contains a JSON object or array somewhere in
// it (workers often wrap JSON in a sentence or a fenced code block).
func containsJSON(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{', '[':
			var v json.RawMessage
			if json.Unmarshal([]byte(s[i:]), &v) == nil {
				return true
			}
			// Not valid from this offset; keep scanning for a later opener.
		}
	}
	return false
}

// escalateWorkerTask re-runs a failed worker task ONCE on the coder model.
// It runs the same prompt and context with the coder role's resolved
// provider/model, so a routine task the cheap model could not complete is
// finished by the strong model. The escalation is recorded by the caller on
// the returned result's envelope; this function only performs the retry.
func escalateWorkerTask(ctx context.Context, a *Agent, spec *subagentLaunchSpec) (*SubagentResult, error) {
	provider, model := "", ""
	if cfg := a.GetConfig(); cfg != nil {
		provider, model = cfg.ResolveRole(configuration.RoleCoder)
	}
	return a.GetSubagentRunner().Run(ctx, spec.enhancedPrompt, SubagentOptions{
		Persona:      spec.persona,
		Model:        model,
		Provider:     provider,
		SystemPrompt: spec.systemPromptText,
		WorkingDir:   spec.workingDir,
		Role:         configuration.RoleCoder,
	}), nil
}

// verifyParallelWorkerResults verifies every worker-role task's result in a
// parallel batch and retries a failed one ONCE on the coder model, replacing
// its entry in results in place. Non-worker tasks are left untouched. A task
// is a worker task when its resolved role is the worker role.
func verifyParallelWorkerResults(ctx context.Context, a *Agent, tasks []SubagentTask, results []*SubagentResult) {
	if len(results) == 0 {
		return
	}
	byID := make(map[string]SubagentTask, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	for i, r := range results {
		if r == nil {
			continue
		}
		task, ok := byID[r.ID]
		if !ok || !isWorkerRoleTask(task.Role) {
			continue
		}
		spec := &subagentLaunchSpec{
			prompt:       task.Prompt,
			persona:      task.Persona,
			outputFormat: task.OutputFormat,
		}
		verification := verifyWorkerResult(a, spec, r)
		if verification.Verified {
			continue
		}
		// Escalate once on the coder model, re-verifying the escalated run.
		spec.enhancedPrompt = task.Prompt
		spec.systemPromptText = task.SystemPrompt
		escalated, err := escalateWorkerTask(ctx, a, spec)
		if err != nil || escalated == nil {
			continue // keep the failed original; collectParallelResults reports it
		}
		escalated.ID = r.ID
		second := verifyWorkerResult(a, spec, escalated)
		second.Escalated = true
		if !second.Verified {
			// Both the worker and the escalated run failed verification:
			// surface the failure on the escalated result so the batch
			// reports it rather than passing it through silently.
			escalated.Error = errWorkerVerificationFailed(second.Reason)
		}
		results[i] = escalated
	}
}

// errWorkerVerificationFailed is the error attached to a worker result whose
// verification (including the single escalation) failed, so the batch's
// failure handling reports it instead of a success.
func errWorkerVerificationFailed(reason string) error {
	return agenterrors.NewAgent("subagent.worker_verify", "worker verification failed: "+reason, nil)
}
