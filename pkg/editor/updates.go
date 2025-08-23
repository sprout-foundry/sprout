package editor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/codereview"
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

	// Track whether we already ran a pre-apply review to avoid double-review
	ranPreapplyReview := false

	// Collect edits first to enable a combined review across the entire changeset
	type preparedEdit struct {
		filename     string
		originalCode string // Original code for change tracking
		currentCode  string // Current state of the file
		newCode      string // New code to be applied
	}
	var edits []preparedEdit

	for newFilename, newCode := range updatedCode {
		// Load original code for diff tracking and change recording
		originalCode, _ := filesystem.LoadOriginalCode(newFilename)

		// Load current file content to determine what changes have been made
		currentFileBytes, currentReadErr := os.ReadFile(newFilename)
		var currentCode string
		if currentReadErr == nil {
			currentCode = string(currentFileBytes)
		} else {
			// If current file can't be read, use original as baseline
			currentCode = originalCode
		}

		// Check if there are meaningful changes from the current state
		if currentCode == newCode {
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

		// Show diff from current state to new state for better understanding of incremental changes
		diff := changetracker.GetDiff(newFilename, currentCode, newCode)
		if diff == "" {
			ui.Out().Print("No changes detected from current state.")
		} else {
			ui.Out().Print(diff)
		}
		allDiffs.WriteString(diff)
		allDiffs.WriteString("\n")

		// Queue the edit for post-review application
		edits = append(edits, preparedEdit{
			filename:     newFilename,
			originalCode: originalCode,
			currentCode:  currentCode,
			newCode:      newCode,
		})
	}

	// Run a single pre-apply automated review across the combined diff to consider all files together
	if cfg.PreapplyReview && (cfg.SkipPrompt || cfg.FromAgent) {
		combined := allDiffs.String()
		if combined != "" {
			logger := utils.GetLogger(cfg.SkipPrompt)
			ranPreapplyReview = true
			if err := performAutomatedReview(combined, originalInstructions, processedInstructions, cfg, logger, revisionID); err != nil {
				// Check if this is a retry request error
				if retryRequest, ok := err.(*codereview.RetryRequestError); ok {
					logger.LogProcessStep(fmt.Sprintf("Code review requested retry with refined prompt: %s", retryRequest.Feedback))
					// For pre-apply review, use the refined prompt to regenerate
					logger.LogProcessStep(fmt.Sprintf("Retrying pre-apply review with refined prompt: %s", retryRequest.RefinedPrompt))
					// Use the refined prompt as the new processed instructions for retry
					processedInstructions = retryRequest.RefinedPrompt
					// Retry the review with the refined prompt
					if retryErr := performAutomatedReview(combined, originalInstructions, processedInstructions, cfg, logger, revisionID); retryErr != nil {
						logger.LogProcessStep(fmt.Sprintf("Retry failed: %v", retryErr))
						// If retry fails, continue with original error handling
						if !strings.Contains(retryErr.Error(), "revisions applied, re-validating") {
							return "", retryErr
						}
					} else {
						logger.LogProcessStep("Pre-apply review retry completed successfully")
					}
				} else if !strings.Contains(err.Error(), "revisions applied, re-validating") {
					return "", err
				}
			}
		}
	}

	// Apply queued edits per file (prompt/write/record)
	for _, e := range edits {
		newFilename := e.filename
		originalCode := e.originalCode
		_ = e.currentCode // Available for future enhancements
		newCode := e.newCode

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
			if err := os.MkdirAll(filepath.Dir(newFilename), os.ModePerm); err != nil {
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

			note, description, commit, err := GetChangeSummaries(cfg, newCode, originalInstructions, newFilename, reader)
			if err != nil {
				return "", fmt.Errorf("failed to get change summaries: %w", err)
			}

			// Use the passed llmResponseRaw directly for llmMessage
			llmMessage := llmResponseRaw

			// Record the change from original to final state for proper change tracking
			// This ensures the changelog shows the complete transformation
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
	if cfg.SkipPrompt && !ranPreapplyReview {
		combinedDiff := allDiffs.String()
		if combinedDiff != "" {
			logger := utils.GetLogger(cfg.SkipPrompt)
			reviewErr := performAutomatedReview(combinedDiff, originalInstructions, processedInstructions, cfg, logger, revisionID)
			if reviewErr != nil {
				// Check if this is a retry request error
				if retryRequest, ok := reviewErr.(*codereview.RetryRequestError); ok {
					logger.LogProcessStep(fmt.Sprintf("Code review requested retry with refined prompt: %s", retryRequest.Feedback))
					// For post-apply review, attempt retry with refined prompt
					logger.LogProcessStep(fmt.Sprintf("Retrying post-apply review with refined prompt: %s", retryRequest.RefinedPrompt))
					// Use the refined prompt for the retry
					refinedInstructions := retryRequest.RefinedPrompt
					if retryErr := performAutomatedReview(combinedDiff, originalInstructions, refinedInstructions, cfg, logger, revisionID); retryErr != nil {
						logger.LogProcessStep(fmt.Sprintf("Post-apply retry failed: %v", retryErr))
						// If retry fails, continue with current changes
						logger.LogProcessStep("Continuing with current changes after failed retry")
					} else {
						logger.LogProcessStep("Post-apply review retry completed successfully")
					}
				} else if !strings.Contains(reviewErr.Error(), "revisions applied, re-validating") {
					return "", reviewErr
				}
			}
		}
	}

	return allDiffs.String(), nil
}
