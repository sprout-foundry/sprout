package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/context"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// EditingOperationResult contains the result of an editing operation with rollback information
type EditingOperationResult struct {
	Diff       string `json:"diff"`
	RevisionID string `json:"revision_id"`
}

// ProcessCodeGenerationWithRollback generates code and returns revision ID for rollback
func ProcessCodeGenerationWithRollback(filename, instructions string, cfg *config.Config, imagePath string) (*EditingOperationResult, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	var originalCode string
	var err error

	// If no filename was provided, try to infer a single explicit target from instructions
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
		currentBytes, readErr := os.ReadFile(effectiveFilename)
		if readErr != nil {
			// Fallback to original code if current file can't be read
			originalCode, err = filesystem.LoadOriginalCode(effectiveFilename)
			if err != nil {
				return nil, err
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

	// Process workspace and filename tags
	if effectiveFilename == "" {
		instructionsWithWS, err := ProcessInstructionsWithWorkspace(instructions, cfg)
		if err == nil && strings.TrimSpace(instructionsWithWS) != "" {
			instructions = instructionsWithWS
		}
	}

	processedInstructions, err := ProcessInstructions(instructions, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to process instructions: %w", err)
	}

	requestHash := utils.GenerateRequestHash(processedInstructions)
	logger.Log("DEBUG: About to call getUpdatedCode")
	updatedCodeFiles, llmResponseRaw, tokenUsage, err := getUpdatedCode(originalCode, processedInstructions, effectiveFilename, cfg, imagePath)

	// Store token usage in config for later display
	if tokenUsage != nil {
		cfg.LastTokenUsage = tokenUsage
	}
	if err != nil {
		return nil, err
	}

	// Record the base revision with the full raw LLM response for auditing
	revisionID, err := changetracker.RecordBaseRevision(requestHash, processedInstructions, llmResponseRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to record base revision: %w", err)
	}

	// Handle file updates and get the diff
	combinedDiff, err := handleFileUpdates(updatedCodeFiles, revisionID, cfg, instructions, processedInstructions, llmResponseRaw)
	if err != nil {
		return nil, err
	}

	return &EditingOperationResult{
		Diff:       combinedDiff,
		RevisionID: revisionID,
	}, nil
}

// ProcessPartialEditWithRollback performs a partial edit and returns revision ID for rollback
func ProcessPartialEditWithRollback(filePath, targetInstructions string, cfg *config.Config, logger *utils.Logger) (*EditingOperationResult, error) {
	// Read the current file content
	originalContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// For Go files, try to identify the relevant function/struct/section to edit
	relevantSection, sectionStart, sectionEnd, err := extractRelevantSection(string(originalContent), targetInstructions, filePath)
	if err != nil {
		logger.Logf("Could not extract relevant section, falling back to full file edit: %v", err)
		return ProcessCodeGenerationWithRollback(filePath, targetInstructions, cfg, "")
	}

	// Create focused instructions that work with just the relevant section
	partialInstructions := buildPartialEditInstructions(targetInstructions, relevantSection, filePath, sectionStart, sectionEnd)

	// Get the updated section from the LLM
	_, llmResponse, err := getUpdatedCodeSection(relevantSection, partialInstructions, filePath, cfg)
	if err != nil {
		logger.Logf("Partial edit failed, falling back to full file edit: %v", err)
		return ProcessCodeGenerationWithRollback(filePath, targetInstructions, cfg, "")
	}

	// Extract the updated code from the LLM response
	updatedSection, err := parser.ExtractCodeFromResponse(llmResponse, getLanguageFromExtension(filePath))
	if err != nil || updatedSection == "" {
		logger.Logf("Could not extract updated section from LLM response, falling back to full file edit: %v", err)
		return ProcessCodeGenerationWithRollback(filePath, targetInstructions, cfg, "")
	}

	// Smart handling of partial code snippets
	if parser.IsPartialResponse(updatedSection) {
		if isIntentionalPartialCode(updatedSection, targetInstructions) {
			logger.Logf("LLM provided intentional partial code snippet for targeted edit")
			updatedSection = cleanPartialCodeSnippet(updatedSection)
		} else {
			logger.Logf("LLM provided truncated/incomplete code, falling back to full file edit")
			return ProcessCodeGenerationWithRollback(filePath, targetInstructions, cfg, "")
		}
	}

	// Apply the partial edit to the original file
	updatedContent := applyPartialEdit(string(originalContent), updatedSection, sectionStart, sectionEnd)

	// Three-way merge guard
	currentBytes, _ := os.ReadFile(filePath)
	current := string(currentBytes)
	merged, hadConflicts, mErr := ApplyThreeWayMerge(string(originalContent), current, updatedContent)
	if mErr == nil && merged != "" {
		updatedContent = merged
	} else if hadConflicts {
		return nil, fmt.Errorf("merge conflict applying partial edit to %s: %v", filePath, mErr)
	}

	// Create a revision tracking system
	requestHash := utils.GenerateRequestHash(partialInstructions)
	revisionID, err := changetracker.RecordBaseRevision(requestHash, partialInstructions, llmResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to record base revision: %w", err)
	}

	// Create updatedCodeFiles map for handleFileUpdates
	updatedCodeFiles := map[string]string{
		filePath: updatedContent,
	}

	// Use handleFileUpdates to apply changes and trigger automated review
	combinedDiff, err := handleFileUpdates(updatedCodeFiles, revisionID, cfg, targetInstructions, partialInstructions, llmResponse)
	if err != nil {
		return nil, err
	}

	logger.Logf("Successfully processed partial edit for %s", filePath)
	return &EditingOperationResult{
		Diff:       combinedDiff,
		RevisionID: revisionID,
	}, nil
}

// cleanPartialCodeSnippet cleans up a partial code snippet for insertion
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

// getUpdatedCodeSection gets LLM response for just a section of code (implementation from partial_edit.go)
func getUpdatedCodeSection(sectionContent, instructions, filePath string, cfg *config.Config) (string, string, error) {
	_, response, _, err := context.GetLLMCodeResponse(cfg, sectionContent, instructions, filePath, "")
	return response, "", err
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

	// Replace the lines from startLine to endLine with the updated section (idempotent guard)
	before := lines[:startLine]
	after := lines[endLine+1:]

	// Idempotent guard: avoid duplicate insertion if updated already present
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, cleanedUpdatedSection) {
		return joined
	}

	// Combine: before + updated + after
	result := append(before, updatedLines...)
	result = append(result, after...)

	return strings.Join(result, "\n")
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
	if strings.HasPrefix(firstUpdatedLine, "import ") || strings.Contains(firstUpdatedLine, "\"") {
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
