package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// ProcessCodeGeneration generates code based on instructions and returns the combined diff for all changed files.
// The full raw LLM response is still recorded in the changelog for auditing.
func ProcessCodeGeneration(filename, instructions string, cfg *config.Config, imagePath string) (string, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	var originalCode string
	var err error

	// If no filename was provided, try to infer a single explicit target from instructions, e.g., "In file1.txt, ..."
	inferredFilename := ""
	if filename == "" {
		// Match common patterns: In <file>, into <file>, to <file>
		re := regexp.MustCompile(`(?i)\b(?:in|into|to)\s+([\w./-]+\.[A-Za-z0-9]+)\b`)
		if m := re.FindStringSubmatch(instructions); len(m) == 2 {
			inferredFilename = m[1]
		}
	}

	effectiveFilename := filename
	if effectiveFilename == "" && inferredFilename != "" {
		effectiveFilename = inferredFilename
	}

	if effectiveFilename != "" {
		// Ensure the target file exists so downstream logic and LLM have a concrete file
		if _, statErr := os.Stat(effectiveFilename); os.IsNotExist(statErr) {
			// Create empty file and parent directories if needed
			if dir := filepath.Dir(effectiveFilename); dir != "." && dir != "" {
				_ = os.MkdirAll(dir, os.ModePerm)
			}
			_ = os.WriteFile(effectiveFilename, []byte(""), 0644)
		}

		// Load current file content instead of original code for subsequent edits
		// This ensures that review iterations build upon previous changes
		currentBytes, readErr := os.ReadFile(effectiveFilename)
		if readErr != nil {
			// Fallback to original code if current file can't be read
			originalCode, err = filesystem.LoadOriginalCode(effectiveFilename)
			if err != nil {
				return "", err
			}
		} else {
			originalCode = string(currentBytes)
		}
	}

	// Prepend workspace context by default when not targeting a specific file and not skipping
	if effectiveFilename == "" {
		ws := workspace.GetWorkspaceContext(instructions, cfg)
		if ws != "" {
			instructions = ws + "\n\n" + instructions
		}
	}

	// this parses the workspace and filename tags and returns the enriched instructions
	if effectiveFilename == "" {
		instructionsWithWS, err := ProcessInstructionsWithWorkspace(instructions, cfg)
		if err == nil && strings.TrimSpace(instructionsWithWS) != "" {
			instructions = instructionsWithWS
		}
	}

	processedInstructions, err := ProcessInstructions(instructions, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to process instructions: %w", err)
	}

	requestHash := utils.GenerateRequestHash(processedInstructions)
	// Pass the effectiveFilename to guide targeted edits when inferred
	// Indicate streaming when UI is enabled (getUpdatedCode handles LLM; we surface activity in TUI logs via other sinks)
	logger.Log("DEBUG: About to call getUpdatedCode")
	updatedCodeFiles, llmResponseRaw, tokenUsage, err := getUpdatedCode(originalCode, processedInstructions, effectiveFilename, cfg, imagePath)
	// Store token usage in config for later display (even if err != nil)
	if tokenUsage != nil {
		cfg.LastTokenUsage = tokenUsage
	}
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
