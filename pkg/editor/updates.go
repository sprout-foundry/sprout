package editor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/git"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"

	"github.com/fatih/color"
)

// handleFileUpdates writes updated files, manages staged edits, records changes, and optionally commits via git.
func handleFileUpdates(updatedCode map[string]string, revisionID string, cfg *config.Config, originalInstructions string, processedInstructions string, llmResponseRaw string) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	var allDiffs strings.Builder

	for newFilename, newCode := range updatedCode {
		originalCode, _ := filesystem.LoadOriginalCode(newFilename)

		if originalCode == newCode {
			ui.Out().Print(prompts.NoChangesDetected(newFilename))
			continue
		}

		// Check if this is a TRULY incomplete response (not just partial code snippets)
		if isIncompleteTruncatedResponse(newCode, newFilename) {
			ui.Out().Printf("⚠️  Detected incomplete/truncated response for %s. The LLM provided genuinely incomplete code.\n", newFilename)
			ui.Out().Printf("Requesting the LLM to provide the complete file content...\n")

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

			retryResult, err := ProcessCodeGeneration(newFilename, retryPrompt, cfg, "")
			if err != nil {
				return "", fmt.Errorf("failed to get complete file content after truncated response: %w", err)
			}

			if retryResult != "" {
				ui.Out().Printf("✅ Received complete file content for %s\n", newFilename)
				continue
			} else {
				return "", fmt.Errorf("failed to get complete file content for %s after retry", newFilename)
			}
		}

		color.Yellow(prompts.OriginalFileHeader(newFilename))
		color.Yellow(prompts.UpdatedFileHeader(newFilename))

		// Three-way merge to avoid stomping concurrent changes
		currentBytes, _ := os.ReadFile(newFilename)
		current := string(currentBytes)
		merged, hadConflicts, mErr := ApplyThreeWayMerge(originalCode, current, newCode)
		if mErr != nil && hadConflicts {
			return "", fmt.Errorf("merge conflict applying changes to %s: %v", newFilename, mErr)
		}
		if merged != "" {
			newCode = merged
		}

		diff := changetracker.GetDiff(newFilename, originalCode, newCode)
        if diff == "" {
            ui.Out().Print("No changes detected.")
        } else {
			ui.Out().Print(diff)
		}
		allDiffs.WriteString(diff)
		allDiffs.WriteString("\n")

		// Pre-apply diff review when enabled
        if cfg.PreapplyReview && diff != "" {
			logger := utils.GetLogger(cfg.SkipPrompt)
			if err := performAutomatedReview(diff, originalInstructions, processedInstructions, cfg, logger, revisionID); err != nil {
				// If review requested revisions, allow subsequent iterations to handle it,
				// but do not abort the current write path for initial creation edits.
				if !strings.Contains(err.Error(), "revisions applied, re-validating") {
					return "", err
				}
			}
		}

		applyChanges := false
		editChoice := false
		if cfg.SkipPrompt {
			applyChanges = true
		} else {
			ui.Out().Print(prompts.ApplyChangesPrompt(newFilename))
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
			if err := ensureDir(newFilename); err != nil {
				return "", fmt.Errorf("could not create directory for %s: %w", newFilename, err)
			}

			// Staged edits
			if cfg.StagedEdits {
				stageRoot := filepath.Join(".ledit", "stage")
				if err := prepareStagedWorkspace(stageRoot); err != nil {
					return "", fmt.Errorf("failed to prepare staged workspace: %w", err)
				}
				stagedPath := filepath.Join(stageRoot, filepath.Clean(newFilename))
				if err := os.MkdirAll(filepath.Dir(stagedPath), os.ModePerm); err != nil {
					return "", fmt.Errorf("could not create staged directory %s: %w", filepath.Dir(stagedPath), err)
				}
				if cfg.DryRun || os.Getenv("LEDIT_DRY_RUN") == "1" {
					ui.Out().Printf("[dry-run] Skipping staged write for %s\n", stagedPath)
				} else if err := os.WriteFile(stagedPath, []byte(newCode), 0644); err != nil {
					return "", fmt.Errorf("failed to save staged file: %w", err)
				}
				if err := writeStageManifest(stageRoot, []string{newFilename}); err != nil {
					return "", err
				}
			} else {
				// Dry-run mode: skip write
				if cfg.DryRun || os.Getenv("LEDIT_DRY_RUN") == "1" {
					ui.Out().Printf("[dry-run] Skipping write for %s\n", newFilename)
				} else if err := filesystem.SaveFile(newFilename, newCode); err != nil {
					return "", fmt.Errorf("failed to save file: %w", err)
				}
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
			ui.Out().Print(prompts.ChangesApplied(newFilename))

			if cfg.TrackWithGit {
				filePath, err := git.GetFileGitPath(newFilename)
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

				if err := git.AddAndCommitFile(newFilename, commitMessage); err != nil {
					return "", err
				}
			}
		} else {
			ui.Out().Print(prompts.ChangesNotApplied(newFilename))
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
