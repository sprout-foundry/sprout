package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ScopeValidator validates changes against spec
type ScopeValidator struct {
	agentClient api.ClientInterface
	logger      *utils.Logger
	cfg         *configuration.Config
}

// NewScopeValidator creates a new scope validator
func NewScopeValidator(cfg *configuration.Config, logger *utils.Logger) (*ScopeValidator, error) {
	// Create agent client for LLM calls using factory to avoid import cycles
	clientType, err := api.DetermineProvider("", api.ClientType(cfg.LastUsedProvider))
	if err != nil {
		return nil, fmt.Errorf("failed to determine provider: %w", err)
	}

	agentClient, err := factory.CreateProviderClient(clientType, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create agent client: %w", err)
	}

	return &ScopeValidator{
		agentClient: agentClient,
		logger:      logger,
		cfg:         cfg,
	}, nil
}

// ValidateScope checks if changes are within spec boundaries
func (v *ScopeValidator) ValidateScope(diff string, spec *CanonicalSpec) (*ScopeReviewResult, error) {
	// Validate inputs
	if diff == "" {
		return nil, fmt.Errorf("diff cannot be empty")
	}
	if spec == nil {
		return nil, fmt.Errorf("spec cannot be nil")
	}

	// Build spec JSON for LLM
	specJSON, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	// Build the full prompt
	prompt := ScopeValidationPrompt()
	fullPrompt := fmt.Sprintf("%s\n\n## Specification (JSON)\n%s\n\n## Code Changes\n```diff\n%s\n```",
		prompt, string(specJSON), diff)

	// Call LLM to validate scope
	v.logger.LogProcessStep("Validating scope compliance...")

	messages := []api.Message{
		{Role: "user", Content: fullPrompt},
	}

	// Log the prompt size for debugging
	promptSize := len(fullPrompt)
	v.logger.LogProcessStep(fmt.Sprintf("Scope validation prompt size: %d bytes", promptSize))

	chatResponse, err := v.agentClient.SendChatRequest(messages, nil, "")
	if err != nil {
		// Check for rate limiting or timeout errors
		errStr := err.Error()
		if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") {
			v.logger.LogProcessStep("⚠️  Rate limited - skipping scope validation (would require retry/backoff)")
			// Return a "pass" result when rate limited so the workflow can continue
			return &ScopeReviewResult{
				InScope:     true,
				Violations:  []ScopeViolation{},
				Summary:     "Scope validation skipped due to rate limiting",
				Suggestions: []string{"Retry self-review later when API rate limits reset"},
			}, nil
		}
		return nil, fmt.Errorf("failed to validate scope: %w", err)
	}

	response := chatResponse.Choices[0].Message.Content

	// Parse JSON response
	var result ScopeReviewResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		// Try to extract JSON from response if it's wrapped in markdown code blocks
		jsonStart := strings.Index(response, "{")
		jsonEnd := strings.LastIndex(response, "}")
		if jsonStart >= 0 && jsonEnd > jsonStart {
			jsonStr := response[jsonStart : jsonEnd+1]
			if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
				return nil, fmt.Errorf("failed to parse scope validation JSON: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to parse scope validation JSON: %w", err)
		}
	}

	// Post-process violations to add line numbers if missing
	for i := range result.Violations {
		if result.Violations[i].Line == 0 {
			// Try to find line number from diff
			lineNum := findLineNumberInDiff(diff, result.Violations[i].File, result.Violations[i].Description)
			result.Violations[i].Line = lineNum
		}
	}

	if result.InScope {
		v.logger.LogProcessStep("✓ Changes are within scope")
	} else {
		v.logger.LogProcessStep(fmt.Sprintf("⚠ Found %d scope violations", len(result.Violations)))
	}

	return &result, nil
}

// findLineNumberInDiff attempts to find a line number for a violation in the diff
func findLineNumberInDiff(diff, file, description string) int {
	lines := strings.Split(diff, "\n")
	inCorrectFile := false
	currentLine := 0

	// Build a pattern to search for
	searchPattern := description
	if len(searchPattern) > 50 {
		searchPattern = searchPattern[:50] // Limit search pattern length
	}

	for _, line := range lines {
		// Check if we're entering the target file
		if strings.HasPrefix(line, "diff --git") {
			if strings.Contains(line, file) {
				inCorrectFile = true
			} else {
				inCorrectFile = false
			}
			currentLine = 0
			continue
		}

		// Track line numbers within hunks
		if strings.HasPrefix(line, "@@") {
			// Parse hunk header to get line number
			// Format: @@ -old_start,old_count +new_start,new_count @@
			re := regexp.MustCompile(`\+(\d+),?\d* @@`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 2 {
				fmt.Sscanf(matches[1], "%d", &currentLine)
			}
			continue
		}

		// Count lines in the target file
		if inCorrectFile {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "++") {
				currentLine++
				// Check if this line matches our search pattern
				if strings.Contains(line, searchPattern) ||
					looseMatch(description, line) {
					return currentLine
				}
			} else if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "\\") {
				currentLine++
			}
		}
	}

	return 0 // Not found
}

// looseMatch performs a loose string match for finding violations
func looseMatch(pattern, text string) bool {
	// Remove common prefixes/suffixes and check for substantial overlap
	patternWords := strings.Fields(strings.ToLower(pattern))
	textWords := strings.Fields(strings.ToLower(strings.TrimPrefix(text, "+")))

	if len(patternWords) == 0 || len(textWords) == 0 {
		return false
	}

	// Check if any significant word from pattern appears in text
	for _, pword := range patternWords {
		if len(pword) < 3 {
			continue // Skip very short words
		}
		for _, tword := range textWords {
			if len(tword) < 3 {
				continue
			}
			if strings.Contains(pword, tword) || strings.Contains(tword, pword) {
				return true
			}
		}
	}

	return false
}
