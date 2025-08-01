package workspace

import (
	"encoding/json" // New import for package.json parsing
	"fmt"
	"os"
	"path/filepath" // New import for security concern detection
	"sort"          // For sorting top directories and string slices
	"strings"
	"sync"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"  // Import the prompts package
	"github.com/alantheprice/ledit/pkg/security" // New import for security checks
	"github.com/alantheprice/ledit/pkg/utils"    // Import the utils package for logger
)

// processResult is used to pass analysis results from goroutines back to the main thread.
type processResult struct {
	relativePath            string
	summary                 string
	exports                 string
	hash                    string
	references              string
	tokenCount              int
	securityConcerns        []string // New field
	ignoredSecurityConcerns []string // New field
	err                     error
}

// fileToProcess holds information about a file that needs to be analyzed by the LLM.
type fileToProcess struct {
	path         string
	relativePath string
	content      string
	hash         string
	tokenCount   int
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

// detectBuildCommand attempts to autogenerate a build command based on project type.
// It checks for Go projects (presence of .go files) and Node.js projects (presence of package.json).
func detectBuildCommand(rootDir string) string {
	// Check for Go project
	goFilesFound := false
	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip common ignored directories
		if info.IsDir() && (info.Name() == "vendor" || info.Name() == "node_modules" || info.Name() == ".git" || info.Name() == "build" || info.Name() == "dist") {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			goFilesFound = true
			return fmt.Errorf("found go file") // Use a custom error to stop walking
		}
		return nil
	})
	if goFilesFound {
		return "gomft -w . && go build"
	}

	// Check for JavaScript/Node.js project
	packageJSONPath := filepath.Join(rootDir, "package.json")
	if _, err := os.Stat(packageJSONPath); err == nil {
		content, err := os.ReadFile(packageJSONPath)
		if err != nil {
			return "" // Cannot read package.json
		}

		var pkgJSON map[string]interface{}
		if err := json.Unmarshal(content, &pkgJSON); err != nil {
			return "" // Cannot parse package.json
		}

		if scripts, ok := pkgJSON["scripts"].(map[string]interface{}); ok {
			if _, hasBuild := scripts["build"]; hasBuild {
				return "npm run build"
			}
			if _, hasStart := scripts["start"]; hasStart {
				return "npm start"
			}
		}
	}

	return "" // Cannot determine build command
}

