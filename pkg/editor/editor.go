package editor

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/context"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/git"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webcontent"
	"github.com/alantheprice/ledit/pkg/workspace"

	"github.com/fatih/color"
)

// getLanguageFromExtension infers the programming language from the file extension.
func getLanguageFromExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".ts":
		return "javascript"
	case ".java":
		return "java"
	case ".c", ".cpp", ".h":
		return "c"
	case ".sh":
		return "bash"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".xml":
		return "xml"
	case ".html":
		return "html"
	case ".css":
		return "css"
	case ".yaml", ".yml":
		return "yaml"
	case ".sql":
		return "sql"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".rs":
		return "rust"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	case ".cs":
		return "csharp"
	default:
		return "text"
	}
}

// ProcessInstructionsWithWorkspace function removed from here. It's moved to pkg/filesystem/loader.go

func ProcessInstructionsWithWorkspace(instructions string, cfg *config.Config) (string, error) {
	// Replace any existing #WS or #WORKSPACE tags with a single #WS tag
	re := regexp.MustCompile(`(?i)\s*#(WS|WORKSPACE)\s*$`)
	instructions = re.ReplaceAllString(instructions, "") + " #WS"

	return ProcessInstructions(instructions, cfg)
}

func ProcessInstructions(instructions string, cfg *config.Config) (string, error) {
	// Note: Search grounding is now handled via explicit tool calls instead of #SG flags
	// This prevents accidental triggering by LLM responses and provides better control

	// Updated pattern to capture line ranges: #filename:start-end or #filename:start,end
	filePattern := regexp.MustCompile(`\s+#(\S+?)(?::(\d+)[-,](\d+))?`)
	matches := filePattern.FindAllStringSubmatch(instructions, -1)

	fmt.Println("Found patterns:", matches) // Logging the patterns found

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		path := match[1]
		var startLine, endLine int
		var err error
		var content string

		// Parse line range if provided
		if len(match) >= 4 && match[2] != "" && match[3] != "" {
			if startLine, err = strconv.Atoi(match[2]); err != nil {
				fmt.Printf("Warning: Invalid start line number '%s' for %s, using full file\n", match[2], path)
				startLine = 0
			}
			if endLine, err = strconv.Atoi(match[3]); err != nil {
				fmt.Printf("Warning: Invalid end line number '%s' for %s, using full file\n", match[3], path)
				endLine = 0
			}
		}

		fmt.Printf("Processing path: %s", path) // Logging the path being processed
		if startLine > 0 && endLine > 0 {
			fmt.Printf(" (lines %d-%d)", startLine, endLine)
		}
		fmt.Println()

		if path == "WORKSPACE" || path == "WS" {
			fmt.Println(prompts.LoadingWorkspaceData())
			content = workspace.GetWorkspaceContext(instructions, cfg)
		} else if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			content, err = webcontent.NewWebContentFetcher().FetchWebContent(path, cfg) // Pass cfg here
			if err != nil {
				fmt.Print(prompts.URLFetchError(path, err))
				continue
			}
		} else {
			// Use partial loading if line range is specified
			if startLine > 0 && endLine > 0 {
				content, err = filesystem.LoadFileContentWithRange(path, startLine, endLine)
			} else {
				content, err = filesystem.LoadFileContent(path) // CHANGED: Call filesystem.LoadFileContent
			}
			if err != nil {
				fmt.Print(prompts.FileLoadError(path, err))
				continue
			}
		}

		// Replace the original pattern (including line range) with content
		originalPattern := match[0] // Full match including whitespace and line range
		instructions = strings.Replace(instructions, originalPattern, content, 1)
	}
	return instructions, nil
}

// GetLLMCodeResponse function removed from here, as it's now in pkg/context/context_builder.go

