package workspace

import (
	"fmt"
	"ledit/pkg/config"
	"ledit/pkg/llm"
	"ledit/pkg/utils" // Import the utils package for logger
	"os"
	"path/filepath"
	"sort" // For sorting top directories
	"strings"
	"sync"
)

// processResult is used to pass analysis results from goroutines back to the main thread.
type processResult struct {
	relativePath string
	summary      string
	exports      string
	hash         string
	references   string
	tokenCount   int
	err          error
}

var (
	maxTokenCount  = 20096
	textExtensions = map[string]bool{
		".txt": true, ".go": true, ".py": true, ".js": true, ".java": true,
		".c": true, ".cpp": true, ".h": true, ".hpp": true, ".md": true,
		".json": true, ".yaml": true, ".yml": true, ".sh": true, ".bash": true,
		".sql": true, ".html": true, ".css": true, ".xml": true, ".csv": true,
		".ts": true, ".tsx": true, ".php": true, ".rb": true, ".swift": true,
		".kt": true, ".scala": true, ".rs": true, ".dart": true, ".pl": true,
		".pm": true, ".lua": true, ".vim": true, ".toml": true,
	}
)

// fileToProcess holds information about a file that needs analysis.
type fileToProcess struct {
	path         string
	relativePath string
	content      string
	hash         string
	tokenCount   int
}

// validateAndUpdateWorkspace checks the current file system against the workspace.json file,
// analyzes new or changed files, removes deleted files, and saves the updated workspace.
func validateAndUpdateWorkspace(rootDir, workspaceFilePath string, cfg *config.Config) (WorkspaceFile, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	workspace, err := loadWorkspaceFile(workspaceFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.LogProcessStep("No existing workspace file found. Creating a new one.")
			workspace = WorkspaceFile{Files: make(map[string]WorkspaceFileInfo)}
		} else {
			return WorkspaceFile{}, fmt.Errorf("failed to load workspace file: %w", err)
		}
	}

	currentFiles := make(map[string]bool)
	ignoreRules := GetIgnoreRules(rootDir)

	var filesToAnalyzeList []fileToProcess
	newFilesCount := 0
	newFilesTopDirs := make(map[string]int) // Map to store count of new files per top-level directory

	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Name() != "." && strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relativePath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		if ignoreRules != nil && ignoreRules.MatchesPath(relativePath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// More robust file type checking
		ext := strings.ToLower(filepath.Ext(path))
		if !textExtensions[ext] {
			return nil
		}

		currentFiles[relativePath] = true

		content, err := os.ReadFile(path)
		if err != nil {
			logger.Logf("Warning: could not read file %s: %v. Skipping.\n", path, err)
			return nil
		}

		fileContent := string(content)
		newHash := generateFileHash(fileContent)

		existingFileInfo, exists := workspace.Files[relativePath]
		if exists {
			if existingFileInfo.Hash == newHash {
				return nil // File exists and is unchanged, no analysis needed
			}
			logger.LogProcessStep(fmt.Sprintf("File %s has changed. Marking for re-analysis...", relativePath))
		} else {
			logger.LogProcessStep(fmt.Sprintf("New file %s found. Marking for analysis...", relativePath))
			newFilesCount++
			// Determine top-level directory
			parts := strings.Split(relativePath, string(os.PathSeparator))
			if len(parts) > 0 {
				topDir := parts[0]
				newFilesTopDirs[topDir]++
			}
		}

		tokenCount := llm.EstimateTokens(fileContent)

		if tokenCount > maxTokenCount {
			logger.Logf("Skipping analysis of large file %s (%d tokens > %d). Adding as 'too large'.\n", relativePath, tokenCount, maxTokenCount)
			workspace.Files[relativePath] = WorkspaceFileInfo{
				Hash:       newHash,
				Summary:    "File is too large to analyze.",
				Exports:    "",
				References: "",
				TokenCount: tokenCount,
			}
			return nil
		}

		filesToAnalyzeList = append(filesToAnalyzeList, fileToProcess{
			path:         path,
			relativePath: relativePath,
			content:      fileContent,
			hash:         newHash,
			tokenCount:   tokenCount,
		})

		return nil
	})

	if err != nil {
		return workspace, err
	}

	// --- Warning and Confirmation for too many new files ---
	if newFilesCount > 30 {
		var topDirsList []string
		for dir := range newFilesTopDirs {
			topDirsList = append(topDirsList, dir)
		}
		sort.Strings(topDirsList) // Sort for consistent output

		var topDirsMessage strings.Builder
		topDirsMessage.WriteString("The following top-level directories contain new files:\n")
		for _, dir := range topDirsList {
			topDirsMessage.WriteString(fmt.Sprintf("  - %s (%d new files)\n", dir, newFilesTopDirs[dir]))
		}

		warningMessage := fmt.Sprintf(
			"WARNING: %d new files have been detected in your workspace.\n"+
				"This might indicate that a large directory (e.g., node_modules, build) is not being correctly ignored.\n"+
				"%s\n"+
				"Do you want to proceed with analyzing these new files? (This may take a long time and consume LLM tokens)",
			newFilesCount, topDirsMessage.String(),
		)

		if !logger.AskForConfirmation(warningMessage, true) { // this is a required confirmation and will exit if in a non-interactive mode
			return WorkspaceFile{}, fmt.Errorf("workspace update cancelled by user due to too many new files")
		}
	}
	// --- End of Warning and Confirmation ---

	var wg sync.WaitGroup
	resultsChan := make(chan processResult, len(filesToAnalyzeList)) // Buffer channel for all results

	if len(filesToAnalyzeList) > 0 {
		logger.LogProcessStep(fmt.Sprintf("Waiting for analysis of %d files to complete...", len(filesToAnalyzeList)))
	}

	for _, file := range filesToAnalyzeList {
		wg.Add(1)
		go func(f fileToProcess, cfg *config.Config) {
			defer wg.Done()

			var fileSummary, fileExports, fileReferences string
			var llmErr error

			if len(f.content) > 0 {
				logger.Logf("Analyzing %s for workspace...", f.path)
				fileSummary, fileExports, fileReferences, llmErr = getSummary(f.content, f.path, cfg)
			}

			resultsChan <- processResult{
				relativePath: f.relativePath,
				summary:      fileSummary,
				exports:      fileExports,
				references:   fileReferences,
				hash:         f.hash,
				tokenCount:   f.tokenCount,
				err:          llmErr,
			}
		}(file, cfg)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	for result := range resultsChan {
		if result.err != nil {
			logger.Logf("Warning: could not analyze file %s: %v. Proceeding with empty summary/exports.\n", result.relativePath, result.err)
		}
		workspace.Files[result.relativePath] = WorkspaceFileInfo{
			Hash:       result.hash,
			Summary:    result.summary,
			Exports:    result.exports,
			References: result.references,
			TokenCount: result.tokenCount,
		}
	}

	for filePath := range workspace.Files {
		if _, exists := currentFiles[filePath]; !exists {
			logger.LogProcessStep(fmt.Sprintf("File %s has been removed. Removing from workspace...", filePath))
			delete(workspace.Files, filePath)
		}
	}

	if err := saveWorkspaceFile(workspace, workspaceFilePath); err != nil {
		return workspace, err
	}

	return workspace, nil
}

