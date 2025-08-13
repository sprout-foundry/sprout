package editor

import (
	"fmt"
	"os"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/context"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ProcessPartialEdit performs a targeted edit on a specific file using partial content and instructions
// This is more efficient than full file replacement for small, focused changes
func ProcessPartialEdit(filePath, targetInstructions string, cfg *config.Config, logger *utils.Logger) (string, error) {
	// Partial edits are only supported via explicit tool calls. This entry point is
	// reserved for the micro_edit tool path and should not be used by the standard
	// editing flow directly.
	return processPartialEdit(filePath, targetInstructions, cfg, logger)
}

func processPartialEdit(filePath, targetInstructions string, cfg *config.Config, logger *utils.Logger) (string, error) {
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