func getUpdatedCode(originalCode, instructions, filename string, cfg *config.Config, imagePath string) (map[string]string, string, error) {
	log := utils.GetLogger(cfg.SkipPrompt)
	modelName, llmContent, err := context.GetLLMCodeResponse(cfg, originalCode, instructions, filename, imagePath) // Updated call site
	if err != nil {
		return nil, "", fmt.Errorf("failed to get LLM response: %w", err)
	}

	log.Log(prompts.ModelReturned(modelName, llmContent))

	updatedCode, err := parser.GetUpdatedCodeFromResponse(llmContent)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse updated code from response: %w", err)
	}
	if len(updatedCode) == 0 {
		fmt.Println(prompts.NoCodeBlocksParsed())
		fmt.Printf("%s\n", llmContent) // Print the raw LLM response since it may be used directly by the user
	}
	return updatedCode, llmContent, nil
}

func parseCommitMessage(commitMessage string) (string, string, error) {
	lines := strings.Split(commitMessage, "\n")
	if len(lines) < 2 {
		return "", "", fmt.Errorf("failed to parse commit message")
	}

	note := lines[0]
	description := strings.Join(lines[2:], "\n")
	return note, description, nil
}

// OpenInEditor opens the provided content in the user's default editor (or vim)
// and returns the edited content.
func OpenInEditor(content, fileExtension string) (string, error) {
	tempFile, err := os.CreateTemp("", "ledit-*"+fileExtension)
	if err != nil {
		return "", fmt.Errorf("could not create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())

	if _, err := tempFile.WriteString(content); err != nil {
		return "", fmt.Errorf("could not write to temp file: %w", err)
	}
	tempFile.Close()

	editorPath := os.Getenv("EDITOR")
	if editorPath == "" {
		editorPath = "vim" // A reasonable default
	}
	cmd := exec.Command(editorPath, tempFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error running editor: %w", err)
	}

	editedContent, err := os.ReadFile(tempFile.Name())
	if err != nil {
		return "", fmt.Errorf("could not read edited file: %w", err)
	}
	return string(editedContent), nil
}

func handleFileUpdates(updatedCode map[string]string, revisionID string, cfg *config.Config, originalInstructions string, processedInstructions string, llmResponseRaw string) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	var allDiffs strings.Builder

	for newFilename, newCode := range updatedCode {
		originalCode, _ := filesystem.LoadOriginalCode(newFilename) // CHANGED: Call filesystem.LoadOriginalCode

		if originalCode == newCode {
			fmt.Print(prompts.NoChangesDetected(newFilename))
			continue
		}

		// Check if this is a partial response by looking for the partial content marker pattern
		if parser.IsPartialResponse(newCode) {
			// Instead of trying to merge, reject the partial response and ask for the full file
			fmt.Printf("⚠️  Detected partial response for %s. The LLM provided incomplete code with markers like '...unchanged...'.\n", newFilename)
			fmt.Printf("Requesting the LLM to provide the complete file content...\n")

			// Create a more specific prompt asking for the complete file
			retryPrompt := fmt.Sprintf(`The previous response contained partial content markers (like "...unchanged..." or "// rest of file") for the file %s. 
This is not acceptable as I need the COMPLETE file content.

Please provide the ENTIRE file content for %s from beginning to end, including:
- ALL imports and package declarations
- ALL existing functions and methods (both modified and unmodified)
- ALL variable declarations and constants
- ALL comments and documentation
- The specific changes requested in the original instructions

Do NOT use any partial content markers like "...unchanged...", "// rest of file", or similar abbreviations.
The file must be complete and ready to save and execute.

Original instructions: %s

Here is the current content of %s for reference:
`+"```"+`%s
%s
`+"```"+`

Please provide the complete updated file content.`, newFilename, newFilename, originalInstructions, newFilename, getLanguageFromExtension(newFilename), originalCode)

			// Use the editor to get a complete response
			retryResult, err := ProcessCodeGeneration(newFilename, retryPrompt, cfg, "")
			if err != nil {
				return "", fmt.Errorf("failed to get complete file content after partial response: %w", err)
			}

			if retryResult != "" {
				fmt.Printf("✅ Received complete file content for %s\n", newFilename)
				// The retry should have updated the file properly, continue with the next file
				continue
			} else {
				return "", fmt.Errorf("failed to get complete file content for %s after retry", newFilename)
			}
		}

		color.Yellow(prompts.OriginalFileHeader(newFilename))
		color.Yellow(prompts.UpdatedFileHeader(newFilename))

		diff := changetracker.GetDiff(newFilename, originalCode, newCode)
		if diff == "" {
			fmt.Print("No changes detected.")
		} else {
			fmt.Print(diff)
		}
		allDiffs.WriteString(diff)
		allDiffs.WriteString("\n")

		applyChanges := false
		editChoice := false
		if cfg.SkipPrompt {
			applyChanges = true
		} else {
			fmt.Print(prompts.ApplyChangesPrompt(newFilename))
			userInput, _ := reader.ReadString('\n')
			userInput = strings.TrimSpace(strings.ToLower(userInput))
			applyChanges = userInput == "y" || userInput == "yes"
			editChoice = userInput == "e"
		}

		if applyChanges || editChoice {
			if editChoice {
				editedCode, err := OpenInEditor(newCode, filepath.Ext(newFilename))
				if err != nil {
					return "", fmt.Errorf("error editing file: %w", err)
				}
				newCode = editedCode
			}

			// Ensure the directory exists
			dir := filepath.Dir(newFilename)
			if dir != "" {
				if err := os.MkdirAll(dir, os.ModePerm); err != nil {
					return "", fmt.Errorf("could not create directory %s: %w", dir, err)
				}
			}

			if err := filesystem.SaveFile(newFilename, newCode); err != nil {
				return "", fmt.Errorf("failed to save file: %w", err)
			}

			note, description, commit, err := getChangeSummaries(cfg, newCode, originalInstructions, newFilename, reader)
			if err != nil {
				return "", fmt.Errorf("failed to get change summaries: %w", err)
			}

			// Use the passed llmResponseRaw directly for llmMessage
			llmMessage := llmResponseRaw

			if err := changetracker.RecordChangeWithDetails(revisionID, newFilename, originalCode, newCode, description, note, originalInstructions, llmMessage, cfg.EditingModel); err != nil {
				return "", fmt.Errorf("failed to record change: %w", err)
			}
			fmt.Print(prompts.ChangesApplied(newFilename))

			if cfg.TrackWithGit {
				// get the filename path from the root of the git repository
				filePath, err := git.GetFileGitPath(newFilename) // CHANGED: Call git.GetFileGitPath
				if err != nil {
					return "", err
				}
				changeTypeName := "Update"
				if originalCode == "" {
					changeTypeName = "Add"
				} else if newCode == "" {
					changeTypeName = "Delete"
				}
				message := commit
				if message == "" {
					message = note
				}
				commitMessage := fmt.Sprintf("%s %s - %s", changeTypeName, filePath, message)

				if err := git.AddAndCommitFile(newFilename, commitMessage); err != nil { // CHANGED: Call git.AddAndCommitFile
					return "", err
				}
			}
		} else {
			fmt.Print(prompts.ChangesNotApplied(newFilename))
		}
	}

	// Perform automated review when skipPrompt is active
	if cfg.SkipPrompt {
		combinedDiff := allDiffs.String()
		if combinedDiff != "" {
			logger := utils.GetLogger(cfg.SkipPrompt)
			reviewErr := performAutomatedReview(combinedDiff, originalInstructions, processedInstructions, cfg, logger, revisionID)
			if reviewErr != nil {
				return "", reviewErr
			}
		}
	}

	return allDiffs.String(), nil
}

func getChangeSummaries(cfg *config.Config, newCode string, instructions string, newFilename string, reader *bufio.Reader) (note string, description string, commit string, err error) {
	note = "Changes made by ledit based on LLM suggestions."
	description = ""
	generatedDescription, err := llm.GetCommitMessage(cfg, newCode, instructions, newFilename)
	if err == nil && generatedDescription != "" {
		note, description, err := parseCommitMessage(generatedDescription)
		if err == nil {
			return note, description, generatedDescription, nil
		}
	}
	// It failed in the process, lets try one more time.
	generatedDescription, err = llm.GetCommitMessage(cfg, newCode, instructions, newFilename)
	if err == nil && generatedDescription != "" {
		note, description, err = parseCommitMessage(generatedDescription)
		if err == nil {
			return note, description, generatedDescription, nil
		}
	}

	// If skip-prompt is true, do not ask the user for a description.
	if cfg.SkipPrompt {
		return "Changes made by ledit (skipped prompt)", "", "", nil
	}

	// falling back to manual input
	fmt.Print(prompts.EnterDescriptionPrompt(newFilename))
	note, _ = reader.ReadString('\n')
	note = strings.TrimSpace(note)
	generatedDescription = ""

	return note, description, generatedDescription, nil
}

// performAutomatedReview performs an LLM-based code review of the combined diff.
func performAutomatedReview(combinedDiff, originalPrompt, processedInstructions string, cfg *config.Config, logger *utils.Logger, revisionID string) error {
	logger.LogProcessStep("Performing automated code review...")

	review, err := llm.GetCodeReview(cfg, combinedDiff, originalPrompt, processedInstructions)
	if err != nil {
		return fmt.Errorf("failed to get code review from LLM: %w", err)
	}

	switch review.Status {
	case "approved":
		logger.LogProcessStep("Code review approved.")
		logger.LogProcessStep(fmt.Sprintf("Feedback: %s", review.Feedback))
		return nil
	case "needs_revision":
		logger.LogProcessStep("Code review requires revisions.")
		logger.LogProcessStep(fmt.Sprintf("Feedback: %s", review.Feedback))
		logger.LogProcessStep("Applying suggested revisions...")

		// The review gives new instructions. We execute them.
		// This is like a fix.
		_, fixErr := ProcessCodeGeneration("", review.Instructions+" #WS", cfg, "")
		if fixErr != nil {
			return fmt.Errorf("failed to apply review revisions: %w", fixErr)
		}
		// After applying, the next iteration of validation loop will run.
		// We need to signal a failure to re-validate.
		return fmt.Errorf("revisions applied, re-validating. Feedback: %s", review.Feedback)
	case "rejected":
		logger.LogProcessStep("Code review rejected.")
		logger.LogProcessStep(fmt.Sprintf("Feedback: %s", review.Feedback))

		// Rollback the changes first
		rollbackErr := changetracker.RevertChangeByRevisionID(revisionID)
		if rollbackErr != nil {
			logger.LogError(fmt.Errorf("failed to rollback changes for revision %s: %w", revisionID, rollbackErr))
			return fmt.Errorf("changes rejected by automated review, but rollback failed. Feedback: %s. New prompt suggestion: %s. Rollback error: %w", review.Feedback, review.NewPrompt, rollbackErr)
		}

		// Check if we've already retried once
		if cfg.RetryAttemptCount >= 1 {
			return fmt.Errorf("changes rejected by automated review after retry. Feedback: %s. New prompt suggestion: %s", review.Feedback, review.NewPrompt)
		}

		// Increment retry attempt count
		cfg.RetryAttemptCount++

		// Automatically retry with the new prompt
		logger.LogProcessStep(fmt.Sprintf("Retrying code generation with new prompt: %s", review.NewPrompt))
		_, retryErr := ProcessCodeGeneration("", review.NewPrompt, cfg, "")
		if retryErr != nil {
			return fmt.Errorf("retry failed: %w. Original feedback: %s. New prompt: %s", retryErr, review.Feedback, review.NewPrompt)
		}

		// If we get here, the retry was successful
		logger.LogProcessStep("Retry successful.")
		return nil
	default:
		return fmt.Errorf("unknown review status from LLM: %s. Full feedback: %s", review.Status, review.Feedback)
	}
}

func ProcessWorkspaceCodeGeneration(filename, instructions string, cfg *config.Config) (string, error) {
	// Replace any existing #WS or #WORKSPACE tags with a single #WS tag
	re := regexp.MustCompile(`(?i)\s*#(WS|WORKSPACE)\s*$`)
	instructions = re.ReplaceAllString(instructions, "") + " #WS" // Ensure we have a single #WS tag

	return ProcessCodeGeneration(filename, instructions, cfg, "")
}

// ProcessCodeGeneration generates code based on instructions and returns the combined diff for all changed files.
// The full raw LLM response is still recorded in the changelog for auditing.
func ProcessCodeGeneration(filename, instructions string, cfg *config.Config, imagePath string) (string, error) {
	var originalCode string
	var err error
	if filename != "" {
		originalCode, err = filesystem.LoadOriginalCode(filename) // CHANGED: Call filesystem.LoadOriginalCode
		if err != nil {
			return "", err
		}
	}

	processedInstructions, err := ProcessInstructions(instructions, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to process instructions: %w", err)
	}
	// fmt.Print(prompts.ProcessedInstructionsSeparator(processedInstructions))

	requestHash := utils.GenerateRequestHash(processedInstructions)
	updatedCodeFiles, llmResponseRaw, err := getUpdatedCode(originalCode, processedInstructions, filename, cfg, imagePath)
	if err != nil {
		return "", err
	}

	// Record the base revision with the full raw LLM response for auditing
	revisionID, err := changetracker.RecordBaseRevision(requestHash, processedInstructions, llmResponseRaw)
	if err != nil {
		return "", fmt.Errorf("failed to record base revision: %w", err)
	}

	// Handle file updates (write to disk, record individual file changes, git commit)
	// This now returns the combined diff of all changes.
	combinedDiff, err := handleFileUpdates(updatedCodeFiles, revisionID, cfg, instructions, processedInstructions, llmResponseRaw)
	if err != nil {
		return "", err
	}

	return combinedDiff, nil
}

// ProcessPartialEdit performs a targeted edit on a specific file using partial content and instructions
// This is more efficient than full file replacement for small, focused changes
func ProcessPartialEdit(filePath, targetInstructions string, cfg *config.Config, logger *utils.Logger) (string, error) {
	// Read the current file content
	originalContent, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// For Go files, try to identify the relevant function/struct/section to edit
	relevantSection, sectionStart, sectionEnd, err := extractRelevantSection(string(originalContent), targetInstructions, filePath)
	if err != nil {
		logger.Logf("Could not extract relevant section, falling back to full file edit: %v", err)
		return ProcessCodeGeneration(filePath, targetInstructions, cfg, "")
	}

	// Create focused instructions that work with just the relevant section
	partialInstructions := buildPartialEditInstructions(targetInstructions, relevantSection, filePath, sectionStart, sectionEnd)

	// Get the updated section from the LLM
	_, llmResponse, err := getUpdatedCodeSection(relevantSection, partialInstructions, filePath, cfg)
	if err != nil {
		logger.Logf("Partial edit failed, falling back to full file edit: %v", err)
		return ProcessCodeGeneration(filePath, targetInstructions, cfg, "")
	}

	// Extract the updated code from the LLM response
	updatedSection, err := parser.ExtractCodeFromResponse(llmResponse, getLanguageFromExtension(filePath))
	if err != nil || updatedSection == "" {
		logger.Logf("Could not extract updated section from LLM response, falling back to full file edit: %v", err)
		return ProcessCodeGeneration(filePath, targetInstructions, cfg, "")
	}

	// Apply the partial edit to the original file
	updatedContent := applyPartialEdit(string(originalContent), updatedSection, sectionStart, sectionEnd)

	// Write the updated content directly to the file
	err = os.WriteFile(filePath, []byte(updatedContent), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write updated content to %s: %w", filePath, err)
	}

	logger.Logf("Successfully processed partial edit for %s", filePath)

	// Generate a simple diff for user feedback
	diff := fmt.Sprintf("Applied partial edit to %s (section lines %d-%d)", filePath, sectionStart+1, sectionEnd+1)
	return diff, nil
}

// extractRelevantSection identifies the specific section of a file that needs to be edited
// Returns the section content, start line, end line, and any error
func extractRelevantSection(content, instructions, filePath string) (string, int, int, error) {
	lines := strings.Split(content, "\n")
	lang := getLanguageFromExtension(filePath)

	// For Go files, try to identify functions, types, or logical blocks
	if lang == "go" {
		return extractGoSection(lines, instructions)
	}

	// For other languages, use simpler heuristics
	return extractGenericSection(lines, instructions)
}

// extractGoSection extracts relevant Go code sections (functions, types, etc.)
func extractGoSection(lines []string, instructions string) (string, int, int, error) {
	instructionsLower := strings.ToLower(instructions)

	// Handle "top of file" requests specially
	if strings.Contains(instructionsLower, "top of") || strings.Contains(instructionsLower, "beginning of") ||
		strings.Contains(instructionsLower, "start of") {
		// Return the first few lines of the file including package declaration and imports
		endLine := 10 // Get first 10 lines to include package and initial imports
		if len(lines) < endLine {
			endLine = len(lines) - 1
		}
		section := strings.Join(lines[0:endLine+1], "\n")
		return section, 0, endLine, nil
	}

	// Try to find function names mentioned in instructions
	funcPattern := regexp.MustCompile(`func\s+(\w+)`)
	typePattern := regexp.MustCompile(`type\s+(\w+)`)

	for i, line := range lines {
		// Check for function declarations
		if matches := funcPattern.FindStringSubmatch(line); len(matches) > 1 {
			funcName := strings.ToLower(matches[1])
			if strings.Contains(instructionsLower, funcName) {
				// Find the end of this function
				endLine := findGoFunctionEnd(lines, i)
				section := strings.Join(lines[i:endLine+1], "\n")
				return section, i, endLine, nil
			}
		}

		// Check for type declarations
		if matches := typePattern.FindStringSubmatch(line); len(matches) > 1 {
			typeName := strings.ToLower(matches[1])
			if strings.Contains(instructionsLower, typeName) {
				// Find the end of this type
				endLine := findGoTypeEnd(lines, i)
				section := strings.Join(lines[i:endLine+1], "\n")
				return section, i, endLine, nil
			}
		}
	}

	// If no specific function/type found, try to find a logical block
	return extractGenericSection(lines, instructions)
}

// findGoFunctionEnd finds the end line of a Go function starting at startLine
func findGoFunctionEnd(lines []string, startLine int) int {
	braceCount := 0
	foundOpenBrace := false

	for i := startLine; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		for _, char := range line {
			if char == '{' {
				braceCount++
				foundOpenBrace = true
			} else if char == '}' {
				braceCount--
				if foundOpenBrace && braceCount == 0 {
					return i
				}
			}
		}
	}

	// If we couldn't find the end, return a reasonable default
	return startLine + 20 // Arbitrary limit
}

