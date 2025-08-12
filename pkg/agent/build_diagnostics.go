package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// getProjectFileTree returns a representation of the project file structure
func getProjectFileTree() (string, error) {
	var tree strings.Builder
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".") && path != "." {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		skipDirs := []string{"vendor", "node_modules", "target", "build", "dist"}
		for _, skipDir := range skipDirs {
			if strings.Contains(path, skipDir) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		depth := strings.Count(path, string(os.PathSeparator))
		indent := strings.Repeat("  ", depth)
		if info.IsDir() {
			tree.WriteString(fmt.Sprintf("%s%s/\n", indent, filepath.Base(path)))
		} else {
			tree.WriteString(fmt.Sprintf("%s%s\n", indent, filepath.Base(path)))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return tree.String(), nil
}

// analyzeBuildErrorsAndCreateFix uses LLM to understand build errors and create targeted fixes
func analyzeBuildErrorsAndCreateFix(validationResults []string, originalIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (string, int, error) {
	var errorMessages []string
	for _, result := range validationResults {
		if strings.HasPrefix(result, "❌") {
			errorMessages = append(errorMessages, strings.TrimPrefix(result, "❌ "))
		}
	}
	if len(errorMessages) == 0 {
		return "", 0, fmt.Errorf("no error messages found in validation results")
	}

	prompt := fmt.Sprintf(`You are an expert Go developer helping to fix build errors and improve code quality.

ORIGINAL TASK: %s
TASK CATEGORY: %s

BUILD/VALIDATION ERRORS:
%s

PROJECT CONTEXT:
- This project has detected dependencies and module structure
- All import paths must use proper module paths
- Key APIs available:
  * Logger: Available logging functionality for debugging
  * Filesystem: Use appropriate file operations for this project type
  * Follow existing patterns and conventions in the codebase

ANALYSIS INSTRUCTIONS:
1. Primary Fix: Analyze the build/validation errors and determine minimal fixes needed
2. Error Classification: Are these errors related to the recent changes or pre-existing issues?
3. Test Assessment: Based on the original task, determine if tests are needed
4. Code Quality: Identify obvious quality improvements that align with the original task

Create a detailed fix prompt:`, originalIntent, intentAnalysis.Category, strings.Join(errorMessages, "\n"))

	messages := []prompts.Message{{Role: "system", Content: "You are an expert Go developer who excels at diagnosing and fixing build errors. Respond with a clear, actionable fix prompt."}, {Role: "user", Content: prompt}}
	response, _, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get orchestration model analysis of build errors: %w", err)
	}
	tokens := utils.EstimateTokens(prompt + response)
	logger.Logf("LLM build error analysis: %s", response)
	return response, tokens, nil
}
