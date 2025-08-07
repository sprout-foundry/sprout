package security

import (
	"regexp"
	"sort"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// Define regex patterns for security concerns
var (
	// API Key/Token patterns: looks for common key names followed by assignment and a string of 16-128 alphanumeric/symbol chars
	// This is a more robust pattern to avoid false positives from just "key" or "token"
	apiKeyRegex = regexp.MustCompile(`(?i)(api_key|apikey|api-key|access_key|access-key|secret_key|secret-key|auth_token|auth-token|bearer_token|bearer-token|client_secret|client-secret|consumer_key|consumer-key|consumer_secret|consumer-secret|private_key|private-key|public_key|public-key|token|key|secret|client_id|app_id|api_secret|auth_key|api_secret_key)\s*(=|:|is)\s*['"]?[a-zA-Z0-9_.\-=/+]{16,128}['"]?`)

	// Password patterns: looks for common password names followed by assignment and a string of 8-64 alphanumeric/symbol chars
	passwordRegex = regexp.MustCompile(`(?i)(password|passwd|pass|pwd|passphrase)\s*(=|:|is)\s*['"]?[a-zA-Z0-9_.\-=/+]{8,64}['"]?`)

	// Database/Service URL patterns: looks for common protocol prefixes followed by :// and non-whitespace/quote characters
	dbUrlRegex = regexp.MustCompile(`(?i)(jdbc|mongodb|mysql|postgresql|sqlite|sqlserver|redis|amqp|kafka|mqtt|sftp|ftp|smb|ldap|rdp):\/\/[^\s'"]+`)

	// SSH Private Key patterns: looks for standard PEM headers
	sshPrivateKeyRegex = regexp.MustCompile(`(?i)BEGIN (RSA|DSA|EC|OPENSSH) PRIVATE KEY`)

	// AWS Credentials patterns: specific patterns for AWS Access Key ID and Secret Access Key
	awsAccessKeyIDRegex     = regexp.MustCompile(`(AKIA|AROA|AIDA|ASIA)[0-9A-Z]{16}`)
	awsSecretAccessKeyRegex = regexp.MustCompile(`(?i)aws_secret_access_key\s*=\s*['"]?[a-zA-Z0-9\/+=]{40}['"]?`)
	awsSessionTokenRegex    = regexp.MustCompile(`(?i)aws_session_token\s*=\s*['"]?[a-zA-Z0-9\/+=]{100,200}['"]?`) // Session tokens are longer

	// Generic Bearer Token (often JWTs or similar long strings)
	bearerTokenRegex = regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9\-_=\.]{30,}`)

	// JSON Web Token (JWT) pattern
	jwtRegex = regexp.MustCompile(`eyJ[A-Za-z0-9-_=]+\.[A-Za-z0-9-_=]+\.[A-Za-z0-9-_.+/=]*`)

	// GitHub Personal Access Token (PAT)
	githubPatRegex = regexp.MustCompile(`(ghp_[a-zA-Z0-9]{36}|github_pat_[a-zA-Z0-9_]{80})`)

	// GitLab Personal Access Token (PAT)
	gitlabPatRegex = regexp.MustCompile(`glpat-[a-zA-Z0-9\-_]{20,}`)

	// Stripe API Keys (sk_live_, pk_live_)
	stripeApiKeyRegex = regexp.MustCompile(`(sk|pk)_(test|live)_[a-zA-Z0-9]{24,}`)

	// Twilio Auth Tokens (ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxx, SKxxxxxxxxxxxxxxxxxxxxxxxxxxxxx)
	twilioAuthTokenRegex = regexp.MustCompile(`(AC|SK)[a-zA-Z0-9]{32}`)

	// Slack Tokens (xoxb-, xapp-)
	slackTokenRegex = regexp.MustCompile(`(xoxb|xapp)-[0-9]{10,15}-[0-9]{10,15}-[a-zA-Z0-9]{10,}`)

	// Google API Key (AIza...)
	googleApiKeyRegex = regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`)

	// Heroku API Key (UUID format)
	herokuApiKeyRegex = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
)

// DetectSecurityConcerns analyzes the given content for common security-related patterns.
// It returns a list of detected concern types and a map linking each concern type to its first matched snippet.
func DetectSecurityConcerns(content string) ([]string, map[string]string) {
	var concerns []string
	snippets := make(map[string]string) // Stores the first matched snippet for each concern type

	// Helper function to add concern and its first matched snippet
	addConcern := func(concernType string, regex *regexp.Regexp) {
		if match := regex.FindString(content); match != "" {
			concerns = append(concerns, concernType)
			if _, ok := snippets[concernType]; !ok { // Only store the first snippet found for this type
				snippets[concernType] = match
			}
		}
	}

	addConcern("API Key Exposure", apiKeyRegex)
	addConcern("Password Exposure", passwordRegex)
	addConcern("Database/Service Creds Exposure", dbUrlRegex)
	addConcern("SSH Private Key Exposure", sshPrivateKeyRegex)
	addConcern("AWS Access Key ID Exposure", awsAccessKeyIDRegex)
	addConcern("AWS Secret Access Key Exposure", awsSecretAccessKeyRegex)
	addConcern("AWS Session Token Exposure", awsSessionTokenRegex)
	addConcern("Generic Bearer Token Exposure", bearerTokenRegex)
	addConcern("JWT Token Exposure", jwtRegex)
	addConcern("GitHub PAT Exposure", githubPatRegex)
	addConcern("GitLab PAT Exposure", gitlabPatRegex)
	addConcern("Stripe API Key Exposure", stripeApiKeyRegex)
	addConcern("Twilio Auth Token Exposure", twilioAuthTokenRegex)
	addConcern("Slack Token Exposure", slackTokenRegex)
	addConcern("Google API Key Exposure", googleApiKeyRegex)
	addConcern("Heroku API Key Exposure", herokuApiKeyRegex)

	// Deduplicate concerns list and sort it
	uniqueConcernsMap := make(map[string]bool)
	var uniqueConcernsList []string
	for _, c := range concerns {
		if !uniqueConcernsMap[c] {
			uniqueConcernsMap[c] = true
			uniqueConcernsList = append(uniqueConcernsList, c)
		}
	}
	sort.Strings(uniqueConcernsList)

	return uniqueConcernsList, snippets
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
	detectedConcernsList, detectedSnippetsMap := DetectSecurityConcerns(fileContent)

	// Filter out concerns that were previously ignored
	newlyDetectedConcerns := []string{}
	for _, concern := range detectedConcernsList {
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
		snippet := detectedSnippetsMap[concern] // Retrieve the snippet for this concern type
		prompt := prompts.PotentialSecurityConcernsFound(relativePath, concern, snippet)
		if logger.AskForConfirmation(prompt, true, false) { // We default to not ignoring new concerns
			concernsForThisFile = append(concernsForThisFile, concern)
			logger.Logf("Security concern '%s' in %s noted as an issue.", concern, relativePath)
		} else {
			ignoredConcernsForThisFile = append(ignoredConcernsForThisFile, concern)
			logger.Logf("Security concern '%s' in %s noted as unimportant.", concern, relativePath)
		}
	}

	// Add back any concerns that were previously marked as issues and are still detected
	if !isNew && isChanged {
		for _, prevConcern := range existingSecurityConcerns {
			isStillDetected := false
			for _, currentDetected := range detectedConcernsList { // Use the newly detected list
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
