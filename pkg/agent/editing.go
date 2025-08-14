package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/git"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/security"
	"github.com/alantheprice/ledit/pkg/utils"
)

// runSingleEditWithRetries attempts a single operation with partial/full strategies and retry logic
func runSingleEditWithRetries(operation EditOperation, editInstructions string, context *AgentContext, maxRetries int) (completionTokens int, success bool, err error, opResult string) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			context.Logger.LogProcessStep(fmt.Sprintf("🔄 Retry attempt %d/%d for edit %d", attempt, maxRetries, attempt))
		}

		if false { // Implicit partial edits disabled; use explicit micro_edit tool instead
		} else {
			context.Logger.Logf("Using full file edit for %s (attempt %d)", operation.FilePath, attempt+1)
			fdiff, ferr := editor.ProcessCodeGeneration(operation.FilePath, editInstructions, context.Config, "")
			if ferr == nil {
				// Evidence verification: ensure the diff mentions the target file
				if !strings.Contains(fdiff, operation.FilePath) {
					context.Logger.LogProcessStep("⚠️ Diff did not reference target file; treating as no-op")
					return completionTokens, false, fmt.Errorf("evidence verification failed: no diff for %s", operation.FilePath), ""
				}
				// Minimal-diff guard: avoid full-file rewrites unless necessary
				if isDiffTooLarge(operation.FilePath, fdiff) {
					context.Logger.LogProcessStep("⚠️ Diff appears too large (potential full-file rewrite). Retrying as targeted partial edit.")
					pdiff, perr := editor.ProcessPartialEdit(operation.FilePath, editInstructions, context.Config, context.Logger)
					if perr == nil {
						completionTokens += utils.EstimateTokens(pdiff)
						success = true
						opResult = "✅ Edit operation completed (partial edit fallback)"
						return completionTokens, success, nil, opResult
					}
					// If partial also fails, continue with original diff but report warning
					context.Logger.LogProcessStep("ℹ️ Partial edit fallback failed; accepting original diff.")
				}
				completionTokens += utils.EstimateTokens(fdiff)
				success = true
				opResult = "✅ Edit operation completed successfully"
				return completionTokens, success, nil, opResult
			}
			err = ferr
		}

		if err != nil {
			// Special success signal
			if strings.Contains(err.Error(), "revisions applied, re-validating") {
				success = true
				opResult = "✅ Edit operation completed (with review cycle)"
				return completionTokens, success, nil, opResult
			}

			context.Logger.LogProcessStep(fmt.Sprintf("❌ Edit attempt %d failed: %v", attempt+1, err))
			if attempt == maxRetries {
				opResult = fmt.Sprintf("❌ Edit operation failed after %d attempts: %v", maxRetries+1, err)
				return completionTokens, false, err, opResult
			}
		}
	}
	// Should not reach here; return last error
	return completionTokens, false, err, opResult
}

// isDiffTooLarge heuristically detects likely full-file rewrites by counting changed lines
func isDiffTooLarge(filePath, diff string) bool {
	// If the diff contains an entire file replacement marker or has very high change counts, flag it
	// Heuristic: > 200 changed lines or more than 10 hunks suggests too big
	lines := strings.Split(diff, "\n")
	changed := 0
	hunks := 0
	inHunk := false
	for _, l := range lines {
		if strings.HasPrefix(l, "@@") { // hunk header
			hunks++
			inHunk = true
			continue
		}
		if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
			changed++
		} else if inHunk {
			// end of hunk when encountering context line after changes
			if !strings.HasPrefix(l, " ") {
				inHunk = false
			}
		}
		if changed > 200 || hunks > 10 {
			return true
		}
	}
	return false
}

// trimForCommit returns a compact string suitable for commit messages
func trimForCommit(s string, max int) string {
	t := strings.TrimSpace(s)
	if len(t) <= max {
		return t
	}
	if max <= 3 {
		return t[:max]
	}
	return t[:max-3] + "..."
}

