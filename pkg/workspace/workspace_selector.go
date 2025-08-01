package workspace

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
)

const fileBatchSize = 50

type llmFileSelectionResponse struct {
	FullContextFiles    []string `json:"full_context_files"`
	SummaryContextFiles []string `json:"summary_context_files"`
}

// getFilesForContext uses an LLM to determine which files from the workspace
// are relevant to the user's instructions. It returns two lists: one for files
// to be included with full content, and one for files to be included as summaries.
func getFilesForContext(instructions string, workspace WorkspaceFile, cfg *config.Config) ([]string, []string, error) {
	var allFiles []string
	for file := range workspace.Files {
		allFiles = append(allFiles, file)
	}

	if len(allFiles) == 0 {
		return nil, nil, nil
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

			_, response, err := llm.GetLLMResponse(cfg.WorkspaceModel, messages, "workspace_selector", cfg, 2*time.Minute, false)
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
		for _, file := range result.FullContextFiles {
			if _, ok := workspace.Files[file]; ok {
				fullContextFiles[file] = true
			}
		}
		for _, file := range result.SummaryContextFiles {
			if _, ok := workspace.Files[file]; ok {
				if _, exists := fullContextFiles[file]; !exists {
					summaryContextFiles[file] = true
				}
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