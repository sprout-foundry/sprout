package llm

import (
	"regexp"
	"strings"
)

// removeThinkTags removes  blocks from the content.
func removeThinkTags(content string) string {
	re := regexp.MustCompile(`(?s)`)
	return re.ReplaceAllString(content, "")
}

// IsGeminiModel checks if the given model name is a Gemini model.
func IsGeminiModel(modelName string) bool {
	lowerModelName := strings.ToLower(modelName)
	return strings.Contains(lowerModelName, "gemini")
}
