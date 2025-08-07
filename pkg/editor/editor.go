package editor

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

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

// loadOriginalCode function removed from here. It's moved to pkg/filesystem/loader.go

func ProcessInstructionsWithWorkspace(instructions string, cfg *config.Config) (string, error) {
	// Replace any existing #WS or #WORKSPACE tags with a single #WS tag
	re := regexp.MustCompile(`(?i)\s*#(WS|WORKSPACE)\s*$`)
	instructions = re.ReplaceAllString(instructions, "") + " #WS"

	return ProcessInstructions(instructions, cfg)
}

func ProcessInstructions(instructions string, cfg *config.Config) (string, error) {
	originalInstructions := instructions // Capture original instructions for LLM-generated queries

	// Handle #SG "search query" pattern first
	sgPattern := regexp.MustCompile(`(?s)#SG\s*"(.*?)"`)
	instructions = sgPattern.ReplaceAllStringFunc(instructions, func(match string) string {
		submatches := sgPattern.FindStringSubmatch(match)
		if len(submatches) > 1 {
			query := submatches[1]
			// Always use Jina search regardless of model type
			fmt.Print(prompts.PerformingSearch(query))
			content, err := webcontent.FetchContextFromSearch(query, cfg)
			if err != nil {
				fmt.Print(prompts.SearchError(query, err))
				return ""
			}
			return content
		}
		return match
	})

	// New: Handle #SG when it's not followed by a quoted string, indicating an LLM-generated search query
	// This pattern matches #SG followed by a word boundary, and not followed by optional whitespace and a double quote.
	sgLLMQueryPattern := regexp.MustCompile(`(?s)#SG\b`)
	instructions = sgLLMQueryPattern.ReplaceAllStringFunc(instructions, func(match string) string {
		// Check if this #SG is followed by a quoted string by looking at the context
		matchIndex := strings.Index(instructions, match)
		if matchIndex != -1 {
			afterMatch := instructions[matchIndex+len(match):]
			// If it starts with optional whitespace followed by a quote, skip it
			trimmed := strings.TrimSpace(afterMatch)
			if strings.HasPrefix(trimmed, `"`) {
				return match // Don't replace, let the quoted version handle it
			}
		}

		// Always use Jina search regardless of model type
		fmt.Printf("Ledit is generating a search query using LLM based on your instructions...\n")
		generatedQueries, err := llm.GenerateSearchQuery(cfg, originalInstructions)
		if err != nil {
			fmt.Printf("Error generating search queries with LLM: %v\n", err)
			return ""
		}

		var allFetchedContent strings.Builder
		searchCount := 0
		for _, query := range generatedQueries {
			if searchCount >= 2 { // Limit to a maximum of two searches
				break
			}
			fmt.Printf("Performing LLM-generated search for: %s\n", query)
			content, err := webcontent.FetchContextFromSearch(query, cfg)
			if err != nil {
				fmt.Print(prompts.SearchError(query, err))
				// Continue to the next query even if one fails
				continue
			}
			allFetchedContent.WriteString(content)
			allFetchedContent.WriteString("\n\n") // Add a separator between contents from different searches
			searchCount++
		}
		return allFetchedContent.String()
	})

	filePattern := regexp.MustCompile(`\s+#(\S+)(?:(\d+),(\d+))?`)
	matches := filePattern.FindAllStringSubmatch(instructions, -1)

	fmt.Println("Found patterns:", matches) // Logging the patterns found

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		path := match[1]
		var content string
		var err error

		fmt.Printf("Processing path: %s\n", path) // Logging the path being processed

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
			content, err = filesystem.LoadFileContent(path) // CHANGED: Call filesystem.LoadFileContent
			if err != nil {
				fmt.Print(prompts.FileLoadError(path, err))
				continue
			}
		}
		instructions = strings.Replace(instructions, "#"+match[1], content, 1)
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
			// Handle partial response by merging with original code
			mergedCode, err := mergePartialCode(originalCode, newCode)
			if err != nil {
				return "", fmt.Errorf("failed to merge partial code for %s: %w", newFilename, err)
			}
			newCode = mergedCode
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
		_, fixErr := ProcessCodeGeneration("", review.Instructions, cfg, "")
		if fixErr != nil {
			return fmt.Errorf("failed to apply review revisions: %w", fixErr)
		}
		// After applying, the next iteration of validation loop will run.
		// We need to signal a failure to re-validate.
		return fmt.Errorf("revisions applied, re-validating. Feedback: %s", review.Feedback)
	case "rejected":
		logger.LogProcessStep("Code review rejected.")
		logger.LogProcessStep(fmt.Sprintf("Feedback: %s", review.Feedback))
		// The instruction says "reject the changes and create a more detailed prompt for the code changes to address the issue."
		// We fail the orchestration and tell the user to re-run with the new prompt.
		// we need to actually rollback the changes made in this iteration.
		rollbackErr := changetracker.RevertChangeByRevisionID(revisionID)
		if rollbackErr != nil {
			logger.LogError(fmt.Errorf("failed to rollback changes for revision %s: %w", revisionID, rollbackErr))
			return fmt.Errorf("changes rejected by automated review, but rollback failed. Feedback: %s. New prompt suggestion: %s. Rollback error: %w", review.Feedback, review.NewPrompt, rollbackErr)
		}
		return fmt.Errorf("changes rejected by automated review. Feedback: %s. New prompt suggestion: %s", review.Feedback, review.NewPrompt)
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

// mergePartialCode merges a partial code response with the original code
func mergePartialCode(originalCode, partialCode string) (string, error) {
	if originalCode == "" {
		return partialCode, nil
	}

	lines := strings.Split(partialCode, "\n")
	originalLines := strings.Split(originalCode, "\n")

	var resultLines []string
	lineIndex := 0

	for _, line := range lines {
		if parser.IsPartialContentMarker(line) {
			// Find the next unchanged marker or the end of the block
			nextIndex := findNextUnchangedMarker(lines, lineIndex+1)
			if nextIndex == -1 {
				// If no next marker, append remaining original lines
				if lineIndex < len(originalLines) {
					resultLines = append(resultLines, originalLines[lineIndex:]...)
				}
			} else {
				// Append original lines between current position and next marker position
				if lineIndex < nextIndex && lineIndex < len(originalLines) {
					endIndex := nextIndex
					if endIndex > len(originalLines) {
						endIndex = len(originalLines)
					}
					resultLines = append(resultLines, originalLines[lineIndex:endIndex]...)
				}
				lineIndex = nextIndex
			}
		} else {
			resultLines = append(resultLines, line)
		}
	}

	return strings.Join(resultLines, "\n"), nil
}

// findNextUnchangedMarker finds the next line that contains an unchanged marker
func findNextUnchangedMarker(lines []string, startIndex int) int {
	for i := startIndex; i < len(lines); i++ {
		if parser.IsPartialContentMarker(lines[i]) {
			return i
		}
	}
	return -1
}
