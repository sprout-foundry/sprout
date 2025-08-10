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

// isIncompleteTruncatedResponse checks if a response is genuinely incomplete/truncated
// This is much more conservative than the old IsPartialResponse to avoid false positives
func isIncompleteTruncatedResponse(code, filename string) bool {
	// Check for obvious truncation markers that indicate the LLM gave up
	truncationMarkers := []string{
		"... (rest of file unchanged)",
		"... rest of the file ...",
		"... content truncated ...",
		"... full file content ...",
		"[TRUNCATED]",
		"[INCOMPLETE]",
		"// ... (truncated)",
	}

	codeLower := strings.ToLower(code)
	for _, marker := range truncationMarkers {
		if strings.Contains(codeLower, marker) {
			return true
		}
	}

	// Check if the file appears to end abruptly (no proper closing braces for Go files)
	if strings.HasSuffix(filename, ".go") {
		// Count opening and closing braces - if severely unbalanced, likely truncated
		openBraces := strings.Count(code, "{")
		closeBraces := strings.Count(code, "}")

		// Allow some imbalance for partial code, but large imbalances suggest truncation
		if openBraces > closeBraces+5 {
			return true
		}

		// Check if it looks like it ends mid-function (very short and ends with incomplete syntax)
		lines := strings.Split(strings.TrimSpace(code), "\n")
		if len(lines) < 10 { // Very short response
			lastLine := strings.TrimSpace(lines[len(lines)-1])
			// Ends with incomplete syntax patterns
			incompleteSyntax := []string{"{", "if ", "for ", "func ", "var ", "const "}
			for _, syntax := range incompleteSyntax {
				if strings.HasSuffix(lastLine, syntax) {
					return true
				}
			}
		}
	}

	// Default to accepting the response - better to process partial code than loop forever
	return false
}

// isIntentionalPartialCode determines if partial code is intentional vs truncated
func isIntentionalPartialCode(code, instructions string) bool {
	instructionsLower := strings.ToLower(instructions)

	// If instructions specifically ask for partial/targeted changes, accept partial code
	partialIntentKeywords := []string{
		"add function", "add method", "add import", "add constant",
		"modify function", "update function", "change function",
		"add to", "insert", "create function", "new function",
		"add the following", "implement the following",
	}

	for _, keyword := range partialIntentKeywords {
		if strings.Contains(instructionsLower, keyword) {
			return true
		}
	}

	// If the code looks structurally complete for what was asked
	lines := strings.Split(strings.TrimSpace(code), "\n")
	if len(lines) == 0 {
		return false
	}

	// Check if it's a complete function/method/struct
	firstLine := strings.TrimSpace(lines[0])
	lastLine := strings.TrimSpace(lines[len(lines)-1])

	// For Go code, check if it looks like a complete structure
	if strings.HasPrefix(firstLine, "func ") {
		// Should end with } or similar
		return strings.HasSuffix(lastLine, "}") || strings.Contains(lastLine, "return")
	}

	if strings.HasPrefix(firstLine, "type ") {
		// Should end with } for structs/interfaces
		return strings.HasSuffix(lastLine, "}")
	}

	if strings.HasPrefix(firstLine, "import ") || strings.Contains(firstLine, `"`) {
		// Import statements are naturally short and partial
		return true
	}

	if strings.HasPrefix(firstLine, "const ") || strings.HasPrefix(firstLine, "var ") {
		// Variable/constant declarations are naturally short
		return true
	}

	// If none of the above, be conservative and assume it's intentional
	// (better to handle partial code than loop forever)
	return true
}

