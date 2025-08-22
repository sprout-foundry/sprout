package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/index"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// findRelevantFilesRobust uses embeddings and fallback strategies to find relevant files
func findRelevantFilesRobust(userIntent string, cfg *config.Config, logger *utils.Logger) []string {
	// Try embeddings first
	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		if os.IsNotExist(err) {
			logger.Logf("No workspace file found for embeddings, will use fallback methods")
		} else {
			logger.LogError(fmt.Errorf("failed to load workspace for file discovery: %w", err))
		}
	} else {
		fullFiles, _, embErr := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFile, cfg, logger)
		if embErr != nil {
			logger.LogError(fmt.Errorf("embedding search failed: %w", embErr))
		} else {
			// Rerank using symbol index overlap
			root, _ := os.Getwd()
			symHits := map[string]int{}
			if idx, err := index.BuildSymbols(root); err == nil && idx != nil {
				tokens := strings.Fields(userIntent)
				for _, f := range index.SearchSymbols(idx, tokens) {
					symHits[filepath.ToSlash(f)]++
				}
			}
			if len(fullFiles) > 0 {
				type scored struct {
					file  string
					score int
				}
				var res []scored
				for _, f := range fullFiles {
					s := symHits[filepath.ToSlash(f)]
					res = append(res, scored{file: f, score: s})
				}
				// stable sort by score desc
				for i := 0; i < len(res); i++ {
					for j := i + 1; j < len(res); j++ {
						if res[j].score > res[i].score {
							res[i], res[j] = res[j], res[i]
						}
					}
				}
				out := make([]string, 0, len(res))
				for _, r := range res {
					out = append(out, r.file)
				}
				if len(out) > 0 {
					logger.Logf("Embeddings+symbols selected %d files (reranked)", len(out))
					return out
				}
			}
		}
	}

	// If embeddings failed, try symbol index
	root, _ := os.Getwd()
	if idx, err := index.BuildSymbols(root); err == nil && idx != nil {
		tokens := strings.Fields(userIntent)
		if sym := index.SearchSymbols(idx, tokens); len(sym) > 0 {
			logger.Logf("Symbol index found %d candidate files", len(sym))
			return sym
		}
	}

	// If embeddings and symbols failed, try content-based search
	logger.Logf("Embeddings found no files, trying content search...")
	contentFiles := findRelevantFilesByContent(userIntent, logger)
	if len(contentFiles) > 0 {
		return contentFiles
	}

	// If all else fails, use shell commands to find files
	logger.Logf("Content search found no files, trying shell-based discovery...")

	// We need workspace info for shell commands, but keep it lightweight
	workspaceInfo := &WorkspaceInfo{
		ProjectType:   "other", // Default fallback
		RootFiles:     []string{},
		AllFiles:      []string{},
		FilesByDir:    map[string][]string{},
		RelevantFiles: map[string]string{},
	}

	shellFiles := findFilesUsingShellCommands(userIntent, workspaceInfo, logger)
	if len(shellFiles) > 0 {
		return shellFiles
	}

	// Absolute fallback - return empty slice, let caller handle
	logger.Logf("All file discovery methods failed")
	return []string{}
}

// rewordPromptForBetterSearch uses workspace model to reword the user prompt for better file discovery
func rewordPromptForBetterSearch(userIntent string, workspaceInfo *WorkspaceInfo, cfg *config.Config, logger *utils.Logger) (string, int, error) {
	logger.Logf("Using workspace model to reword prompt for better file discovery...")

	prompt := fmt.Sprintf(`You are a %s codebase expert. The user wants to: "%s"

WORKSPACE CONTEXT:
Project Type: %s
Available files include: %v

The initial file search found very few relevant files. Rewrite the user's intent using technical terms and patterns that would be found in a %s codebase to help find the right files.

Focus on:
- Function names that might exist
- File naming patterns in %s projects
- Technical terms specific to this domain
- Package/module names that might be relevant

Respond with ONLY the reworded search query, no explanation:`,
		workspaceInfo.ProjectType, userIntent, workspaceInfo.ProjectType,
		workspaceInfo.AllFiles[:min(10, len(workspaceInfo.AllFiles))], // Show sample files
		workspaceInfo.ProjectType, workspaceInfo.ProjectType)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at understanding codebases and creating effective search queries."},
		{Role: "user", Content: prompt},
	}

	response, usage, err := llm.GetLLMResponse(cfg.WorkspaceModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		logger.LogError(fmt.Errorf("workspace model failed to reword prompt: %w", err))
		return userIntent, 0, err // Return original on failure
	}

	reworded := strings.TrimSpace(response)
	if reworded == "" {
		return userIntent, 0, fmt.Errorf("empty reworded response")
	}

	// Compute tokens used (prefer actual usage if available)
	tokensUsed := 0
	if usage != nil {
		tokensUsed = usage.TotalTokens
	} else {
		// Fallback estimate
		tokensUsed = utils.EstimateTokens(prompt) + utils.EstimateTokens(response)
	}
	logger.Logf("Intent rewording tokens used: total=%d", tokensUsed)

	return reworded, tokensUsed, nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// File discovery helper functions are defined in workspace_discovery.go
