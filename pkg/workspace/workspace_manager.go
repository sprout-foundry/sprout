package workspace

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts" // Import the prompts package
	"github.com/alantheprice/ledit/pkg/utils"   // Import the utils package for logger
	"os"
	"path/filepath" // New import for security concern detection
	"regexp"        // New import for regex
	"sort"          // For sorting top directories and string slices
	"strings"
	"sync"
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

	// Regex patterns for common security concerns.
	// The first capturing group should ideally capture the "value" part if applicable.
	// Patterns are designed to be robust and capture common key/value formats.
	securityConcernRegexes = map[string][]*regexp.Regexp{
		"API Key": {
			regexp.MustCompile(`(?i)(?:api_key|apikey|api-key|client_id|client_secret|auth_token|access_token|bearer_token|token)\s*[:=]\s*['"]?([a-zA-Z0-9+/=._-]{16,})['"]?`),
			regexp.MustCompile(`(?i)x-api-key:\s*([a-zA-Z0-9+/=._-]{16,})`),
		},
		"Password": {
			regexp.MustCompile(`(?i)(?:password|pass|pwd|db_password|db_pass)\s*[:=]\s*['"]?([a-zA-Z0-9!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?~` + "`" + `]{8,})['"]?`),
		},
		"Secret": {
			regexp.MustCompile(`(?i)(?:secret_key|secretkey|secret-key|app_secret|client_secret)\s*[:=]\s*['"]?([a-zA-Z0-9+/=._-]{16,})['"]?`),
		},
		"AWS Key": {
			regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                                                 // AWS Access Key ID format
			regexp.MustCompile(`(?i)aws_secret_access_key\s*[:=]\s*['"]?([0-9a-zA-Z/+]{40})['"]?`), // AWS Secret Access Key format
		},
		"Azure Key": {
			regexp.MustCompile(`(?i)(?:azure_client_secret|azure_subscription_key)\s*[:=]\s*['"]?([a-zA-Z0-9+/=._-]{16,})['"]?`),
		},
		"Database Creds": {
			regexp.MustCompile(`(?i)(?:db_user|db_host|db_name)\s*[:=]\s*['"]?([a-zA-Z0-9_.-]{1,})['"]?`), // Catching these might indicate a connection string
			regexp.MustCompile(`(?i)jdbc:mysql://(?:[^/]+)/([^?]+)\?user=([^&]+)&password=([^&]+)`),       // JDBC connection string
			regexp.MustCompile(`(?i)mongodb://(?:[^:]+):([^@]+)@`),                                        // MongoDB password
		},
		"SSH Key": {
			regexp.MustCompile(`BEGIN (RSA|DSA|EC|OPENSSH) PRIVATE KEY`),
		},
		"Encryption Key": {
			regexp.MustCompile(`(?i)(?:encryption_key|decryption_key|aes_key|rsa_private_key)\s*[:=]\s*['"]?([a-zA-Z0-9+/=._-]{16,})['"]?`),
		},
	}

	// Regex patterns for common placeholder values to ignore.
	commonPlaceholderRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)your_api_key`),
		regexp.MustCompile(`(?i)your_secret_key`),
		regexp.MustCompile(`(?i)your_password`),
		regexp.MustCompile(`(?i)your_token`),
		regexp.MustCompile(`(?i)dummy`),
		regexp.MustCompile(`(?i)placeholder`),
		regexp.MustCompile(`(?i)example`),
		regexp.MustCompile(`(?i)test`),
		regexp.MustCompile(`(?i)changeme`),
		regexp.MustCompile(`null`),
		regexp.MustCompile(`nil`),
		regexp.MustCompile(`""`),
		regexp.MustCompile(`''`),
		regexp.MustCompile(`0000`),
		regexp.MustCompile(`xxxx`),
		regexp.MustCompile(`yyyy`),
		regexp.MustCompile(`zzzz`),
		regexp.MustCompile(`1234567890`),
		regexp.MustCompile(`abcdefghij`),
		regexp.MustCompile(`[a-f0-9]{32}`), // Common hash-like placeholders (MD5)
		regexp.MustCompile(`[a-f0-9]{40}`), // Common hash-like placeholders (SHA1)
		regexp.MustCompile(`[a-f0-9]{64}`), // Common hash-like placeholders (SHA256)
	}
)

// detectSecurityConcerns scans file content for security-sensitive patterns using robust regex.
// It attempts to find common patterns for keys and ignores common placeholder values.
func detectSecurityConcerns(content string, cfg *config.Config) []string {
	var concerns []string
	detectedConcernsMap := make(map[string]bool) // Use a map to store unique concerns

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			continue
		}

		for concernType, patterns := range securityConcernRegexes {
			for _, re := range patterns {
				matches := re.FindStringSubmatch(trimmedLine)
				if len(matches) > 0 {
					// If the regex has a capturing group (i.e., it's trying to extract a value)
					if len(matches) > 1 {
						value := matches[1]
						isPlaceholder := false
						for _, placeholderRe := range commonPlaceholderRegexes {
							if placeholderRe.MatchString(value) {
								isPlaceholder = true
								break
							}
						}
						if !isPlaceholder {
							if !detectedConcernsMap[concernType] {
								concerns = append(concerns, concernType)
								detectedConcernsMap[concernType] = true
							}
						}
					} else {
						// No capturing group, meaning the presence of the pattern itself is a concern (e.g., "BEGIN PRIVATE KEY")
						if !detectedConcernsMap[concernType] {
							concerns = append(concerns, concernType)
							detectedConcernsMap[concernType] = true
						}
					}
				}
			}
		}
	}
	sort.Strings(concerns) // Sort for consistent output
	return concerns
}

// stringSliceEqual checks if two string slices are equal, ignoring order.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aCopy := make([]string, len(a))
	bCopy := make([]string, len(b))
	copy(aCopy, a)
	copy(bCopy, b)
	sort.Strings(aCopy)
	sort.Strings(bCopy)
	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}
	return true
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

	// Maps to temporarily store security concerns and ignored ones for the current run
	// These will be used to update the workspace.Files map after LLM analysis (if any)
	fileSecurityConcernsMap := make(map[string][]string)
	fileIgnoredSecurityConcernsMap := make(map[string][]string)

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

		var detectedConcerns []string
		var concernsForThisFile []string
		var ignoredConcernsForThisFile []string
		skipLLMSummarization := false // New flag to control LLM summarization

		// --- Security Concern Detection and User Interaction ---
		if cfg.EnableSecurityChecks { // Only perform security checks if the flag is enabled
			if isNew || isChanged {
				// Only run security detection for new or changed files
				detectedConcerns = detectSecurityConcerns(fileContent, cfg)
				// If file exists and is changed, start with previously ignored concerns
				if exists {
					ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, existingFileInfo.IgnoredSecurityConcerns...)
				}
			} else {
				// File is unchanged. Use existing security concerns and ignored concerns.
				concernsForThisFile = append(concernsForThisFile, existingFileInfo.SecurityConcerns...)
				ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, existingFileInfo.IgnoredSecurityConcerns...)
				// detectedConcerns is not strictly needed here, but for consistency in the following logic,
				// we can set it to the previously identified issues.
				detectedConcerns = existingFileInfo.SecurityConcerns
			}

			// Filter out concerns that were previously ignored (only applies if isNew or isChanged)
			newlyDetectedConcerns := []string{}
			if isNew || isChanged { // Only prompt for new detections on new/changed files
				for _, concern := range detectedConcerns {
					isAlreadyIgnored := false
					for _, ignored := range ignoredConcernsForThisFile {
						if ignored == concern {
							isAlreadyIgnored = true
							break
						}
					}
					if !isAlreadyIgnored {
						newlyDetectedConcerns = append(newlyDetectedConcerns, concern)
					}
				}
			}

			// Prompt user for newly detected, unignored concerns
			for _, concern := range newlyDetectedConcerns {
				prompt := fmt.Sprintf("Security concern detected in %s: '%s'. Is this an issue?", relativePath, concern)
				if logger.AskForConfirmation(prompt, true) { // This is a required check
					concernsForThisFile = append(concernsForThisFile, concern)
					logger.Logf("Security concern '%s' in %s noted as an issue.", concern, relativePath)
				} else {
					ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, concern)
					logger.Logf("Security concern '%s' in %s noted as unimportant.", concern, relativePath)
				}
			}

			// Add back any concerns that were previously marked as issues and are still detected
			// This applies only if the file content changed, as for unchanged files, concernsForThisFile already holds them.
			if isNew || isChanged {
				if exists { // Only if it's an existing file that changed
					for _, prevConcern := range existingFileInfo.SecurityConcerns {
						isStillDetected := false
						for _, currentDetected := range detectedConcerns { // `detectedConcerns` is fresh for new/changed files
							if prevConcern == currentDetected {
								isStillDetected = true
								break
							}
						}
						if isStillDetected {
							// Ensure it's not already added to concernsForThisFile (e.g., if it was also in newlyDetectedConcerns)
							found := false
							for _, c := range concernsForThisFile {
								if c == prevConcern {
									found = true
									break
								}
							}
							if !found {
								concernsForThisFile = append(concernsForThisFile, prevConcern)
							}
						}
					}
				}
			}
			sort.Strings(concernsForThisFile)
			sort.Strings(ignoredConcernsForThisFile)

			// If there are confirmed security concerns, mark for skipping LLM summarization
			if len(concernsForThisFile) > 0 {
				skipLLMSummarization = true
				logger.LogProcessStep(prompts.SkippingLLMSummarizationDueToSecurity(relativePath))
			}
		} else {
			// If security checks are disabled, ensure no security concerns are stored.
			concernsForThisFile = []string{}
			ignoredConcernsForThisFile = []string{}
		}

		// Store the determined security status for this file
		fileSecurityConcernsMap[relativePath] = concernsForThisFile
		fileIgnoredSecurityConcernsMap[relativePath] = ignoredConcernsForThisFile
		// --- End Security Concern Detection ---

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
			// File is unchanged. Check if security concerns or ignored concerns have changed.
			// If they changed, update the workspace.Files directly without LLM analysis.
			if !stringSliceEqual(existingFileInfo.SecurityConcerns, concernsForThisFile) ||
				!stringSliceEqual(existingFileInfo.IgnoredSecurityConcerns, ignoredConcernsForThisFile) {
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

			resultsChan <- processResult{
				relativePath:            f.relativePath,
				summary:                 fileSummary,
				exports:                 fileExports,
				references:              fileReferences,
				hash:                    f.hash,
				tokenCount:              f.tokenCount,
				securityConcerns:        fileSecurityConcernsMap[f.relativePath],        // Retrieve from map
				ignoredSecurityConcerns: fileIgnoredSecurityConcernsMap[f.relativePath], // Retrieve from map
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

	// If security checks are enabled, log the start of the check.
	if cfg.EnableSecurityChecks {
		logger.LogProcessStep(prompts.PerformingSecurityCheck())
	}

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
		if fileInfo.Summary == "Skipped due to confirmed security concerns." { // New check
			logger.LogUserInteraction(fmt.Sprintf("----- WARNING!!! -----:\n\n The file %s was selected for full context but was skipped due to confirmed security concerns. Its content will not be provided to the LLM.\n", file))
			continue
		}
	}

	return getWorkspaceInfo(workspace, fullContextFiles, summaryContextFiles)
}