// findGoTypeEnd finds the end line of a Go type declaration starting at startLine
func findGoTypeEnd(lines []string, startLine int) int {
	line := strings.TrimSpace(lines[startLine])

	// If it's a simple type (no braces), it's just one line
	if !strings.Contains(line, "{") {
		return startLine
	}

	// Otherwise, find the matching closing brace
	return findGoFunctionEnd(lines, startLine)
}

// extractGenericSection extracts a relevant section using simple heuristics
func extractGenericSection(lines []string, instructions string) (string, int, int, error) {
	instructionsLower := strings.ToLower(instructions)
	words := strings.Fields(instructionsLower)

	// Look for lines that contain keywords from the instructions
	bestMatch := -1
	bestScore := 0

	for i, line := range lines {
		lineLower := strings.ToLower(line)
		score := 0

		for _, word := range words {
			if len(word) > 3 && strings.Contains(lineLower, word) {
				score++
			}
		}

		if score > bestScore {
			bestScore = score
			bestMatch = i
		}
	}

	if bestMatch == -1 {
		return "", 0, 0, fmt.Errorf("could not find relevant section")
	}

	// Extract a reasonable context around the best match
	start := bestMatch - 5
	if start < 0 {
		start = 0
	}

	end := bestMatch + 15
	if end >= len(lines) {
		end = len(lines) - 1
	}

	section := strings.Join(lines[start:end+1], "\n")
	return section, start, end, nil
}