// automatedSecurityRiskGate scans added lines that matched secret heuristics and
// uses a small/cheap local model (ollama if available) to score risk 0-100.
// Blocks auto-commit when score >= 70.
func automatedSecurityRiskGate(cfg *config.Config, logger *utils.Logger) (bool, int, error) {
	diff, err := git.GetUncommittedChanges()
	if err != nil {
		return false, 0, fmt.Errorf("failed to get uncommitted diff: %w", err)
	}
	if strings.TrimSpace(diff) == "" {
		return true, 0, nil
	}
	// Collect added lines only
	var added []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added = append(added, strings.TrimPrefix(line, "+"))
		}
	}
	if len(added) == 0 {
		return true, 0, nil
	}
	content := strings.Join(added, "\n")
	// Quick heuristic prefilter; if no patterns matched, skip LLM call
	concerns, _ := security.DetectSecurityConcerns(content)
	if len(concerns) == 0 {
		return true, 0, nil
	}
	// Choose a small local/cheap model
	model := cfg.LocalModel
	if strings.TrimSpace(model) == "" {
		model = cfg.SummaryModel
		if strings.TrimSpace(model) == "" {
			model = cfg.OrchestrationModel
		}
	}
	sys := prompts.Message{Role: "system", Content: "Return ONLY an integer 0-100: likelihood that the following added lines contain a real credential/secret. No text, no symbols. Consider typical secret formats and context. Be conservative."}
	user := prompts.Message{Role: "user", Content: fmt.Sprintf("Added lines flagged by heuristics:\n\n%s\n\nReply with a single integer 0-100.", content)}
	resp, _, err := llm.GetLLMResponse(model, []prompts.Message{sys, user}, "", cfg, 10*time.Second)
	if err != nil {
		return false, 0, err
	}
	// Parse first integer 0-100
	re := regexp.MustCompile(`\b(\d{1,3})\b`)
	m := re.FindStringSubmatch(resp)
	if len(m) < 2 {
		return true, 0, nil
	}
	n, _ := strconv.Atoi(m[1])
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	logger.LogProcessStep(fmt.Sprintf("Automated security risk score: %d", n))
	if n >= 70 {
		return false, n, nil
	}
	return true, n, nil
}

// summarizeEditResults logs and returns counts for success/failure
func summarizeEditResults(context *AgentContext, editPlan *EditPlan, operationResults []string) (successCount, failureCount int, hasFailures bool) {
	for _, result := range operationResults {
		if strings.HasPrefix(result, "❌") {
			hasFailures = true
			failureCount++
		} else if strings.HasPrefix(result, "✅") {
			successCount++
		}
	}
	context.Logger.LogProcessStep(fmt.Sprintf("📊 Edit execution summary: %d successful, %d failed out of %d total operations", successCount, failureCount, len(editPlan.EditOperations)))
	return
}

