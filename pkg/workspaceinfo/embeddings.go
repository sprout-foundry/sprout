package workspaceinfo

import (
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

// GetFilesForContextUsingEmbeddings selects files for context using embeddings.
func GetFilesForContextUsingEmbeddings(instructions string, workspaceFile WorkspaceFile, cfg *config.Config, logger *utils.Logger) ([]string, []string, error) {
	// Simple keyword-based selection for now.
	fullContextFiles := []string{}
	summaryContextFiles := []string{}
	for file := range workspaceFile.Files {
		if strings.Contains(file, "README.md") {
			fullContextFiles = append(fullContextFiles, file)
		} else {
			summaryContextFiles = append(summaryContextFiles, file)
		}
	}
	return fullContextFiles, summaryContextFiles, nil
}

