package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// MicroEditTool performs small, targeted edits
type MicroEditTool struct {
	*BaseTool
	maxTotal int
	maxHunk  int
	maxHunks int
}

// NewMicroEditTool creates a new micro edit tool
func NewMicroEditTool(maxTotal, maxHunk, maxHunks int) *MicroEditTool {
	base := NewBaseTool(
		"micro_edit",
		"Attempts a very small targeted edit using patch-based editing",
		"edit",
		[]string{"read", "write"},
		5*time.Second,
	)

	return &MicroEditTool{
		BaseTool: base,
		maxTotal: maxTotal,
		maxHunk:  maxHunk,
		maxHunks: maxHunks,
	}
}

// Execute runs the micro edit tool
func (t *MicroEditTool) Execute(ctx context.Context, params ToolParameters) (*ToolResult, error) {
	if params.Context == nil {
		return &ToolResult{
			Success: false,
			Errors:  []string{"agent context is required"},
		}, nil
	}

	startTime := time.Now()

	// Choose target file and instruction
	var targetFile string
	var instructions string

	if params.Context.CurrentPlan != nil && len(params.Context.CurrentPlan.EditOperations) > 0 {
		op := params.Context.CurrentPlan.EditOperations[0]
		targetFile = op.FilePath
		if op.Instructions != "" {
			instructions = op.Instructions
		} else {
			instructions = fmt.Sprintf("Make the smallest possible change to accomplish: %s. Do not refactor; limit change to a few lines.", params.Context.UserIntent)
		}
	} else if params.Context.IntentAnalysis != nil && len(params.Context.IntentAnalysis.EstimatedFiles) > 0 {
		targetFile = params.Context.IntentAnalysis.EstimatedFiles[0]
		instructions = fmt.Sprintf("Make the smallest possible change in %s to accomplish: %s. Do not refactor; limit change to a few lines.", targetFile, params.Context.UserIntent)
	} else {
		return &ToolResult{
			Success:       false,
			Errors:        []string{"no target file available"},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	params.Logger.LogProcessStep(fmt.Sprintf("micro_edit: attempting minimal change in %s", targetFile))

	// Use patch-based editing instead of partial editing
	// Create messages for patch-based editing
	messages := []prompts.Message{
		{Role: "system", Content: prompts.GetBaseCodePatchSystemMessage()},
		{Role: "user", Content: fmt.Sprintf("Target file: %s\n\nInstructions: %s", targetFile, instructions)},
	}

	// Get patch response from LLM
	response, _, err := llm.GetLLMResponse(params.Context.Config.GetLLMConfig().EditingModel, messages, targetFile, params.Context.Config, 30*time.Second)
	if err != nil {
		return &ToolResult{
			Success:       false,
			Errors:        []string{fmt.Sprintf("LLM call failed: %v", err)},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	// Parse patches from response
	patches, err := parser.GetUpdatedCodeFromPatchResponse(response)
	if err != nil {
		return &ToolResult{
			Success:       false,
			Errors:        []string{fmt.Sprintf("failed to parse patches: %v", err)},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	if len(patches) == 0 {
		return &ToolResult{
			Success:       false,
			Errors:        []string{"no patches generated"},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	// Apply the patch
	filePatch, exists := patches[targetFile]
	if !exists {
		return &ToolResult{
			Success:       false,
			Errors:        []string{fmt.Sprintf("no patch found for target file %s", targetFile)},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	// Validate patch size limits
	if exceeded, reason := t.enforceMicroEditLimits(filePatch, t.maxTotal, t.maxHunk, t.maxHunks); exceeded {
		return &ToolResult{
			Success:       false,
			Errors:        []string{fmt.Sprintf("patch exceeds limits: %s", reason)},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	err = parser.ApplyPatchToFile(filePatch, targetFile)
	if err != nil {
		return &ToolResult{
			Success:       false,
			Errors:        []string{fmt.Sprintf("failed to apply patch: %v", err)},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	// Token accounting approximation
	completionTokens := utils.EstimateTokens(response)
	params.Context.TokenUsage.CodeGeneration += completionTokens
	params.Context.TokenUsage.CodegenSplit.Completion += completionTokens
	params.Context.ExecutedOperations = append(params.Context.ExecutedOperations, fmt.Sprintf("micro_edit applied to %s", targetFile))
	params.Logger.LogProcessStep("micro_edit: applied")

	return &ToolResult{
		Success: true,
		Output:  fmt.Sprintf("Successfully applied micro edit to %s", targetFile),
		Data: map[string]interface{}{
			"target_file":  targetFile,
			"instructions": instructions,
			"patch":        filePatch,
		},
		Files:         []string{targetFile},
		ExecutionTime: time.Since(startTime),
		TokenUsage:    &AgentTokenUsage{CodeGeneration: completionTokens},
	}, nil
}

// CanExecute checks if the tool can execute
func (t *MicroEditTool) CanExecute(ctx context.Context, params ToolParameters) bool {
	if params.Context == nil || params.Logger == nil {
		return false
	}

	// Check if we have a target file
	if params.Context.CurrentPlan != nil && len(params.Context.CurrentPlan.EditOperations) > 0 {
		return true
	}
	if params.Context.IntentAnalysis != nil && len(params.Context.IntentAnalysis.EstimatedFiles) > 0 {
		return true
	}

	return false
}

// enforceMicroEditLimits inspects a patch to approximate change size
func (t *MicroEditTool) enforceMicroEditLimits(patch *parser.Patch, maxTotal int, maxHunk int, maxHunks int) (bool, string) {
	if patch == nil {
		return false, "No patch to validate"
	}

	totalChanged := 0
	hunks := 0

	for _, hunk := range patch.Hunks {
		hunks++
		hunkChanges := hunk.OldLines + hunk.NewLines

		// Count the net changes in this hunk
		if hunkChanges > maxHunk {
			return true, fmt.Sprintf("Exceeded per-hunk limit: %d > %d", hunkChanges, maxHunk)
		}

		totalChanged += hunkChanges

		if totalChanged > maxTotal {
			return true, fmt.Sprintf("Exceeded total changed lines limit: %d > %d", totalChanged, maxTotal)
		}

		if hunks > maxHunks {
			return true, fmt.Sprintf("Exceeded hunk count limit: %d > %d", hunks, maxHunks)
		}
	}

	return false, fmt.Sprintf("Within limits: total=%d, hunks=%d", totalChanged, hunks)
}
