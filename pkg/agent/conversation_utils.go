package agent

import (
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// containsFrontendKeywords checks if the query contains frontend-related keywords
func containsFrontendKeywords(query string) bool {
	// High-priority frontend indicators
	highPriorityKeywords := []string{
		"react", "vue", "angular", "nextjs", "next.js", "svelte",
		"app", "website", "webpage", "web app", "web application",
		"frontend", "front-end", "ui", "user interface", "interface",
		"layout", "design", "responsive", "mobile-first",
		"css", "html", "styling", "styles", "stylesheet",
		"component", "components", "widget", "widgets",
		"dashboard", "landing page", "homepage", "navigation",
		"mockup", "wireframe", "prototype", "screenshot",
	}

	// Secondary frontend indicators
	secondaryKeywords := []string{
		"colors", "palette", "theme", "branding",
		"bootstrap", "tailwind", "material", "chakra",
		"sass", "scss", "less", "styled-components",
		"button", "form", "input", "modal", "dropdown",
		"header", "footer", "sidebar", "menu",
		"grid", "flexbox", "margin", "padding", "border",
		"typography", "font", "text", "heading",
		"animation", "transition", "hover", "interactive",
	}

	queryLower := strings.ToLower(query)

	// Check high-priority keywords first (any match = frontend)
	for _, keyword := range highPriorityKeywords {
		if strings.Contains(queryLower, keyword) {
			return true
		}
	}

	// Check for multiple secondary keywords (2+ matches = frontend)
	matches := 0
	for _, keyword := range secondaryKeywords {
		if strings.Contains(queryLower, keyword) {
			matches++
			if matches >= 2 {
				return true
			}
		}
	}

	return false
}

// determineReasoningEffort determines the appropriate reasoning effort level based on the query
func (a *Agent) determineReasoningEffort(messages []api.Message) string {
	// Only certain providers support reasoning effort
	if a.GetProvider() != "openai" && a.GetProvider() != "deepseek" {
		return "" // Default - provider will ignore it
	}

	// Get the last user message
	var lastUserMessage string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserMessage = messages[i].Content
			break
		}
	}

	if lastUserMessage == "" {
		return "medium" // Default
	}

	templatePayload := isPromptTemplatePayload(lastUserMessage)
	queryLower := strings.ToLower(lastUserMessage)

	// High reasoning effort indicators
	highEffortKeywords := []string{
		"algorithm", "optimize", "performance", "complexity",
		"architect", "design pattern", "refactor", "security",
		"analyze", "debug", "trace", "investigate",
		"compare", "evaluate", "trade-off", "decision",
		"implement", "integrate", "migrate", "transform",
		"explain why", "explain how", "deep dive", "comprehensive",
		"edge case", "corner case", "test case", "validation",
		"best practice", "recommendation", "strategy",
		"fix", "solve", "resolve", "troubleshoot",
		"create", "build", "develop", "construct",
	}

	// Low reasoning effort indicators
	lowEffortKeywords := []string{
		"what is", "define", "list", "show", "display",
		"tell me", "give me", "provide", "fetch",
		"simple", "basic", "quick", "brief",
		"yes or no", "true or false", "check if",
		"count", "how many", "number of",
		"rename", "move", "copy", "delete",
		"format", "indent", "spacing", "style",
		"typo", "spelling", "grammar",
		"comment", "document", "annotate",
	}

	// Count matches
	highMatches := 0
	lowMatches := 0

	for _, keyword := range highEffortKeywords {
		if strings.Contains(queryLower, keyword) {
			highMatches++
		}
	}

	for _, keyword := range lowEffortKeywords {
		if strings.Contains(queryLower, keyword) {
			lowMatches++
		}
	}

	// Determine effort level based on matches and query characteristics
	if highMatches >= 2 || (highMatches > lowMatches && len(lastUserMessage) > 100) {
		if templatePayload {
			return a.capReasoningEffortForModel("medium")
		}
		return a.capReasoningEffortForModel("high")
	} else if lowMatches >= 2 || (lowMatches > highMatches) {
		return a.capReasoningEffortForModel("low")
	}

	// Check query length as additional factor
	if len(lastUserMessage) > 200 {
		if templatePayload {
			return a.capReasoningEffortForModel("medium")
		}
		return a.capReasoningEffortForModel("high") // Complex queries likely need more reasoning
	} else if len(lastUserMessage) < 50 {
		return a.capReasoningEffortForModel("low") // Short queries are usually simple
	}

	return a.capReasoningEffortForModel("medium") // Default for balanced tasks
}

func (a *Agent) capReasoningEffortForModel(effort string) string {
	if effort != "high" {
		return effort
	}

	model := strings.ToLower(a.GetModel())
	// gpt-oss high effort tends to over-deliberate in tool-heavy workflows.
	// Cap to medium for better latency/token performance while preserving quality.
	if strings.Contains(model, "gpt-oss") {
		return "medium"
	}

	return effort
}

func isPromptTemplatePayload(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}

	lower := strings.ToLower(text)
	markers := []string{
		"agentic restaurant extraction prompt",
		"ocr trigger policy",
		"output directory layout",
		"common json envelope",
		"canonical structured tool calls",
		"schema: organization",
		"schema: menu",
		"schema: offer",
	}

	hits := 0
	for _, m := range markers {
		if strings.Contains(lower, m) {
			hits++
		}
	}

	return hits >= 2
}
