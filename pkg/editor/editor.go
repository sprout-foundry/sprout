package editor

import (
	"bufio"
	"fmt"
	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webcontent"
	"github.com/alantheprice/ledit/pkg/workspace"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fatih/color"
)

func processInstructions(instructions string, cfg *config.Config) (string, error) {

	// Handle #SG "search query" pattern first
	sgPattern := regexp.MustCompile(`(?s)#SG\s*"(.*?)"`)
	instructions = sgPattern.ReplaceAllStringFunc(instructions, func(match string) string {
		submatches := sgPattern.FindStringSubmatch(match)
		if len(submatches) > 1 {
			query := submatches[1]
			fmt.Printf(prompts.PerformingSearch(query)) // Use prompt
			content, err := webcontent.FetchContextFromSearch(query, cfg)
			if err != nil {
				fmt.Printf(prompts.SearchError(query, err)) // Use prompt
				return ""
			}
			return content
		}
		return match
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

			content, err = webcontent.NewWebContentFetcher().FetchWebContent(path)
			if err != nil {
				fmt.Printf(prompts.URLFetchError(path, err)) // Use prompt
				continue
			}
		} else {
			content, err = LoadFileContent(path)
			if err != nil {
				fmt.Printf(prompts.FileLoadError(path, err)) // Use prompt
				continue
			}
		}
		instructions = strings.Replace(instructions, "#"+match[1], content, 1)
	}
	return instructions, nil
}

func getUpdatedCode(originalCode, instructions, filename string, cfg *config.Config) (map[string]string, string, error) {
	modelName, llmContent, err := llm.GetLLMCodeResponse(cfg, originalCode, instructions, filename)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get LLM response: %w", err)
	}

	fmt.Printf(prompts.ModelReturned(modelName, llmContent)) // Use prompt

	updatedCode, err := parser.GetUpdatedCodeFromResponse(llmContent)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse updated code from response: %w", err)
	}
	if len(updatedCode) == 0 {
		fmt.Println(prompts.NoCodeBlocksParsed()) // Use prompt
	}
	return updatedCode, llmContent, nil
}

func parseCommitMessage(commitMessage string, attempts int) (string, string, error) {
	lines := strings.Split(commitMessage, "\n")
	if len(lines) < 2 {
		return "", "", fmt.Errorf("failed to parse commit message")
	}

	note := lines[0]
	description := strings.Join(lines[2:], "\n")
	return note, description, nil
}

func handleFileUpdates(updatedCode map[string]string, revisionID string, cfg *config.Config, instructions string) error {
	reader := bufio.NewReader(os.Stdin)

	for newFilename, newCode := range updatedCode {
		originalCode, _ := loadOriginalCode(newFilename)

		if originalCode == newCode {
			fmt.Printf(prompts.NoChangesDetected(newFilename)) // Use prompt
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
			fmt.Printf(prompts.ApplyChangesPrompt(newFilename)) // Use prompt
			userInput, _ := reader.ReadString('\n')
			userInput = strings.TrimSpace(strings.ToLower(userInput))
			applyChanges = userInput == "y" || userInput == "yes"
			editChoice = userInput == "e"
		}

		if applyChanges || editChoice {
			if editChoice {
				tempFile, err := os.CreateTemp("", "ledit-*.py")
				if err != nil {
					return fmt.Errorf("could not create temp file: %w", err)
				}
				defer os.Remove(tempFile.Name())

				if _, err := tempFile.WriteString(newCode); err != nil {
					return fmt.Errorf("could not write to temp file: %w", err)
				}
				tempFile.Close()

				editor := os.Getenv("EDITOR")
				if editor == "" {
					editor = "vim" // A reasonable default
				}
				cmd := exec.Command(editor, tempFile.Name())
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("error running editor: %w", err)
				}

				editedCode, err := os.ReadFile(tempFile.Name())
				if err != nil {
					return fmt.Errorf("could not read edited file: %w", err)
				}
				newCode = string(editedCode)
			}

			// Ensure the directory exists
			dir := filepath.Dir(newFilename)
			if dir != "" {
				if err := os.MkdirAll(dir, os.ModePerm); err != nil {
					return fmt.Errorf("could not create directory %s: %w", dir, err)
				}
			}

			if err := utils.SaveFile(newFilename, newCode); err != nil {
				return fmt.Errorf("failed to save file: %w", err)
			}

			note, description, commit, err := getChangeSummaries(cfg, newCode, instructions, newFilename, reader)
			if err != nil {
				return fmt.Errorf("failed to get change summaries: %w", err)
			}

			if err := changetracker.RecordChange(revisionID, newFilename, originalCode, newCode, description, note); err != nil {
				return fmt.Errorf("failed to record change: %w", err)
			}
			fmt.Printf(prompts.ChangesApplied(newFilename)) // Use prompt

			if cfg.TrackWithGit {
				// get the filename path from the root of the git repository
				filePath, err := getFileGitPath(newFilename)
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

				if err := addAndCommitFile(newFilename, commitMessage); err != nil {
					return err
				}
			}
		} else {
			fmt.Printf(prompts.ChangesNotApplied(newFilename)) // Use prompt
		}
	}
	return nil
}

func getChangeSummaries(cfg *config.Config, newCode string, instructions string, newFilename string, reader *bufio.Reader) (string, string, string, error) {
	note := "Changes made by ledit based on LLM suggestions."
	description := ""
	generatedDescription, err := llm.GetCommitMessage(cfg, newCode, instructions, newFilename)
	if err == nil && generatedDescription != "" {
		note, description, err := parseCommitMessage(generatedDescription, 0)
		if err == nil {
			return note, description, generatedDescription, nil
		}
	}
	// It failed in the process, lets try one more time.
	generatedDescription, err = llm.GetCommitMessage(cfg, newCode, instructions, newFilename)
	if err == nil && generatedDescription != "" {
		note, description, err := parseCommitMessage(generatedDescription, 0)
		if err == nil {
			return note, description, generatedDescription, nil
		}
	}
	// falling back to manual input
	fmt.Printf(prompts.EnterDescriptionPrompt(newFilename)) // Use prompt
	note, _ = reader.ReadString('\n')
	note = strings.TrimSpace(note)
	generatedDescription = ""

	return note, description, generatedDescription, nil
}

// ProcessCodeGeneration generates code based on instructions and returns the diff for the target file.
// The full raw LLM response is still recorded in the changelog for auditing.
func ProcessCodeGeneration(filename, instructions string, cfg *config.Config) (string, error) {
	var originalCode string
	var err error
	if filename != "" {
		originalCode, err = loadOriginalCode(filename)
		if err != nil {
			return "", err
		}
	}

	processedInstructions, err := processInstructions(instructions, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to process instructions: %w", err)
	}
	fmt.Printf(prompts.ProcessedInstructionsSeparator(processedInstructions)) // Use prompt

	requestHash := utils.GenerateRequestHash(processedInstructions)
	updatedCodeFiles, llmResponseRaw, err := getUpdatedCode(originalCode, processedInstructions, filename, cfg)
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