// getFirstNLines returns the first N lines of content

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
	filePattern := regexp.MustCompile(`\s+#(\S+)(?::(\d+)[-,](\d+))?`)
	matches := filePattern.FindAllStringSubmatch(instructions, -1)
	fmt.Printf("full instructions: %s\n", instructions)
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

		// Check if this is a TRULY incomplete response (not just partial code snippets)
		// We need to be much more conservative here to avoid infinite retry loops
		if isIncompleteTruncatedResponse(newCode, newFilename) {
			// Only retry if the response is genuinely incomplete/truncated
			fmt.Printf("⚠️  Detected incomplete/truncated response for %s. The LLM provided genuinely incomplete code.\n", newFilename)
			fmt.Printf("Requesting the LLM to provide the complete file content...\n")

			// Create a more specific prompt asking for the complete file
			retryPrompt := fmt.Sprintf(`The previous response was incomplete or truncated for the file %s. 
This appears to be a genuine truncation issue (not intentional partial code).

Please provide the ENTIRE file content for %s from beginning to end, including:
- ALL imports and package declarations
- ALL existing functions and methods (both modified and unmodified)
- ALL variable declarations and constants
- ALL comments and documentation
- The specific changes requested in the original instructions

Do NOT use any truncation markers like "... (rest of file unchanged)" or similar.
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
				return "", fmt.Errorf("failed to get complete file content after truncated response: %w", err)
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

	// Smart handling of partial code snippets - don't reject them if they look intentional
	if parser.IsPartialResponse(updatedSection) {
		// Check if this looks like an intentional partial code snippet vs truncation
		if isIntentionalPartialCode(updatedSection, targetInstructions) {
			logger.Logf("LLM provided intentional partial code snippet for targeted edit")
			// Clean and proceed with the partial code
			updatedSection = cleanPartialCodeSnippet(updatedSection)
		} else {
			logger.Logf("LLM provided truncated/incomplete code, falling back to full file edit")
			return ProcessCodeGeneration(filePath, targetInstructions, cfg, "")
		}
	}

	// Apply the partial edit to the original file
	updatedContent := applyPartialEdit(string(originalContent), updatedSection, sectionStart, sectionEnd)

	// Create a revision tracking system like ProcessCodeGeneration
	requestHash := utils.GenerateRequestHash(partialInstructions)
	revisionID, err := changetracker.RecordBaseRevision(requestHash, partialInstructions, llmResponse)
	if err != nil {
		return "", fmt.Errorf("failed to record base revision: %w", err)
	}

	// Create updatedCodeFiles map for handleFileUpdates
	updatedCodeFiles := map[string]string{
		filePath: updatedContent,
	}

	// Use handleFileUpdates to apply changes and trigger automated review
	combinedDiff, err := handleFileUpdates(updatedCodeFiles, revisionID, cfg, targetInstructions, partialInstructions, llmResponse)
	if err != nil {
		return "", err
	}

	logger.Logf("Successfully processed partial edit for %s", filePath)
	return combinedDiff, nil
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
6. Do NOT use placeholder comments like "// existing code" or "// unchanged"

Format your response as:
`+"```"+`go
[updated top section here]
`+"```"+`

Provide ONLY the actual code for this section, no placeholders or truncation markers.`, filePath, startLine+1, endLine+1, originalInstructions, sectionContent)
	}

	// Standard partial edit instructions for other sections
	return fmt.Sprintf(`You are editing a specific section of %s (lines %d-%d).

ORIGINAL TASK: %s

CURRENT SECTION TO EDIT:
`+"```"+`go
%s
`+"```"+`

CRITICAL PARTIAL EDIT INSTRUCTIONS:
1. Make the requested changes to this specific section ONLY
2. Return ONLY the modified version of this section
3. Do NOT include the entire file - just this section with your changes
4. Do NOT use placeholder comments like "// unchanged", "// existing code", or "// rest of file"
5. Do NOT add truncation markers like "..." or "// (content continues)"
6. Provide complete, working code for this section
7. Maintain proper Go syntax and formatting
8. If adding new functions/methods, include them completely
9. If modifying existing functions, include the complete modified function

WHAT TO RETURN:
- Only the specific code section that needs to be changed
- Complete and syntactically correct
- No placeholders or "..." markers
- Ready to be inserted directly into the file

Format your response as:
`+"```"+`go
[complete updated section here]
`+"```"+`

Remember: Return ONLY the actual code for this section, make it complete and functional.`, filePath, startLine+1, endLine+1, originalInstructions, sectionContent)
}

// getUpdatedCodeSection gets LLM response for just a section of code
func getUpdatedCodeSection(sectionContent, instructions, filePath string, cfg *config.Config) (string, string, error) {
	return context.GetLLMCodeResponse(cfg, sectionContent, instructions, filePath, "")
}

// applyPartialEdit applies the updated section to the original content
// Improved version that handles partial code snippets better and cleans up comments
func applyPartialEdit(originalContent, updatedSection string, startLine, endLine int) string {
	lines := strings.Split(originalContent, "\n")

	// Clean the updated section of problematic comments that could cause issues
	cleanedUpdatedSection := cleanPartialCodeSnippet(updatedSection)
	updatedLines := strings.Split(cleanedUpdatedSection, "\n")

	// Validate and adjust the line range to avoid issues
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}
	if startLine > endLine {
		// If range is invalid, try to find a better insertion point
		betterStart, betterEnd := findBetterInsertionPoint(lines, updatedLines, startLine)
		startLine = betterStart
		endLine = betterEnd
	}

	// Replace the lines from startLine to endLine with the updated section
	before := lines[:startLine]
	after := lines[endLine+1:]

	// Combine: before + updated + after
	result := append(before, updatedLines...)
	result = append(result, after...)

	return strings.Join(result, "\n")
}

// cleanPartialCodeSnippet removes problematic comments and markers from partial code
func cleanPartialCodeSnippet(code string) string {
	lines := strings.Split(code, "\n")
	var cleanedLines []string

	for _, line := range lines {
		lineToCheck := strings.ToLower(strings.TrimSpace(line))

		// Skip obvious placeholder comments that could cause issues
		problematicComments := []string{
			"// existing code",
			"// unchanged",
			"// rest of",
			"// other functions",
			"// previous code",
			"// ... (truncated)",
			"/* existing",
			"/* unchanged",
		}

		shouldSkip := false
		for _, problematic := range problematicComments {
			if strings.Contains(lineToCheck, problematic) {
				shouldSkip = true
				break
			}
		}

		if !shouldSkip {
			cleanedLines = append(cleanedLines, line)
		}
	}

	return strings.Join(cleanedLines, "\n")
}

// findBetterInsertionPoint tries to find a more appropriate place to insert code
// when the provided line range is invalid or problematic
func findBetterInsertionPoint(originalLines, updatedLines []string, preferredStart int) (int, int) {
	// If we have function-like content in the update, try to find where it belongs
	firstUpdatedLine := ""
	if len(updatedLines) > 0 {
		firstUpdatedLine = strings.TrimSpace(updatedLines[0])
	}

	// For Go code, try to place functions in appropriate locations
	if strings.HasPrefix(firstUpdatedLine, "func ") {
		// Find other function definitions to place this near
		for i, line := range originalLines {
			if strings.Contains(strings.TrimSpace(line), "func ") && i >= preferredStart {
				// Insert before this function
				return i, i
			}
		}
	}

	// For imports, place with other imports
	if strings.HasPrefix(firstUpdatedLine, "import ") || strings.Contains(firstUpdatedLine, `"`) {
		for i, line := range originalLines {
			if strings.Contains(line, "import") {
				// Insert after existing imports
				return i + 1, i + 1
			}
		}
	}

	// Default: try to place at the end of the file before the last few lines
	safeEnd := len(originalLines) - 3
	if safeEnd < 0 {
		safeEnd = 0
	}

	return safeEnd, safeEnd
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
