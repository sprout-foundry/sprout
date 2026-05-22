package security

import (
	"regexp"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/prompts"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// dataURLPattern matches data URLs (e.g., data:image/png;base64,iVBOR...).
// These are inline embedded resources and should never be treated as secrets.
var dataURLPattern = regexp.MustCompile(`data:[^;,\s]+;base64,[A-Za-z0-9+/=]+`)

// stripDataURLs removes the base64 payload from data URLs, replacing them
// with a placeholder. This prevents the embedded data from triggering false
// positive matches on credential patterns (e.g., JWT-like base64 strings).
func stripDataURLs(content string) string {
	return dataURLPattern.ReplaceAllString(content, "data:[stripped]")
}

// dbUrlRegex matches database/service URLs with credentials potentially in
// the userinfo portion. This is a sprout-specific concern (gitleaks doesn't
// flag bare connection strings) — we treat any remote DB URL as worth
// surfacing to the user since they often carry embedded credentials.
var dbUrlRegex = regexp.MustCompile(`(?i)(jdbc|mongodb|mysql|postgresql|sqlserver|redis|amqp|kafka|mqtt|sftp|ftp|smb|ldap|rdp):\/\/[^\s'"]+`)

// isTestFile reports whether content or path suggests this is a test, demo,
// example, or sample file. Used as a stronger filter on top of gitleaks'
// built-in stopwords for the user-prompt path (Layer 6), where being slightly
// more permissive is preferable to interrupting a developer with a false
// positive every time they edit an example.
func isTestFile(content, filePath string) bool {
	pathLower := strings.ToLower(filePath)
	pathIndicators := []string{
		"test", "example", "demo", "sample", "mock",
		".env.example", "config.example",
	}
	for _, ind := range pathIndicators {
		if strings.Contains(pathLower, ind) {
			return true
		}
	}

	contentLower := strings.ToLower(content)
	contentIndicators := []string{
		"# test", "// test", "/* test", "test_", "_test",
		"# example", "// example", "/* example", "example_",
		"# demo", "// demo", "/* demo", "demo_",
		"# placeholder", "// placeholder", "/* placeholder",
		"# sample", "// sample", "/* sample",
		"# mock", "// mock", "/* mock",
		"PASS:", "FAIL:", "TODO:", "FIXME:",
	}
	for _, ind := range contentIndicators {
		if strings.Contains(contentLower, ind) {
			return true
		}
	}
	return false
}

// concernNameFor returns the user-facing concern label for a gitleaks rule
// ID, suffixed with " Exposure" for readability in the prompt.
func concernNameFor(ruleID string) string {
	return secretdetect.DisplayName(ruleID) + " Exposure"
}

// DetectSecurityConcerns analyzes content for security-related patterns and
// returns a sorted, deduplicated list of concern types plus a map from each
// concern type to its first matched snippet.
func DetectSecurityConcerns(content string) ([]string, map[string]string) {
	return DetectSecurityConcernsWithContext(content, "")
}

// DetectSecurityConcernsWithContext analyzes content with file-path context
// to reduce false positives. The path is consulted via isTestFile to apply
// stricter filtering in obvious test/example files.
func DetectSecurityConcernsWithContext(content, filePath string) ([]string, map[string]string) {
	scanned := stripDataURLs(content)
	isTest := isTestFile(content, filePath)

	concerns := make(map[string]string)

	scanner, err := secretdetect.Default()
	if err == nil && scanner != nil {
		for _, m := range scanner.Scan(scanned) {
			if isTest && looksLikePlaceholder(m.Secret) {
				continue
			}
			name := concernNameFor(m.RuleID)
			if _, ok := concerns[name]; !ok {
				snippet := m.Secret
				if snippet == "" {
					snippet = m.Match
				}
				concerns[name] = snippet
			}
		}
	}

	if match := dbUrlRegex.FindString(scanned); match != "" && !isLocalDBURL(match) {
		const name = "Database/Service Creds Exposure"
		if _, ok := concerns[name]; !ok {
			concerns[name] = match
		}
	}

	if len(concerns) == 0 {
		return nil, concerns
	}
	names := make([]string, 0, len(concerns))
	for n := range concerns {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, concerns
}

// looksLikePlaceholder reports whether a matched secret value reads as a
// human-written placeholder rather than a real key. Used only when context
// indicates a test/example file.
func looksLikePlaceholder(value string) bool {
	if value == "" {
		return true
	}
	v := strings.ToLower(value)
	for _, marker := range []string{
		"test", "demo", "sample", "placeholder", "example",
		"changeme", "your-", "your_", "paste-", "abc", "123",
	} {
		if strings.Contains(v, marker) {
			return true
		}
	}
	return false
}

// isLocalDBURL reports whether a database URL points at local/loopback
// infrastructure and should not be treated as an exposure.
func isLocalDBURL(url string) bool {
	u := strings.ToLower(url)
	for _, marker := range []string{
		"localhost", "127.0.0.1", "test.db", "example.db",
		"://./", "file://", "memory:",
	} {
		if strings.Contains(u, marker) {
			return true
		}
	}
	return false
}

// CheckFileSecurity analyzes a file's content for security concerns,
// prompts the user for confirmation on new detections, and returns
// the updated lists of security concerns and ignored concerns,
// along with a boolean indicating if local summarization should be skipped.
//
// Deprecated: This function has no callers. Use Agent.CheckFileContentSecurity
// instead, which uses the injected ApprovalManager.
func CheckFileSecurity(
	relativePath string,
	fileContent string,
	isNew bool,
	isChanged bool,
	existingSecurityConcerns []string,
	existingIgnoredSecurityConcerns []string,
	cfg *configuration.Config,
	eventBus *events.EventBus,
	userID string,
	promptManager *ApprovalManager,
) (
	updatedSecurityConcerns []string,
	updatedIgnoredSecurityConcerns []string,
	skipLLMSummarization bool,
) {
	logger := utils.GetLogger(false)

	concernsForThisFile := make([]string, 0)
	ignoredConcernsForThisFile := make([]string, 0)

	// If file is unchanged, return existing security concerns and ignored concerns directly.
	if !isNew && !isChanged {
		concernsForThisFile = append(concernsForThisFile, existingSecurityConcerns...)
		ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, existingIgnoredSecurityConcerns...)
		return concernsForThisFile, ignoredConcernsForThisFile, len(concernsForThisFile) > 0
	}

	// For new or changed files, perform detection and user interaction.
	detectedConcernsList, detectedSnippetsMap := DetectSecurityConcernsWithContext(fileContent, relativePath)

	// Build concernsForThisFile and ignoredConcernsForThisFile based on current detection
	// and previous ignore choices.
	for _, detectedConcern := range detectedConcernsList {
		wasPreviouslyIgnored := false
		for _, ignored := range existingIgnoredSecurityConcerns {
			if ignored == detectedConcern {
				wasPreviouslyIgnored = true
				break
			}
		}

		if wasPreviouslyIgnored {
			// If it was previously ignored and is still detected, keep it ignored.
			ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, detectedConcern)
		} else {
			// This concern was either new, or previously marked as an issue.
			// Prompt the user for a decision.
			snippet := detectedSnippetsMap[detectedConcern]
			prompt := prompts.PotentialSecurityConcernsFound(relativePath, detectedConcern, snippet)
			var userResponse bool

			// Try event-based prompting first (for WebUI)
			if eventBus != nil && promptManager != nil {
				userResponse = promptManager.RequestPrompt(eventBus, userID, prompt, true, map[string]string{
					"file_path": relativePath,
					"concern":   detectedConcern,
				})
				logger.Logf("Security concern '%s' in %s user response: %v", detectedConcern, relativePath, userResponse)
			} else {
				// Fall back to CLI-based prompting
				userResponse = logger.AskForConfirmation(prompt, true, false)
			}

			if userResponse { // User chose to mark as an issue
				concernsForThisFile = append(concernsForThisFile, detectedConcern)
				logger.Logf("Security concern '%s' in %s noted as an issue.", detectedConcern, relativePath)
			} else { // User chose to ignore this specific detected concern
				ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, detectedConcern)
				logger.Logf("Security concern '%s' in %s noted as unimportant.", detectedConcern, relativePath)
			}
		}
	}

	sort.Strings(concernsForThisFile)
	sort.Strings(ignoredConcernsForThisFile)

	if len(concernsForThisFile) > 0 {
		skipLLMSummarization = true
		logger.LogProcessStep(prompts.SkippingLLMSummarizationDueToSecurity(relativePath))
	}

	return concernsForThisFile, ignoredConcernsForThisFile, skipLLMSummarization
}
