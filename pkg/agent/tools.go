package agent

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/utils"
)

// executeWorkspaceInfo gathers and logs lightweight workspace information
func executeWorkspaceInfo(context *AgentContext) error {
	info, err := buildWorkspaceStructure(context.Logger)
	if err != nil {
		context.Logger.Logf("Workspace info unavailable: %v", err)
		return nil
	}
	summary := fmt.Sprintf("Workspace: type=%s files=%d dirs=%d", info.ProjectType, len(info.AllFiles), len(info.FilesByDir))
	context.ExecutedOperations = append(context.ExecutedOperations, summary)
	context.Logger.LogProcessStep(summary)
	return nil
}

// executeListFiles lists a small set of workspace files for quick orientation
func executeListFiles(context *AgentContext, limit int) error {
	info, err := buildWorkspaceStructure(context.Logger)
	if err != nil {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	files := info.AllFiles
	if len(files) > limit {
		files = files[:limit]
	}
	summary := fmt.Sprintf("Files (%d): %s", len(files), strings.Join(files, ", "))
	context.ExecutedOperations = append(context.ExecutedOperations, summary)
	context.Logger.LogProcessStep(summary)
	return nil
}

// executeGrepSearch performs a quick content search for provided terms
func executeGrepSearch(context *AgentContext, terms []string) error {
	if len(terms) == 0 {
		return nil
	}
	joined := strings.Join(terms, " ")
	found := findFilesUsingShellCommands(joined, &WorkspaceInfo{ProjectType: "other"}, context.Logger)
	if len(found) == 0 {
		context.Logger.LogProcessStep("grep_search: no matches found")
		return nil
	}
	summary := fmt.Sprintf("grep_search: %d files: %s", len(found), strings.Join(found, ", "))
	context.ExecutedOperations = append(context.ExecutedOperations, summary)
	context.Logger.LogProcessStep(summary)
	return nil
}

// executeMicroEdit attempts a very small targeted edit using partial-edit flow
func executeMicroEdit(context *AgentContext) error {
	// Choose a target file and instruction
	var targetFile string
	var instructions string

	if context.CurrentPlan != nil && len(context.CurrentPlan.EditOperations) > 0 {
		op := context.CurrentPlan.EditOperations[0]
		targetFile = op.FilePath
		if op.Instructions != "" {
			instructions = op.Instructions
		} else {
			instructions = fmt.Sprintf("Make the smallest possible change to accomplish: %s. Do not refactor; limit change to a few lines.", context.UserIntent)
		}
	} else if context.IntentAnalysis != nil && len(context.IntentAnalysis.EstimatedFiles) > 0 {
		targetFile = context.IntentAnalysis.EstimatedFiles[0]
		instructions = fmt.Sprintf("Make the smallest possible change in %s to accomplish: %s. Do not refactor; limit change to a few lines.", targetFile, context.UserIntent)
	} else {
		context.Logger.LogProcessStep("micro_edit: no target file available")
		return nil
	}

	context.Logger.LogProcessStep(fmt.Sprintf("micro_edit: attempting minimal change in %s", targetFile))
	// This tool explicitly performs a micro/partial edit; allowed path
	// First, perform the partial edit to get a candidate diff
	diff, err := editor.ProcessPartialEdit(targetFile, instructions, context.Config, context.Logger)
	if err != nil {
		context.Logger.LogProcessStep(fmt.Sprintf("micro_edit failed: %v", err))
		return nil
	}
	// Enforce micro edit limits (language-agnostic):
	// - default: <=50 total changed lines
	// - per-hunk <=25 lines
	// - <=2 hunks
	// If limits exceeded, abort and suggest escalation
	if exceeded, summary := enforceMicroEditLimits(diff, 50, 25, 2); exceeded {
		context.Logger.LogProcessStep("micro_edit aborted: diff exceeded safe limits. Escalating to edit_file_section.")
		context.Logger.LogProcessStep(summary)
		// Escalate deterministically with targeted instructions
		escalated := fmt.Sprintf("Targeted change (section-level) for %s: %s. Keep the diff focused; avoid unrelated edits.", targetFile, context.UserIntent)
		diff2, err := editor.ProcessPartialEdit(targetFile, escalated, context.Config, context.Logger)
		if err != nil {
			context.Logger.LogProcessStep(fmt.Sprintf("edit_file_section escalation failed: %v", err))
			return nil
		}
		// Accept escalation result without re-checking micro limits
		completionTokens := utils.EstimateTokens(diff2)
		context.TokenUsage.CodeGeneration += completionTokens
		context.TokenUsage.CodegenSplit.Completion += completionTokens
		context.ExecutedOperations = append(context.ExecutedOperations, fmt.Sprintf("edit_file_section applied to %s", targetFile))
		context.Logger.LogProcessStep("edit_file_section: change applied after micro_edit exceeded limits")
		return nil
	}
	// Token accounting approximation from diff size
	completionTokens := utils.EstimateTokens(diff)
	context.TokenUsage.CodeGeneration += completionTokens
	context.TokenUsage.CodegenSplit.Completion += completionTokens
	context.ExecutedOperations = append(context.ExecutedOperations, fmt.Sprintf("micro_edit applied to %s", targetFile))
	context.Logger.LogProcessStep("micro_edit: applied")
	return nil
}

// enforceMicroEditLimits inspects a unified diff-like string to approximate change size.
// It returns true if limits are exceeded and a brief summary.
func enforceMicroEditLimits(diff string, maxTotal int, maxHunk int, maxHunks int) (bool, string) {
	lines := strings.Split(diff, "\n")
	totalChanged := 0
	currentHunk := 0
	hunks := 0
	for _, l := range lines {
		// Count +/- as changes; ignore context lines
		if strings.HasPrefix(l, "+ ") || strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
			totalChanged++
			currentHunk++
			if currentHunk > maxHunk {
				return true, fmt.Sprintf("Exceeded per-hunk limit: %d > %d", currentHunk, maxHunk)
			}
		} else {
			// End of a change block when we hit a non-change line while in a hunk
			if currentHunk > 0 {
				hunks++
				if hunks > maxHunks {
					return true, fmt.Sprintf("Exceeded hunk count limit: %d > %d", hunks, maxHunks)
				}
				currentHunk = 0
			}
		}
		if totalChanged > maxTotal {
			return true, fmt.Sprintf("Exceeded total changed lines limit: %d > %d", totalChanged, maxTotal)
		}
	}
	// Close final hunk if diff ends in changes
	if currentHunk > 0 {
		hunks++
		if hunks > maxHunks {
			return true, fmt.Sprintf("Exceeded hunk count limit at end: %d > %d", hunks, maxHunks)
		}
	}
	return false, fmt.Sprintf("Within limits: total=%d, hunks=%d", totalChanged, hunks)
}
