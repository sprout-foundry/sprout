package workspace

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/embedding"
	"github.com/alantheprice/ledit/pkg/utils"
)

const (
	embeddingDBPath = "./.ledit/embeddings.json"
	topKFiles       = 10 // Number of top files to return
)

// GetFilesForContextUsingEmbeddings uses vector embeddings to determine which files from the workspace
// are relevant to the user's instructions. It returns two lists: one for files
// to be included with full content, and one for files to be included as summaries.
func GetFilesForContextUsingEmbeddings(instructions string, workspace WorkspaceFile, cfg *config.Config, logger *utils.Logger) ([]string, []string, error) {
	db := embedding.NewVectorDB()

	// GenerateWorkspaceEmbeddings now handles loading, generating, and saving embeddings
	logger.LogProcessStep("--- Generating/Updating embeddings for workspace files ---")
	if err := embedding.GenerateWorkspaceEmbeddings(workspace, db, cfg); err != nil {
		return nil, nil, fmt.Errorf("failed to generate/update workspace embeddings: %w", err)
	}

	// Search for relevant files using embeddings
	logger.LogProcessStep("--- Searching for relevant files using embeddings ---")
	relevantEmbeddings, scores, err := embedding.SearchRelevantFiles(instructions, db, topKFiles, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to search for relevant files: %w", err)
	}

	// Separate into full context and summary context files
	// For now, we'll put the top 50% in full context and the rest in summary context
	// This is a simple heuristic that could be improved
	var fullContextFiles []string
	var summaryContextFiles []string

	halfPoint := len(relevantEmbeddings) / 2
	if halfPoint == 0 && len(relevantEmbeddings) > 0 {
		halfPoint = 1
	}

	for i, emb := range relevantEmbeddings {
		if i < halfPoint {
			fullContextFiles = append(fullContextFiles, emb.Path)
			logger.Logf("Selected for full context (%.4f): %s\n", scores[i], emb.Path)
		} else {
			summaryContextFiles = append(summaryContextFiles, emb.Path)
			logger.Logf("Selected for summary context (%.4f): %s\n", scores[i], emb.Path)
		}
	}

	return fullContextFiles, summaryContextFiles, nil
}