// buildPartialEditInstructions creates instructions specifically for partial editing
func buildPartialEditInstructions(originalInstructions, sectionContent, filePath string, startLine, endLine int) string {
	// Special handling for top-of-file edits
	if startLine == 0 {
		return fmt.Sprintf(`You are editing the top of %s (lines %d-%d), including the package declaration and initial imports.

ORIGINAL TASK: %s

CURRENT TOP SECTION:
%s

CRITICAL INSTRUCTIONS:
1. Add the requested content at the very top of the file (before package declaration)
2. Keep the package declaration and all existing imports exactly as they are
3. Return ONLY the updated version of this top section
4. Maintain proper indentation and formatting
5. Do NOT include the entire file - just the updated top section

Format your response as:
`+"```"+`go
[updated top section here]
`+"```"+`

Make sure to add the requested content at the very beginning.`, filePath, startLine+1, endLine+1, originalInstructions, sectionContent)
	}

	// Standard partial edit instructions for other sections
	return fmt.Sprintf(`You are editing a specific section of %s (lines %d-%d).

ORIGINAL TASK: %s

CURRENT SECTION TO EDIT:
%s

CRITICAL INSTRUCTIONS:
1. Return ONLY the updated version of this section
2. Maintain proper indentation and formatting
3. Keep line numbers and context consistent
4. Do NOT include the entire file - just the updated section
5. Make ONLY the changes specified in the original task
6. Preserve all existing functionality not being changed

Format your response as:
`+"```"+`go
[updated section here]
`+"```"+`

Focus on making precise, targeted changes to achieve the goal.`, filePath, startLine+1, endLine+1, originalInstructions, sectionContent)
}

