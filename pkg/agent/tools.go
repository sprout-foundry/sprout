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
	diff, err := editor.ProcessPartialEdit(targetFile, instructions, context.Config, context.Logger)
	if err != nil {
		context.Logger.LogProcessStep(fmt.Sprintf("micro_edit failed: %v", err))
		return nil
	}
	// Token accounting approximation from diff size
	completionTokens := utils.EstimateTokens(diff)
	context.TokenUsage.CodeGeneration += completionTokens
	context.TokenUsage.CodegenSplit.Completion += completionTokens
	context.ExecutedOperations = append(context.ExecutedOperations, fmt.Sprintf("micro_edit applied to %s", targetFile))
	context.Logger.LogProcessStep("micro_edit: change applied")
	return nil
}
