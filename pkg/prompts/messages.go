package prompts

// Message represents a conversation message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Security-related prompts
func PotentialSecurityConcernsFound(relativePath, concern, snippet string) string {
	return "⚠️  Potential security concern found in " + relativePath + ": " + concern + "\n" + snippet + "\nIs this a security issue? (y/n)"
}

func SkippingLLMSummarizationDueToSecurity(relativePath string) string {
	return "⚠️  Skipping LLM summarization for " + relativePath + " due to potential security concerns"
}

// CodeReviewStagedPrompt returns the prompt for code review
func CodeReviewStagedPrompt() string {
	return "Please review the following code changes and provide feedback on potential issues, improvements, and best practices."
}