// getUpdatedCodeSection gets LLM response for just a section of code
func getUpdatedCodeSection(sectionContent, instructions, filePath string, cfg *config.Config) (string, string, error) {
	return context.GetLLMCodeResponse(cfg, sectionContent, instructions, filePath, "")
}

// applyPartialEdit applies the updated section to the original content
func applyPartialEdit(originalContent, updatedSection string, startLine, endLine int) string {
	lines := strings.Split(originalContent, "\n")

	// Replace the lines from startLine to endLine with the updated section
	before := lines[:startLine]
	after := lines[endLine+1:]

	updatedLines := strings.Split(updatedSection, "\n")

	// Combine: before + updated + after
	result := append(before, updatedLines...)
	result = append(result, after...)

	return strings.Join(result, "\n")
}

// handleTopOfFileEdit handles edits that add content at the top of a file
func handleTopOfFileEdit(filePath, targetInstructions, originalContent string, cfg *config.Config, logger *utils.Logger) (string, error) {
	// Create instructions specifically for adding content at the top
	instructions := fmt.Sprintf(`You need to add content at the very top of the file %s.

ORIGINAL TASK: %s

CURRENT FILE CONTENT (first 10 lines):
%s

INSTRUCTIONS:
1. Add the requested content at the very top of the file
2. Keep all existing content exactly as it is
3. Return the COMPLETE updated file content
4. Make sure the new content is properly formatted for the file type

Format your response as:
`+"```"+`go
[complete updated file content here]
`+"```"+`

The new content should be added at the very beginning, before any existing content.`,
		filePath, targetInstructions, getFirstNLines(originalContent, 10))

	// Get the updated file content from LLM
	_, llmResponse, err := context.GetLLMCodeResponse(cfg, originalContent, instructions, filePath, "")
	if err != nil {
		return "", fmt.Errorf("failed to get LLM response for top-of-file edit: %w", err)
	}

	// Extract the updated code from the LLM response
	updatedContent, err := parser.ExtractCodeFromResponse(llmResponse, getLanguageFromExtension(filePath))
	if err != nil || updatedContent == "" {
		return "", fmt.Errorf("could not extract updated content from LLM response: %w", err)
	}

	// Use the same handleFileUpdates workflow to ensure consistency
	updatedCode := map[string]string{
		filePath: updatedContent,
	}

	// Generate a revision ID for change tracking
	revisionID := fmt.Sprintf("top-edit-%d", time.Now().Unix())

	// Use the standard approval workflow
	diff, err := handleFileUpdates(updatedCode, revisionID, cfg, targetInstructions, targetInstructions, llmResponse)
	if err != nil {
		return "", fmt.Errorf("failed to handle top-of-file updates: %w", err)
	}

	logger.Logf("Successfully processed top-of-file edit for %s", filePath)
	return diff, nil
}

// getFirstNLines returns the first n lines of a string
func getFirstNLines(content string, n int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= n {
		return content
	}
	return strings.Join(lines[:n], "\n")
}