// executeEditOperations executes the planned edit operations
func executeEditOperations(context *AgentContext) error {
	context.Logger.LogProcessStep("⚡ Executing planned edit operations...")

	if context.CurrentPlan == nil {
		return fmt.Errorf("cannot execute edits without a plan")
	}
	if len(context.CurrentPlan.EditOperations) == 0 {
		return fmt.Errorf("cannot execute edits: plan contains 0 operations")
	}

	// Apply simple dependency-aware ordering if not already ordered
	// Try import/use graph ordering first; fallback to heuristic when no edges inferred
	if ordered := orderEditsByImportGraph(context.CurrentPlan.EditOperations); ordered != nil {
		context.CurrentPlan.EditOperations = ordered
	} else {
		context.CurrentPlan.EditOperations = orderEditOperationsHeuristic(context.CurrentPlan.EditOperations)
	}

	tokens, err := executeEditPlanWithErrorHandling(context.CurrentPlan, context)
	if err != nil {
		// Check if this is a non-critical error that shouldn't fail the entire task
		if strings.Contains(err.Error(), "no changes detected") ||
			strings.Contains(err.Error(), "file already") ||
			strings.Contains(err.Error(), "minimal change") {
			context.Logger.LogProcessStep("⚠️ Edit operation had minor issues but task may be complete")
			context.ExecutedOperations = append(context.ExecutedOperations, "Edit operation completed with minor issues")
			context.TokenUsage.CodeGeneration += tokens
			return nil
		}
		return fmt.Errorf("edit execution failed: %w", err)
	}

	context.TokenUsage.CodeGeneration += tokens
	context.ExecutedOperations = append(context.ExecutedOperations,
		fmt.Sprintf("Executed %d edit operations", len(context.CurrentPlan.EditOperations)))

	context.Logger.LogProcessStep(fmt.Sprintf("✅ Completed %d edit operations", len(context.CurrentPlan.EditOperations)))

	// Optionally generate minimal smoke tests for changed files
	var changedFiles []string
	for _, op := range context.CurrentPlan.EditOperations {
		changedFiles = append(changedFiles, op.FilePath)
	}
	generateSmokeTestsIfEnabled(changedFiles, context.Config, context.Logger)

	// Non-interactive git integration: auto-commit staged edits when TrackWithGit is enabled
	if context.Config.TrackWithGit {
		proceed, score, serr := automatedSecurityRiskGate(context.Config, context.Logger)
		if serr != nil {
			context.Logger.LogProcessStep(fmt.Sprintf("⚠️ Security risk review failed (skipping auto-commit): %v", serr))
			return nil
		}
		if !proceed {
			context.Logger.LogProcessStep(fmt.Sprintf("❌ Auto-commit blocked by security risk gate (score=%d)", score))
			return nil
		}
		// Use a deterministic, informative commit message
		msg := fmt.Sprintf("ledit: apply %d planned edit(s) for: %s", len(context.CurrentPlan.EditOperations), trimForCommit(context.UserIntent, 80))
		// Respect shell timeout from config
		timeout := context.Config.ShellTimeoutSecs
		if err := git.AddAllAndCommit(msg, timeout); err != nil {
			context.Logger.LogProcessStep(fmt.Sprintf("⚠️ Git auto-commit failed: %v", err))
		} else {
			context.Logger.LogProcessStep("📝 Auto-committed changes via non-interactive git integration")
		}
	}
	return nil
}

// executeEditPlanWithErrorHandling executes edit plan with proper error handling for agent context
func executeEditPlanWithErrorHandling(editPlan *EditPlan, context *AgentContext) (int, error) {
	totalTokens := 0

	// Track changes for context
	var operationResults []string

	for i, operation := range editPlan.EditOperations {
		context.Logger.LogProcessStep(fmt.Sprintf("🔧 Edit %d/%d: %s (%s)", i+1, len(editPlan.EditOperations), operation.Description, operation.FilePath))

		// Create focused instructions for this specific edit
		editInstructions := buildFocusedEditInstructions(operation, context.Logger)
		// Count prompt/input tokens for this edit
		promptTokens := utils.EstimateTokens(editInstructions)
		totalTokens += promptTokens
		context.TokenUsage.CodeGeneration += promptTokens
		context.TokenUsage.CodegenSplit.Prompt += promptTokens

		// Retry logic via helper
		const maxRetries = 2
		compTokens, success, err, opResult := runSingleEditWithRetries(operation, editInstructions, context, maxRetries)
		if compTokens > 0 {
			totalTokens += compTokens
			context.TokenUsage.CodeGeneration += compTokens
			context.TokenUsage.CodegenSplit.Completion += compTokens
		}
		if opResult != "" {
			operationResults = append(operationResults, opResult)
		}
		if !success && err != nil {
			context.Logger.LogProcessStep(fmt.Sprintf("🚫 Edit %d exhausted all retry attempts", i+1))
			context.Errors = append(context.Errors, err.Error())
		}
	}
	// Update agent context with results
	context.ExecutedOperations = append(context.ExecutedOperations, operationResults...)

	// Summarize results to the user
	successCount, _, hasFailures := summarizeEditResults(context, editPlan, operationResults)

	if hasFailures && successCount == 0 {
		return totalTokens, fmt.Errorf("all edit operations failed")
	}
	return totalTokens, nil
}

