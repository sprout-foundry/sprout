package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// isToolAllowed checks if a tool is allowed based on the configuration.
func isToolAllowed(context *AgentContext, toolName string) bool {
	// With simplified configuration, allow all tools used by the agent pipeline
	return true
}

// executeWorkspaceInfo gathers and logs lightweight workspace information
func executeWorkspaceInfo(context *AgentContext) error {
	if !isToolAllowed(context, "workspace_info") {
		context.Logger.LogProcessStep("workspace_info: tool not allowed by configuration")
		return nil
	}

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
	if !isToolAllowed(context, "list_files") {
		context.Logger.LogProcessStep("list_files: tool not allowed by configuration")
		return nil
	}

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
	if !isToolAllowed(context, "grep_search") {
		context.Logger.LogProcessStep("grep_search: tool not allowed by configuration")
		return nil
	}

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

// executeMicroEdit attempts a very small targeted edit using patch-based editing
func executeMicroEdit(context *AgentContext) error {
	if !isToolAllowed(context, "micro_edit") {
		context.Logger.LogProcessStep("micro_edit: tool not allowed by configuration")
		return nil
	}

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

	// Use patch-based editing instead of partial editing
	// Create messages for patch-based editing
	messages := []prompts.Message{
		{Role: "system", Content: prompts.GetBaseCodePatchSystemMessage()},
		{Role: "user", Content: fmt.Sprintf("Target file: %s\n\nInstructions: %s", targetFile, instructions)},
	}

	// Get patch response from LLM
	response, _, err := llm.GetLLMResponse(context.Config.EditingModel, messages, targetFile, context.Config, 30*time.Second)
	if err != nil {
		context.Logger.LogProcessStep(fmt.Sprintf("micro_edit failed: %v", err))
		return nil
	}

	// Parse patches from response
	patches, err := parser.GetUpdatedCodeFromPatchResponse(response)
	if err != nil {
		context.Logger.LogProcessStep(fmt.Sprintf("Failed to parse patches: %v", err))
		return nil
	}

	if len(patches) == 0 {
		context.Logger.LogProcessStep("No patches generated")
		return nil
	}

	// Apply the patch
	filePatch, exists := patches[targetFile]
	if !exists {
		context.Logger.LogProcessStep(fmt.Sprintf("No patch found for target file %s", targetFile))
		return nil
	}

	err = parser.ApplyPatchToFile(filePatch, targetFile)
	if err != nil {
		context.Logger.LogProcessStep(fmt.Sprintf("Failed to apply patch: %v", err))
		return nil
	}

	// Token accounting approximation
	completionTokens := utils.EstimateTokens(response)
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
