package llm

// --- Token Counting ---

func EstimateTokens(text string) int {
	byteLength := len(text)
	const bytesPerTokenRatio = 2
	if byteLength == 0 {
		return 0
	}
	estimatedTokens := (byteLength + bytesPerTokenRatio - 1) / bytesPerTokenRatio
	return estimatedTokens
}
