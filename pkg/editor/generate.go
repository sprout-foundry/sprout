package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ProcessCodeGeneration generates code based on instructions and returns the combined diff for all changed files.
// The full raw LLM response is still recorded in the changelog for auditing.
func ProcessCodeGeneration(filename, instructions string, cfg *config.Config, imagePath string) (string, error) {
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
		originalCode, err = filesystem.LoadOriginalCode(effectiveFilename)
		if err != nil {
			return "", err
		}
	}

	processedInstructions, err := ProcessInstructions(instructions, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to process instructions: %w", err)
	}

	requestHash := utils.GenerateRequestHash(processedInstructions)
	// Pass the effectiveFilename to guide targeted edits when inferred
	// Indicate streaming when UI is enabled (getUpdatedCode handles LLM; we surface activity in TUI logs via other sinks)
	updatedCodeFiles, llmResponseRaw, err := getUpdatedCode(originalCode, processedInstructions, effectiveFilename, cfg, imagePath)
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
