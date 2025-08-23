//go:build !agent2refactor

package agent

import (
	"strings"
)

// EditStep represents a single granular edit operation
type EditStep struct {
	Description string
	Files       []string
	Changes     string
}

// parsePlanIntoSteps extracts individual edit steps from the planning text
func parsePlanIntoSteps(plan string) []EditStep {
	var steps []EditStep

	// Simple parsing logic - look for numbered steps
	lines := strings.Split(plan, "\n")
	var currentStep *EditStep

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Look for step indicators (Step 1:, 1., - Step, etc.)
		if strings.HasPrefix(strings.ToLower(line), "step ") ||
			(len(line) > 0 && line[0] >= '1' && line[0] <= '9' && strings.Contains(line, ":")) {

			// Save previous step if it exists
			if currentStep != nil && currentStep.Description != "" {
				steps = append(steps, *currentStep)
			}

			// Start new step
			currentStep = &EditStep{
				Description: line,
			}
		} else if currentStep != nil {
			// Add content to current step
			if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
				currentStep.Changes += line + "\n"
			} else if strings.Contains(line, ".go") {
				// Extract file names - check if already exists
				fileExists := false
				for _, existingFile := range currentStep.Files {
					if existingFile == line {
						fileExists = true
						break
					}
				}
				if !fileExists {
					currentStep.Files = append(currentStep.Files, line)
				}
			}
		}
	}

	// Add final step
	if currentStep != nil && currentStep.Description != "" {
		steps = append(steps, *currentStep)
	}

	return steps
}
