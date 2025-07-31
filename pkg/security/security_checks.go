package security

import (
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// DetectSecurityConcerns analyzes the given content for common security-related patterns.
// This is a simplified example and should be expanded with more robust detection logic.
func DetectSecurityConcerns(content string) []string {
	var concerns []string
	contentLower := strings.ToLower(content)

	// Example patterns (these are very basic and need to be comprehensive for real use)
	if strings.Contains(contentLower, "api_key") || strings.Contains(contentLower, "apikey") {
		concerns = append(concerns, "API Key Exposure")
	}
	if strings.Contains(contentLower, "password") || strings.Contains(contentLower, "passwd") {
		concerns = append(concerns, "Password Exposure")
	}
	if strings.Contains(contentLower, "secret") {
		concerns = append(concerns, "Secret Exposure")
	}
	if strings.Contains(contentLower, "database_url") || strings.Contains(contentLower, "db_url") {
		concerns = append(concerns, "Database Creds")
	}
	if strings.Contains(contentLower, "ssh_private_key") || strings.Contains(contentLower, "id_rsa") {
		concerns = append(concerns, "SSH Private Key Exposure")
	}
	if strings.Contains(contentLower, "aws_access_key_id") || strings.Contains(contentLower, "aws_secret_access_key") {
		concerns = append(concerns, "AWS Credentials Exposure")
	}
	if strings.Contains(contentLower, "exec(") || strings.Contains(contentLower, "system(") {
		concerns = append(concerns, "Arbitrary Command Execution")
	}
	if strings.Contains(contentLower, "eval(") {
		concerns = append(concerns, "Code Injection Vulnerability")
	}

	// Deduplicate concerns
	uniqueConcerns := make(map[string]bool)
	var result []string
	for _, c := range concerns {
		if !uniqueConcerns[c] {
			uniqueConcerns[c] = true
			result = append(result, c)
		}
	}
	sort.Strings(result)
	return result
}

// CheckFileSecurity analyzes a file's content for security concerns,
// prompts the user for confirmation on new detections, and returns
// the updated lists of security concerns and ignored concerns,
// along with a boolean indicating if LLM summarization should be skipped.
func CheckFileSecurity(
	relativePath string,
	fileContent string,
	isNew bool,
	isChanged bool,
	existingSecurityConcerns []string,
	existingIgnoredSecurityConcerns []string,
	cfg *config.Config,
) (
	updatedSecurityConcerns []string,
	updatedIgnoredSecurityConcerns []string,
	skipLLMSummarization bool,
) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	var detectedConcerns []string
	concernsForThisFile := make([]string, 0)
	ignoredConcernsForThisFile := make([]string, 0)

	// If file exists and is changed, start with previously ignored concerns
	if !isNew && isChanged {
		ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, existingIgnoredSecurityConcerns...)
	} else if !isNew && !isChanged {
		// File is unchanged. Use existing security concerns and ignored concerns.
		concernsForThisFile = append(concernsForThisFile, existingSecurityConcerns...)
		ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, existingIgnoredSecurityConcerns...)
		// No new detection needed, return current state
		return concernsForThisFile, ignoredConcernsForThisFile, len(concernsForThisFile) > 0
	}

	// Only run security detection for new or changed files
	detectedConcerns = DetectSecurityConcerns(fileContent)

	// Filter out concerns that were previously ignored
	newlyDetectedConcerns := []string{}
	for _, concern := range detectedConcerns {
		isAlreadyIgnored := false
		for _, ignored := range ignoredConcernsForThisFile {
			if ignored == concern {
				isAlreadyIgnored = true
				break
			}
		}
		if !isAlreadyIgnored {
			newlyDetectedConcerns = append(newlyDetectedConcerns, concern)
		}
	}

	// Prompt user for newly detected, unignored concerns
	for _, concern := range newlyDetectedConcerns {
		prompt := prompts.SecurityConcernDetectedPrompt(relativePath, concern)
		if logger.AskForConfirmation(prompt, true) { // This is a required check
			concernsForThisFile = append(concernsForThisFile, concern)
			logger.Logf("Security concern '%s' in %s noted as an issue.", concern, relativePath)
		} else {
			ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, concern)
			logger.Logf("Security concern '%s' in %s noted as unimportant.", concern, relativePath)
		}
	}

	// Add back any concerns that were previously marked as issues and are still detected
	// This applies only if the file content changed, as for unchanged files, concernsForThisFile already holds them.
	if !isNew && isChanged {
		for _, prevConcern := range existingSecurityConcerns {
			isStillDetected := false
			for _, currentDetected := range detectedConcerns { // `detectedConcerns` is fresh for new/changed files
				if prevConcern == currentDetected {
					isStillDetected = true
					break
				}
			}
			if isStillDetected {
				// Ensure it's not already added to concernsForThisFile (e.g., if it was also in newlyDetectedConcerns)
				found := false
				for _, c := range concernsForThisFile {
					if c == prevConcern {
						found = true
						break
					}
				}
				if !found {
					concernsForThisFile = append(concernsForThisFile, prevConcern)
				}
			}
		}
	}

	sort.Strings(concernsForThisFile)
	sort.Strings(ignoredConcernsForThisFile)

	// If there are confirmed security concerns, mark for skipping LLM summarization
	if len(concernsForThisFile) > 0 {
		skipLLMSummarization = true
		logger.LogProcessStep(prompts.SkippingLLMSummarizationDueToSecurity(relativePath))
	}

	return concernsForThisFile, ignoredConcernsForThisFile, skipLLMSummarization
}