// validateAndUpdateWorkspace checks the current file system against the workspace.json file,
// analyzes new or changed files, removes deleted files, and saves the updated workspace.
func validateAndUpdateWorkspace(rootDir string, cfg *config.Config) (WorkspaceFile, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	workspace, err := LoadWorkspaceFile()
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
		isChanged := exists && existingFileInfo.Hash != newHash
		isNew := !exists

		var (
			concernsForThisFile        []string
			ignoredConcernsForThisFile []string
			skipLLMSummarization       bool
		)

		// --- Security Concern Detection and User Interaction ---
		if cfg.EnableSecurityChecks {
			concernsForThisFile, ignoredConcernsForThisFile, skipLLMSummarization = security.CheckFileSecurity(
				relativePath,
				fileContent,
				isNew,
				isChanged,
				existingFileInfo.SecurityConcerns,
				existingFileInfo.IgnoredSecurityConcerns,
				cfg,
			)

			// If file is unchanged and security concerns/ignored concerns are also unchanged,
			// then no further action is needed for this file.
			if !isNew && !isChanged &&
				utils.StringSliceEqual(existingFileInfo.SecurityConcerns, concernsForThisFile) &&
				utils.StringSliceEqual(existingFileInfo.IgnoredSecurityConcerns, ignoredConcernsForThisFile) {
				return nil // File exists, unchanged, security concerns stable.
			}
		} else {
			// If security checks are disabled, ensure no security concerns are stored.
			concernsForThisFile = []string{}
			ignoredConcernsForThisFile = []string{}
			skipLLMSummarization = false
		}

		needsLLMSummarization := false
		if isNew || isChanged {
			if !skipLLMSummarization { // Only summarize if no security concerns confirmed
				needsLLMSummarization = true
			} else {
				// If skipped due to security, update workspace info with placeholder summary
				workspace.Files[relativePath] = WorkspaceFileInfo{
					Hash:                    newHash,
					Summary:                 "Skipped due to confirmed security concerns.",
					Exports:                 "",                              // Clear exports
					References:              "",                              // Clear references
					TokenCount:              llm.EstimateTokens(fileContent), // Still estimate tokens
					SecurityConcerns:        concernsForThisFile,
					IgnoredSecurityConcerns: ignoredConcernsForThisFile,
				}
				return nil // Skip LLM analysis for this file
			}
		} else {
			// File is unchanged, but security concerns might have been updated (e.g., user changed mind on a prompt).
			// In this case, we update the workspace info but don't re-summarize.
			if !utils.StringSliceEqual(existingFileInfo.SecurityConcerns, concernsForThisFile) ||
				!utils.StringSliceEqual(existingFileInfo.IgnoredSecurityConcerns, ignoredConcernsForThisFile) {
				logger.Logf("Updating security concerns for unchanged file %s.", relativePath)
				// Preserve existing summary/exports/references if content is unchanged
				workspace.Files[relativePath] = WorkspaceFileInfo{
					Hash:                    newHash,
					Summary:                 existingFileInfo.Summary,
					Exports:                 existingFileInfo.Exports,
					References:              existingFileInfo.References,
					TokenCount:              existingFileInfo.TokenCount,
					SecurityConcerns:        concernsForThisFile,
					IgnoredSecurityConcerns: ignoredConcernsForThisFile,
				}
				return nil // No LLM analysis needed, continue to next file
			}
		}

		if !needsLLMSummarization {
			return nil // File exists, unchanged, security concerns stable, and no new summarization needed.
		}

		// If we reach here, it means the file is new or its content has changed, AND it's not skipped due to security.
		// So, it needs LLM summarization.
		tokenCount := llm.EstimateTokens(fileContent)

		if tokenCount > maxTokenCount {
			logger.Logf("Skipping analysis of large file %s (%d tokens > %d). Adding as 'too large'.\n", relativePath, tokenCount, maxTokenCount)
			workspace.Files[relativePath] = WorkspaceFileInfo{
				Hash:                    newHash,
				Summary:                 "File is too large to analyze.",
				Exports:                 "",
				References:              "",
				TokenCount:              tokenCount,
				SecurityConcerns:        concernsForThisFile,
				IgnoredSecurityConcerns: ignoredConcernsForThisFile,
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

		if isNew {
			newFilesCount++
			// Determine top-level directory
			parts := strings.Split(relativePath, string(os.PathSeparator))
			if len(parts) > 0 {
				topDir := parts[0]
				newFilesTopDirs[topDir]++
			}
		}

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

			// Re-fetch security concerns and ignored concerns from the workspace map
			// This is important because the security check might have updated them
			// even if LLM summarization was skipped.
			currentFileInfo, exists := workspace.Files[f.relativePath]
			var finalSecurityConcerns []string
			var finalIgnoredSecurityConcerns []string
			if exists {
				finalSecurityConcerns = currentFileInfo.SecurityConcerns
				finalIgnoredSecurityConcerns = currentFileInfo.IgnoredSecurityConcerns
			} else {
				// This case should ideally not happen if the file was added to filesToAnalyzeList
				// after the security check, but as a fallback, use empty slices.
				finalSecurityConcerns = []string{}
				finalIgnoredSecurityConcerns = []string{}
			}

			resultsChan <- processResult{
				relativePath:            f.relativePath,
				summary:                 fileSummary,
				exports:                 fileExports,
				references:              fileReferences,
				hash:                    f.hash,
				tokenCount:              f.tokenCount,
				securityConcerns:        finalSecurityConcerns,
				ignoredSecurityConcerns: finalIgnoredSecurityConcerns,
				err:                     llmErr,
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
			Hash:                    result.hash,
			Summary:                 result.summary,
			Exports:                 result.exports,
			References:              result.references,
			TokenCount:              result.tokenCount,
			SecurityConcerns:        result.securityConcerns,
			IgnoredSecurityConcerns: result.ignoredSecurityConcerns,
		}
	}

	for filePath := range workspace.Files {
		if _, exists := currentFiles[filePath]; !exists {
			logger.LogProcessStep(fmt.Sprintf("File %s has been removed. Removing from workspace...", filePath))
			delete(workspace.Files, filePath)
		}
	}

	// Autogenerate BuildCommand if it's empty
	if workspace.BuildCommand == "" {
		logger.LogProcessStep("--- Attempting to autogenerate build command ---")
		buildCommand := detectBuildCommand(rootDir)
		if buildCommand != "" {
			workspace.BuildCommand = buildCommand
			logger.LogProcessStep(fmt.Sprintf("--- Autogenerated build command: '%s' ---", buildCommand))
		} else {
			logger.LogProcessStep("--- Could not autogenerate build command. Will attempt again next time. ---")
		}
	}

	if err := saveWorkspaceFile(workspace); err != nil {
		return workspace, err
	}

	return workspace, nil
}

// GetWorkspaceContext orchestrates the workspace loading, analysis, and context generation process.
func GetWorkspaceContext(instructions string, cfg *config.Config) string {
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.LogProcessStep("--- Loading in workspace data ---")
	workspaceFilePath := "./.ledit/workspace.json"

	// If security checks are enabled, log the start of the check.
	if cfg.EnableSecurityChecks {
		logger.LogProcessStep(prompts.PerformingSecurityCheck())
	}

	if err := os.MkdirAll(filepath.Dir(workspaceFilePath), os.ModePerm); err != nil {
		logger.Logf("Error creating .ledit directory for WORKSPACE: %v. Continuing without it.\n", err)
		return ""
	}

	workspace, err := validateAndUpdateWorkspace("./", cfg)
	if err != nil {
		logger.Logf("Error loading/updating content from WORKSPACE: %v. Continuing without it.\n", err)
		return ""
	}

	// Autogenerate Project Goals if they are empty
	if (workspace.ProjectGoals == ProjectGoals{}) { // Check if the struct is zero-valued
		logger.LogProcessStep("--- Autogenerating project goals based on workspace context ---")
		// Create a summary of the workspace for the LLM to infer goals
		var workspaceSummaryBuilder strings.Builder
		workspaceSummaryBuilder.WriteString("Current Workspace File Summaries:\n")
		for filePath, fileInfo := range workspace.Files {
			workspaceSummaryBuilder.WriteString(fmt.Sprintf("File: %s\nSummary: %s\n", filePath, fileInfo.Summary))
			if fileInfo.Exports != "" {
				workspaceSummaryBuilder.WriteString(fmt.Sprintf("Exports: %s\n", fileInfo.Exports))
			}
			workspaceSummaryBuilder.WriteString("\n")
		}

		generatedGoals, goalErr := GetProjectGoals(cfg, workspaceSummaryBuilder.String())
		if goalErr != nil {
			logger.Logf("Warning: Failed to autogenerate project goals: %v. Proceeding without them.\n", goalErr)
		} else {
			workspace.ProjectGoals = generatedGoals
			// Save the workspace with the newly generated goals
			if err := saveWorkspaceFile(workspace); err != nil {
				logger.Logf("Warning: Failed to save autogenerated project goals to workspace file: %v\n", err)
			} else {
				logger.LogProcessStep("--- Autogenerated project goals saved. ---")
			}
		}
	}

	logger.LogProcessStep("--- Asking LLM to select relevant files for context ---")
	fullContextFiles, summaryContextFiles, err := getFilesForContext(instructions, workspace, cfg)
	if err != nil {
		logger.Logf("Warning: could not determine which files to load for context: %v. Proceeding with all summaries.\n", err)
		// If LLM fails to select files, provide the full file list but no specific full/summary context.
		return getWorkspaceInfo(workspace, nil, nil, workspace.ProjectGoals, cfg.CodeStyle)
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
		if fileInfo.Summary == "Skipped due to confirmed security concerns." { // New check
			logger.LogUserInteraction(fmt.Sprintf("----- WARNING!!! -----:\n\n The file %s was selected for full context but was skipped due to confirmed security concerns. Its content will not be provided to the LLM.\n", file))
			continue
		}
	}

	return getWorkspaceInfo(workspace, fullContextFiles, summaryContextFiles, workspace.ProjectGoals, cfg.CodeStyle)
}
