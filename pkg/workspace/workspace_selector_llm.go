package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/git"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

const fileBatchSize = 50

type llmFileSelectionResponse struct {
	FullContextFiles    []string `json:"full_context_files"`
	SummaryContextFiles []string `json:"summary_context_files"`
}

// getFilesForContext uses an LLM to determine which files from the workspace
// are relevant to the user's instructions. It returns two lists: one for files
// to be included with full content, and one for files to be included as summaries.
func getFilesForContext(instructions string, workspace WorkspaceFile, cfg *config.Config, logger *utils.Logger) ([]string, []string, error) {
	var allFiles []string
	for file := range workspace.Files {
		allFiles = append(allFiles, file)
	}

	if len(allFiles) == 0 {
		return nil, nil, nil
	}

	// Get CWD and Git Root for path normalization
	cwd, err := os.Getwd()
	if err != nil {
		logger.Logf("Warning: Could not get current working directory: %v\n", err)
		cwd = "" // Proceed without CWD for path normalization
	}

	gitRoot, err := git.GetGitRootDir()
	if err != nil {
		logger.Logf("Warning: Could not get Git root directory: %v\n", err)
		gitRoot = "" // Proceed without Git root for path normalization
	}

	var wg sync.WaitGroup
	numBatches := (len(allFiles) + fileBatchSize - 1) / fileBatchSize
	resultsChan := make(chan llmFileSelectionResponse, numBatches)
	errChan := make(chan error, numBatches)

	for i := 0; i < len(allFiles); i += fileBatchSize {
		end := i + fileBatchSize
		if end > len(allFiles) {
			end = len(allFiles)
		}
		batch := allFiles[i:end]

		wg.Add(1)
		go func(fileBatch []string) {
			defer wg.Done()

			var batchSummary strings.Builder
			batchSummary.WriteString("Workspace Summary (for this batch):\n")
			for _, filePath := range fileBatch {
				if fileInfo, ok := workspace.Files[filePath]; ok {
					batchSummary.WriteString(fmt.Sprintf("\nFile: %s\n", filePath))
					batchSummary.WriteString(fmt.Sprintf("  Summary: %s\n", fileInfo.Summary))
					if fileInfo.Exports != "" {
						batchSummary.WriteString(fmt.Sprintf("  Exports: %s\n", fileInfo.Exports))
					}
				}
			}

			prompt := fmt.Sprintf(`Based on the following user instructions and the workspace summary, identify which files are relevant.
For each relevant file, decide if its FULL content is necessary or if just its summary is sufficient.

User Instructions:
%s

%s

Respond with a JSON object containing two keys:
1. "full_context_files": A list of file paths that require their full content to be included for implementing the user's request.
2. "summary_context_files": A list of file paths for which only the summary is sufficient to understand their role and context.

- Use "full_context_files" for files that will likely need to be modified or contain core logic relevant to the request.
- Use "summary_context_files" for files that provide helpful context but are not central to the task.
- If a file is not relevant, do not include it in either list.

Only include files from the provided "Workspace Summary". Do not include files not in the list.
If no files are relevant, return an empty JSON object or JSON with empty lists.
Your response MUST be only the raw JSON, without any surrounding text or code fences.`, instructions, batchSummary.String())

			messages := []prompts.Message{
				{
					Role:    "system",
					Content: "You are an expert code assistant. Your task is to select relevant files for a programming task based on user instructions and a file summary. You must respond with a valid JSON object containing 'full_context_files' and 'summary_context_files' keys.",
				},
				{
					Role:    "user",
					Content: prompt,
				},
			}

			_, response, err := llm.GetLLMResponse(cfg.WorkspaceModel, messages, "workspace_selector", cfg, 2*time.Minute)
			if err != nil {
				errChan <- fmt.Errorf("LLM request for file selection batch failed: %w", err)
				return
			}

			// Enhanced response cleaning
			response = cleanLLMResponse(response)

			var selection llmFileSelectionResponse
			if response == "" {
				resultsChan <- llmFileSelectionResponse{}
				return
			}

			if err := json.Unmarshal([]byte(response), &selection); err != nil {
				errChan <- fmt.Errorf("could not unmarshal file selection response: %w. Response was: %s", err, response)
				return
			}

			resultsChan <- selection
		}(batch)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
		close(errChan)
	}()

	fullContextFiles := make(map[string]bool)
	summaryContextFiles := make(map[string]bool)
	var lastErr error

	for err := range errChan {
		if err != nil {
			// The first error is usually the most informative.
			if lastErr == nil {
				lastErr = err
			}
		}
	}

	if lastErr != nil {
		return nil, nil, lastErr
	}

	for result := range resultsChan {
		// Process full context files
		for _, file := range result.FullContextFiles {
			normalizedFile, corrected := normalizeLLMPath(file, cwd, gitRoot, workspace.Files, logger)
			if corrected {
				logger.LogUserInteraction(fmt.Sprintf("Using corrected path '%s' for full context (original: '%s').", normalizedFile, file))
			}
			if _, ok := workspace.Files[normalizedFile]; ok {
				fullContextFiles[normalizedFile] = true
			} else {
				logger.LogUserInteraction(fmt.Sprintf("Warning: LLM suggested file '%s' (normalized to '%s') for full context, but it does not exist in the workspace. Skipping.", file, normalizedFile))
			}
		}
		// Process summary context files
		for _, file := range result.SummaryContextFiles {
			normalizedFile, corrected := normalizeLLMPath(file, cwd, gitRoot, workspace.Files, logger)
			if corrected {
				logger.LogUserInteraction(fmt.Sprintf("Using corrected path '%s' for summary context (original: '%s').", normalizedFile, file))
			}
			if _, ok := workspace.Files[normalizedFile]; ok {
				if _, existsInFull := fullContextFiles[normalizedFile]; !existsInFull { // Ensure it's not already in full context
					summaryContextFiles[normalizedFile] = true
				}
			} else {
				logger.LogUserInteraction(fmt.Sprintf("Warning: LLM suggested file '%s' (normalized to '%s') for summary context, but it does not exist in the workspace. Skipping.", file, normalizedFile))
			}
		}
	}

	var fullFiles, summaryFiles []string
	for file := range fullContextFiles {
		fullFiles = append(fullFiles, file)
	}
	for file := range summaryContextFiles {
		summaryFiles = append(summaryFiles, file)
	}

	return fullFiles, summaryFiles, nil
}

// cleanLLMResponse handles various response formats from the LLM
func cleanLLMResponse(response string) string {
	// Remove markdown code blocks if present
	if strings.Contains(response, "```") {
		re := regexp.MustCompile("(?s)```(json)?\\n?(.*?)\\n?```")
		matches := re.FindStringSubmatch(response)
		if len(matches) > 1 {
			response = matches[len(matches)-1]
		}
	}

	// Remove any leading/trailing whitespace or quotes
	response = strings.TrimSpace(response)
	response = strings.Trim(response, `"`)

	return response
}