// GetWorkspaceContext orchestrates the workspace loading, analysis, and context generation process.
func GetWorkspaceContext(instructions string, cfg *config.Config) string {
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.LogProcessStep("--- Loading in workspace data ---")
	workspaceFilePath := "./.ledit/workspace.json"

	if err := os.MkdirAll(filepath.Dir(workspaceFilePath), os.ModePerm); err != nil {
		logger.Logf("Error creating .ledit directory for WORKSPACE: %v. Continuing without it.\n", err)
		return ""
	}

	workspace, err := validateAndUpdateWorkspace("./", workspaceFilePath, cfg)
	if err != nil {
		logger.Logf("Error loading/updating content from WORKSPACE: %v. Continuing without it.\n", err)
		return ""
	}

	logger.LogProcessStep("--- Asking LLM to select relevant files for context ---")
	fullContextFiles, summaryContextFiles, err := getFilesForContext(instructions, workspace, cfg)
	if err != nil {
		logger.Logf("Warning: could not determine which files to load for context: %v. Proceeding with all summaries.\n", err)
		// If LLM fails to select files, provide the full file list but no specific full/summary context.
		return getWorkspaceInfo(workspace, nil, nil)
	}

	if len(fullContextFiles) > 0 {
		logger.LogProcessStep(fmt.Sprintf("--- LLM selected the following files for full context: %s ---", strings.Join(fullContextFiles, ", ")))
	}
	if len(summaryContextFiles) > 0 {
		logger.LogProcessStep(fmt.Sprintf("--- LLM selected the following files for summary context: %s ---", strings.Join(summaryContextFiles, ", ")))
	}
	if len(fullContextFiles) == 0 && len(summaryContextFiles) == 0 {
		logger.LogProcessStep("--- LLM decided no files are relevant for context. ---")
	}

	for _, file := range fullContextFiles {
		fileInfo, exists := workspace.Files[file]
		if !exists {
			logger.Logf("Warning: file %s selected for full context not found in workspace. Skipping.\n", file)
			continue
		}
		if fileInfo.Summary == "File is too large to analyze." {
			logger.LogUserInteraction(fmt.Sprintf("----- ERROR!!! -----:\n\n The file %s is too large to include in full context. Please pass it directly if needed.\n", file))
			continue
		}
	}

	return getWorkspaceInfo(workspace, fullContextFiles, summaryContextFiles)
}