// shouldUsePartialEdit determines whether to use partial editing or full file editing
// based on the operation characteristics and file size
// THIS IS NOT READY TO USE, NEEDS IMPROVEMENTS
func shouldUsePartialEdit(operation EditOperation, logger *utils.Logger) bool {
	// Check if file exists and get its size
	fileInfo, err := os.Stat(operation.FilePath)
	if err != nil {
		logger.Logf("Cannot stat file %s, using full file edit: %v", operation.FilePath, err)
		return false
	}

	// For very small files (< 1KB), partial editing overhead isn't worth it
	if fileInfo.Size() < 1024 {
		logger.Logf("File %s is small (%d bytes), using full file edit", operation.FilePath, fileInfo.Size())
		return false
	}

	// For very large files (> 50KB), partial editing is more efficient
	if fileInfo.Size() > 50*1024 {
		logger.Logf("File %s is large (%d bytes), using partial edit", operation.FilePath, fileInfo.Size())
		return true
	}

	// For medium files, check if the operation seems focused/targeted
	instructionsLower := strings.ToLower(operation.Instructions)
	description := strings.ToLower(operation.Description)

	// Keywords that suggest focused changes suitable for partial editing
	focusedKeywords := []string{
		"function", "method", "struct", "type", "variable",
		"add", "modify", "update", "change", "fix",
		"import", "constant", "field",
	}

	// Keywords that suggest broad changes requiring full file context
	broadKeywords := []string{
		"refactor", "restructure", "rewrite", "reorganize",
		"architecture", "design pattern", "interface",
		"multiple", "throughout", "entire",
	}

	focusedScore := 0
	broadScore := 0

	for _, keyword := range focusedKeywords {
		if strings.Contains(instructionsLower, keyword) || strings.Contains(description, keyword) {
			focusedScore++
		}
	}

	for _, keyword := range broadKeywords {
		if strings.Contains(instructionsLower, keyword) || strings.Contains(description, keyword) {
			broadScore++
		}
	}

	// If it seems focused, use partial editing
	if focusedScore > broadScore {
		logger.Logf("Operation seems focused (score: %d vs %d), using partial edit", focusedScore, broadScore)
		return true
	}

	// Default to full file editing for ambiguous cases
	logger.Logf("Operation seems broad or ambiguous (score: %d vs %d), using full file edit", focusedScore, broadScore)
	return false
}

