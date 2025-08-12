package agent

import "strings"

// determineTaskComplexity determines the complexity level for optimization routing
func determineTaskComplexity(intent string, analysis *IntentAnalysis) TaskComplexityLevel {
	intentLower := strings.ToLower(intent)

	investigativeKeywords := []string{
		"find", "search", "grep", "list", "show", "check", "analyze", "investigate",
		"look for", "locate", "identify", "discover", "scan", "examine",
		"use grep", "use find", "run command", "execute", "shell",
	}
	for _, keyword := range investigativeKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskModerate
		}
	}

	simpleKeywords := []string{
		"comment", "add comment", "add a comment", "simple comment",
		"documentation", "docs", "readme", "add doc", "update doc",
		"typo", "fix typo", "spelling", "whitespace", "formatting",
		"rename variable", "rename function", "simple rename",
	}
	for _, keyword := range simpleKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskSimple
		}
	}

	if analysis != nil {
		if analysis.Category == "docs" && analysis.Complexity == "simple" {
			return TaskSimple
		}
		if analysis.Complexity == "complex" || analysis.Category == "refactor" || len(analysis.EstimatedFiles) > 3 {
			return TaskComplex
		}
	}

	refactorKeywords := []string{
		"refactor", "restructure", "redesign", "architecture",
		"migrate", "convert", "rewrite", "overhaul",
		"extract", "move code", "split file", "organize code",
	}
	for _, keyword := range refactorKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskComplex
		}
	}

	complexKeywords := []string{
		"implement feature", "add feature", "new feature",
		"remove feature", "delete module",
	}
	for _, keyword := range complexKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskComplex
		}
	}

	return TaskModerate
}
