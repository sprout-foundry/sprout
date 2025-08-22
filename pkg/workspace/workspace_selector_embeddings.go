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
	// Use scores to determine context allocation
	var fullContextFiles []string
	var summaryContextFiles []string

	// Always include at least one file with full context
	if len(relevantEmbeddings) > 0 {
		// Find the highest scoring file for full context
		maxScoreIndex := 0
		maxScore := scores[0]
		for i, score := range scores {
			if score > maxScore {
				maxScore = score
				maxScoreIndex = i
			}
		}

		// Include ONLY the top-scoring file by default
		fullContextFiles = append(fullContextFiles, relevantEmbeddings[maxScoreIndex].Path)
		logger.Logf("Selected for full context (top match %.4f): %s\n", scores[maxScoreIndex], relevantEmbeddings[maxScoreIndex].Path)

		// High-confidence threshold for additional full-context files
		const absoluteFloor = 0.6
		relativeFloor := maxScore * 0.5
		highConfidence := relativeFloor
		if highConfidence < absoluteFloor {
			highConfidence = absoluteFloor
		}
		const maxAdditionalFull = 6
		added := 0

		for i, emb := range relevantEmbeddings {
			if i == maxScoreIndex {
				continue // Already handled above
			}

			if scores[i] >= highConfidence && added < maxAdditionalFull {
				fullContextFiles = append(fullContextFiles, emb.Path)
				added++
				logger.Logf("Selected for full context (high-confidence %.4f): %s\n", scores[i], emb.Path)
			} else {
				summaryContextFiles = append(summaryContextFiles, emb.Path)
				logger.Logf("Selected for summary context (%.4f): %s\n", scores[i], emb.Path)
			}
		}
	}

	return fullContextFiles, summaryContextFiles, nil
}