// buildFocusedEditInstructions creates targeted instructions for a single file edit
// The orchestration model should provide self-contained instructions with hashtag file references
func buildFocusedEditInstructions(operation EditOperation, logger *utils.Logger) string {
	// Log inputs for debugging
	logger.LogProcessStep("🔧 BUILDING EDIT INSTRUCTIONS:")
	logger.LogProcessStep(fmt.Sprintf("Operation: %s", operation.Description))
	logger.LogProcessStep(fmt.Sprintf("Target File: %s", operation.FilePath))
	logger.LogProcessStep(fmt.Sprintf("Scope Justification: %s", operation.ScopeJustification))

	var instructions strings.Builder

	// Start with the specific operation instructions
	instructions.WriteString(fmt.Sprintf("Task: %s\n\n", operation.Instructions))

	// Add file-specific context
	instructions.WriteString(fmt.Sprintf("Target File: %s\n\n", operation.FilePath))

	// Add scope constraint
	instructions.WriteString(fmt.Sprintf("SCOPE REQUIREMENT: %s\n\n", operation.ScopeJustification))

	// Add focused guidance for fast editing model
	instructions.WriteString(`CRITICAL EDITING CONSTRAINTS:
- Make ONLY the changes specified in the task - NO ADDITIONAL IMPROVEMENTS
- Do NOT add features, optimizations, or enhancements not explicitly requested  
- Do NOT refactor code unless that was the specific request
- Do NOT fix unrelated issues or add "nice to have" changes
- STAY STRICTLY within the scope defined above
- Make TARGETED, PRECISE edits to achieve the specified goal
- Follow existing code patterns and conventions in the file
- Preserve all existing functionality unless explicitly changing it
- Focus only on the requested change, don't make unrelated improvements
- Ensure the change integrates naturally with the existing code

`)

	// The orchestration model should have provided self-contained instructions
	instructions.WriteString("Please implement the requested change efficiently and precisely.\n")

	// Log the full context being sent to the LLM for debugging
	fullInstructions := instructions.String()
	logger.LogProcessStep("📋 FULL INSTRUCTIONS SENT TO EDITING MODEL:")
	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.LogProcessStep(fmt.Sprintf("Target File: %s", operation.FilePath))
	logger.LogProcessStep(fmt.Sprintf("Instructions Size: %d characters", len(fullInstructions)))
	logger.LogProcessStep("Self-contained: Using hashtag file references for context")

	// Check if instructions contain hashtag references
	if strings.Contains(operation.Instructions, "#") {
		logger.LogProcessStep("✅ Instructions contain hashtag file references - context will be loaded automatically")
	} else {
		logger.LogProcessStep("ℹ️  No hashtag file references found - instructions should be self-contained")
	}

	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return fullInstructions
}

// orderEditOperationsHeuristic orders edits so that likely type/interface definitions are edited
// before their dependents, and root-level files before deeper ones. This is a simple heuristic
// to reduce transient build failures during multi-file edits without full graph analysis.
func orderEditOperationsHeuristic(ops []EditOperation) []EditOperation {
	if len(ops) <= 1 {
		return ops
	}
	scored := make([]struct {
		op    EditOperation
		score int
	}, 0, len(ops))
	for _, op := range ops {
		s := 0
		lower := strings.ToLower(op.Description + " " + op.Instructions + " " + op.FilePath)
		// Prefer likely definitions across languages
		if strings.Contains(lower, "type ") || strings.Contains(lower, "interface") || strings.Contains(lower, "struct") ||
			strings.Contains(lower, "class ") || strings.Contains(lower, "enum ") || strings.Contains(lower, "trait ") ||
			strings.Contains(lower, "module ") || strings.Contains(lower, "export ") || strings.Contains(lower, "declare ") {
			s += 6
		}
		if strings.Contains(lower, "define") || strings.Contains(lower, "add new type") || strings.Contains(lower, "public ") || strings.Contains(lower, "fn ") || strings.Contains(lower, "def ") {
			s += 2
		}
		// Prefer root/shallower paths
		depth := strings.Count(op.FilePath, "/")
		s += max(0, 6-depth)
		// Prefer files named models/types/interfaces/utils being edited first
		if strings.Contains(lower, "model") || strings.Contains(lower, "types") || strings.Contains(lower, "interfaces") || strings.Contains(lower, "util") {
			s += 2
		}
		// If operation mentions another file path, assume dependency and boost this one
		for _, other := range ops {
			if other.FilePath != op.FilePath && strings.Contains(strings.ToLower(other.Instructions+" "+other.Description), strings.ToLower(filepath.Base(op.FilePath))) {
				s += 1
			}
		}
		scored = append(scored, struct {
			op    EditOperation
			score int
		}{op: op, score: s})
	}
	// Stable sort by score descending
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}
	out := make([]EditOperation, 0, len(ops))
	for _, it := range scored {
		out = append(out, it.op)
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
