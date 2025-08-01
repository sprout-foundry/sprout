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

func ProcessInstructionsWithWorkspace(instructions string, cfg *config.Config) (string, bool, error) {
	// Replace any existing #WS or #WORKSPACE tags with a single #WS tag
	re := regexp.MustCompile(`(?i)\s*#(WS|WORKSPACE)\s*$`)
	instructions = re.ReplaceAllString(instructions, "") + " #WS"

	return ProcessInstructions(instructions, cfg)
}

func ProcessInstructions(instructions string, cfg *config.Config) (string, bool, error) {
	originalInstructions := instructions // Capture original instructions for LLM-generated queries
	useGeminiSearchGrounding := false

	// Check if the editing model is a Gemini model
	isGemini := llm.IsGeminiModel(cfg.EditingModel)

	// Handle #SG "search query" pattern first
	sgPattern := regexp.MustCompile(`(?s)#SG\s*"(.*?)"`)
	instructions = sgPattern.ReplaceAllStringFunc(instructions, func(match string) string {
		submatches := sgPattern.FindStringSubmatch(match)
		if len(submatches) > 1 {
			query := submatches[1]
			if isGemini && cfg.UseGeminiSearchGrounding {
				// If Gemini, just remove the #SG tag. Gemini will handle the search internally.
				useGeminiSearchGrounding = true
				return "" // Remove the tag from instructions
			} else {
				// If not Gemini, use Jina search
				fmt.Print(prompts.PerformingSearch(query)) // Use prompt
				content, err := webcontent.FetchContextFromSearch(query, cfg)
				if err != nil {
					fmt.Print(prompts.SearchError(query, err)) // Use prompt
					return ""
				}
				return content
			}
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

		if isGemini && cfg.UseGeminiSearchGrounding {
			// If Gemini, just remove the #SG tag. Gemini will handle the search internally.
			useGeminiSearchGrounding = true
			return "" // Remove the tag from instructions
		} else {
			// If not Gemini, use Jina search
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
		}
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
			fmt.Println(prompts.LoadingWorkspaceData()) // Use prompt
			content = workspace.GetWorkspaceContext(instructions, cfg)
		} else if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {

			content, err = webcontent.NewWebContentFetcher().FetchWebContent(path, cfg) // Pass cfg here
			if err != nil {
				fmt.Print(prompts.URLFetchError(path, err)) // Use prompt
				continue
			}
		} else {
			content, err = filesystem.LoadFileContent(path) // CHANGED: Call filesystem.LoadFileContent
			if err != nil {
				fmt.Print(prompts.FileLoadError(path, err)) // Use prompt
				continue
			}
		}
		instructions = strings.Replace(instructions, "#"+match[1], content, 1)
	}
	return instructions, useGeminiSearchGrounding, nil
}

// GetLLMCodeResponse function removed from here, as it's now in pkg/context/context_builder.go

func getUpdatedCode(originalCode, instructions, filename string, cfg *config.Config, useGeminiSearchGrounding bool) (map[string]string, string, error) {
	modelName, llmContent, err := context.GetLLMCodeResponse(cfg, originalCode, instructions, filename, useGeminiSearchGrounding) // Updated call site
	if err != nil {
		return nil, "", fmt.Errorf("failed to get LLM response: %w", err)
	}

	fmt.Print(prompts.ModelReturned(modelName, llmContent)) // Use prompt

	updatedCode, err := parser.GetUpdatedCodeFromResponse(llmContent)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse updated code from response: %w", err)
	}
	if len(updatedCode) == 0 {
		fmt.Println(prompts.NoCodeBlocksParsed()) // Use prompt
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

func handleFileUpdates(updatedCode map[string]string, revisionID string, cfg *config.Config, instructions string) error {
	reader := bufio.NewReader(os.Stdin)

	for newFilename, newCode := range updatedCode {
		originalCode, _ := filesystem.LoadOriginalCode(newFilename) // CHANGED: Call filesystem.LoadOriginalCode

		if originalCode == newCode {
			fmt.Print(prompts.NoChangesDetected(newFilename)) // Use prompt
			continue
		}

		color.Yellow(prompts.OriginalFileHeader(newFilename)) // Use prompt
		color.Yellow(prompts.UpdatedFileHeader(newFilename))  // Use prompt
		changetracker.PrintDiff(newFilename, originalCode, newCode)

		applyChanges := false
		editChoice := false
		if cfg.SkipPrompt {
			applyChanges = true
		} else {
			fmt.Print(prompts.ApplyChangesPrompt(newFilename)) // Use prompt
			userInput, _ := reader.ReadString('\n')
			userInput = strings.TrimSpace(strings.ToLower(userInput))
			applyChanges = userInput == "y" || userInput == "yes"
			editChoice = userInput == "e"
		}

		if applyChanges || editChoice {
			if editChoice {
				editedCode, err := OpenInEditor(newCode, filepath.Ext(newFilename))
				if err != nil {
					return fmt.Errorf("error editing file: %w", err)
				}
				newCode = editedCode
			}

			// Ensure the directory exists
			dir := filepath.Dir(newFilename)
			if dir != "" {
				if err := os.MkdirAll(dir, os.ModePerm); err != nil {
					return fmt.Errorf("could not create directory %s: %w", dir, err)
				}
			}

			if err := filesystem.SaveFile(newFilename, newCode); err != nil { // CHANGED: Call filesystem.SaveFile
				return fmt.Errorf("failed to save file: %w", err)
			}

			note, description, commit, err := getChangeSummaries(cfg, newCode, instructions, newFilename, reader)
			if err != nil {
				return fmt.Errorf("failed to get change summaries: %w", err)
			}

			if err := changetracker.RecordChange(revisionID, newFilename, originalCode, newCode, description, note); err != nil {
				return fmt.Errorf("failed to record change: %w", err)
			}
			fmt.Print(prompts.ChangesApplied(newFilename)) // Use prompt

			if cfg.TrackWithGit {
				// get the filename path from the root of the git repository
				filePath, err := git.GetFileGitPath(newFilename) // CHANGED: Call git.GetFileGitPath
				if err != nil {
					return err
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
					return err
				}
			}
		} else {
			fmt.Print(prompts.ChangesNotApplied(newFilename)) // Use prompt
		}
	}
	return nil
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
	fmt.Print(prompts.EnterDescriptionPrompt(newFilename)) // Use prompt
	note, _ = reader.ReadString('\n')
	note = strings.TrimSpace(note)
	generatedDescription = ""

	return note, description, generatedDescription, nil
}

func ProcessWorkspaceCodeGeneration(filename, instructions string, cfg *config.Config) (string, error) {
	// Replace any existing #WS or #WORKSPACE tags with a single #WS tag
	re := regexp.MustCompile(`(?i)\s*#(WS|WORKSPACE)\s*$`)
	instructions = re.ReplaceAllString(instructions, "") + " #WS" // Ensure we have a single #WS tag

	return ProcessCodeGeneration(filename, instructions, cfg)
}

// ProcessCodeGeneration generates code based on instructions and returns the diff for the target file.
// The full raw LLM response is still recorded in the changelog for auditing.
func ProcessCodeGeneration(filename, instructions string, cfg *config.Config) (string, error) {
	var originalCode string
	var err error
	if filename != "" {
		originalCode, err = filesystem.LoadOriginalCode(filename) // CHANGED: Call filesystem.LoadOriginalCode
		if err != nil {
			return "", err
		}
	}

	processedInstructions, useGeminiSearchGrounding, err := ProcessInstructions(instructions, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to process instructions: %w", err)
	}
	fmt.Print(prompts.ProcessedInstructionsSeparator(processedInstructions)) // Use prompt

	requestHash := utils.GenerateRequestHash(processedInstructions)
	updatedCodeFiles, llmResponseRaw, err := getUpdatedCode(originalCode, processedInstructions, filename, cfg, useGeminiSearchGrounding)
	if err != nil {
		return "", err
	}

	// Calculate the diff for the target file (filename)
	var diffForTargetFile string
	if newCode, ok := updatedCodeFiles[filename]; ok {
		diffForTargetFile = changetracker.GetDiff(filename, originalCode, newCode)
	} else {
		// If the LLM did not output the target file, the diff is empty.
		// This indicates the LLM did not produce changes for the expected file.
		diffForTargetFile = ""
	}

	// Record the base revision with the full raw LLM response for auditing
	revisionID, err := changetracker.RecordBaseRevision(requestHash, processedInstructions, llmResponseRaw)
	if err != nil {
		return diffForTargetFile, fmt.Errorf("failed to record base revision: %w", err)
	}

	// Handle file updates (write to disk, record individual file changes, git commit)
	err = handleFileUpdates(updatedCodeFiles, revisionID, cfg, instructions)
	if err != nil {
		return diffForTargetFile, err
	}

	return diffForTargetFile, nil
}
