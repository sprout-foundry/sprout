package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// analyzeIntentWithMinimalContext analyzes user intent with workspace context
func analyzeIntentWithMinimalContext(userIntent string, cfg *config.Config, logger *utils.Logger) (*IntentAnalysis, int, error) {
	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: analyzeIntentWithMinimalContext started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		if os.IsNotExist(err) {
			logger.Logf("No workspace file found, creating and populating workspace...")
			if err := os.MkdirAll(".ledit", os.ModePerm); err != nil {
				logger.LogError(fmt.Errorf("failed to create .ledit directory: %w", err))
				return nil, 0, fmt.Errorf("failed to create workspace directory: %w", err)
			}
			_ = workspace.GetWorkspaceContext("", cfg)
			workspaceFile, err = workspace.LoadWorkspaceFile()
			if err != nil {
				logger.LogError(fmt.Errorf("failed to load workspace after creation: %w", err))
				return nil, 0, fmt.Errorf("failed to load workspace after creation: %w", err)
			}
		} else {
			logger.LogError(fmt.Errorf("failed to load workspace file: %w", err))
			return nil, 0, fmt.Errorf("failed to load workspace: %w", err)
		}
	}

	workspaceAnalysis, err := buildWorkspaceStructure(logger)
	if err != nil {
		logger.Logf("Warning: Could not build workspace analysis: %v", err)
		workspaceAnalysis = &WorkspaceInfo{ProjectType: "other", AllFiles: []string{}}
	}

	fullContextFiles, summaryContextFiles, err := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFile, cfg, logger)
	if err != nil {
		logger.LogError(fmt.Errorf("embedding search failed: %w", err))
		fullContextFiles, summaryContextFiles = []string{}, []string{}
	}
	relevantFiles := append(fullContextFiles, summaryContextFiles...)

	rewordTokensUsed := 0
	if len(relevantFiles) < 3 {
		rewordedIntent, rewordTokens, rewordErr := rewordPromptForBetterSearch(userIntent, workspaceAnalysis, cfg, logger)
		if rewordErr == nil && rewordedIntent != userIntent {
			rewordTokensUsed = rewordTokens
			fullContextFiles2, summaryContextFiles2, err2 := workspace.GetFilesForContextUsingEmbeddings(rewordedIntent, workspaceFile, cfg, logger)
			if err2 == nil && len(fullContextFiles2)+len(summaryContextFiles2) > len(relevantFiles) {
				fullContextFiles = fullContextFiles2
				summaryContextFiles = summaryContextFiles2
				relevantFiles = append(fullContextFiles, summaryContextFiles...)
			}
		}
	}

	if len(relevantFiles) < 2 {
		shellFoundFiles := findFilesUsingShellCommands(userIntent, workspaceAnalysis, logger)
		if len(shellFoundFiles) > 0 {
			relevantFiles = append(relevantFiles, shellFoundFiles...)
		}
	}

	if len(relevantFiles) == 0 {
		relevantFiles = findRelevantFilesByContent(userIntent, logger)
	}

	if len(relevantFiles) == 0 {
		candidateFiles := getRecentlyModifiedSourceFiles(workspaceAnalysis, logger)
		if len(candidateFiles) == 0 {
			candidateFiles = getCommonEntryPointFiles(workspaceAnalysis.ProjectType, logger)
		}
		for _, file := range candidateFiles {
			if _, err := os.Stat(file); err == nil {
				relevantFiles = append(relevantFiles, file)
			}
		}
	}

	prompt := BuildIntentAnalysisPrompt(userIntent, workspaceAnalysis.ProjectType, relevantFiles)
	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at analyzing programming tasks. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}
	response, _, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 60*time.Second)
	if err != nil {
		logger.LogError(fmt.Errorf("orchestration model failed to analyze intent: %w", err))
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: analyzeIntentWithMinimalContext completed (LLM error, fallback). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  []string{},
			RequiresContext: true,
		}, 0, nil
	}

	promptTokens := llm.GetConversationTokens([]struct{ Role, Content string }{
		{Role: messages[0].Role, Content: messages[0].Content.(string)},
		{Role: messages[1].Role, Content: messages[1].Content.(string)},
	})
	responseTokens := llm.EstimateTokens(response)
	totalTokens := promptTokens + responseTokens + rewordTokensUsed

	cleanedResponse, err := utils.ExtractJSONFromLLMResponse(response)
	if err != nil {
		logger.LogError(fmt.Errorf("CRITICAL: Failed to extract JSON from intent analysis response: %w\nRaw response: %s", err, response))
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: analyzeIntentWithMinimalContext completed (JSON extraction error, fallback). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  []string{},
			RequiresContext: true,
		}, totalTokens, nil
	}

	var analysis IntentAnalysis
	if err := json.Unmarshal([]byte(cleanedResponse), &analysis); err != nil {
		logger.LogError(fmt.Errorf("CRITICAL: Failed to parse intent analysis JSON from LLM: %w\nCleaned JSON: %s\nRaw response: %s", err, cleanedResponse, response))
		return nil, 0, fmt.Errorf("unrecoverable JSON parsing error in intent analysis: %w\nCleaned JSON: %s\nRaw Response: %s", err, cleanedResponse, response)
	}

	if len(analysis.EstimatedFiles) == 0 {
		workspaceFileData, embErr := workspace.LoadWorkspaceFile()
		if embErr == nil {
			fullContextFiles, summaryContextFiles, embErr := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFileData, cfg, logger)
			embeddingFiles := append(fullContextFiles, summaryContextFiles...)
			if embErr != nil || len(embeddingFiles) == 0 {
				analysis.EstimatedFiles = findRelevantFilesByContent(userIntent, logger)
			} else {
				analysis.EstimatedFiles = embeddingFiles
			}
		} else {
			analysis.EstimatedFiles = findRelevantFilesByContent(userIntent, logger)
		}
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: analyzeIntentWithMinimalContext completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
	return &analysis, totalTokens, nil
}
