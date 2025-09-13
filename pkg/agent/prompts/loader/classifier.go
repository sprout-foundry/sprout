package loader

import (
	"strings"
)

// RequestClassification represents the analysis of a user request
type RequestClassification struct {
	PrimaryType    string   // exploratory, implementation, debugging, testing, analysis
	Complexity     string   // simple, medium, complex
	RequiredTools  []string // shell, file_ops, web_search, ui_analysis, etc.
	ContextNeeds   []string // debugging, testing, batch_ops, todo_workflow, etc.
	Confidence     float64  // 0.0-1.0 confidence in classification
}

// RequestClassifier analyzes user requests to determine appropriate prompt modules
type RequestClassifier struct{}

// NewRequestClassifier creates a new classifier instance
func NewRequestClassifier() *RequestClassifier {
	return &RequestClassifier{}
}

// ClassifyRequest analyzes a user request and returns appropriate modules to include
func (c *RequestClassifier) ClassifyRequest(request string) RequestClassification {
	request = strings.ToLower(strings.TrimSpace(request))
	
	classification := RequestClassification{
		RequiredTools: []string{},
		ContextNeeds:  []string{},
	}
	
	// Determine primary request type
	classification.PrimaryType = c.determinePrimaryType(request)
	classification.Complexity = c.determineComplexity(request)
	classification.RequiredTools = c.determineRequiredTools(request)
	classification.ContextNeeds = c.determineContextNeeds(request, classification.PrimaryType)
	classification.Confidence = c.calculateConfidence(request, classification)
	
	return classification
}

// determinePrimaryType identifies the main type of request
func (c *RequestClassifier) determinePrimaryType(request string) string {
	// Exploratory indicators
	exploratoryWords := []string{
		"tell me about", "explore", "understand", "what does", "how does", 
		"explain", "analyze", "show me", "describe", "overview", "summary",
	}
	
	// Implementation indicators  
	implementationWords := []string{
		"add", "implement", "create", "build", "change", "update", 
		"refactor", "write", "generate", "develop", "make",
	}
	
	// Debugging indicators
	debuggingWords := []string{
		"fix", "debug", "troubleshoot", "error", "failing", "broken", 
		"issue", "problem", "crash", "bug",
	}
	
	// Testing indicators
	testingWords := []string{
		"test", "tests", "testing", "unit test", "integration test",
		"verify", "validate", "check",
	}
	
	// Check for each type (order matters - more specific first)
	if c.containsAny(request, debuggingWords) {
		return "debugging"
	}
	if c.containsAny(request, testingWords) {
		return "testing"  
	}
	if c.containsAny(request, implementationWords) {
		return "implementation"
	}
	if c.containsAny(request, exploratoryWords) {
		return "exploratory"
	}
	
	// Default to exploratory for questions
	if strings.Contains(request, "?") || strings.HasPrefix(request, "what") || 
	   strings.HasPrefix(request, "how") || strings.HasPrefix(request, "where") ||
	   strings.HasPrefix(request, "why") {
		return "exploratory"
	}
	
	// Default to implementation for action requests
	return "implementation"
}

// determineComplexity assesses task complexity
func (c *RequestClassifier) determineComplexity(request string) string {
	complexityIndicators := []string{
		"multiple", "all", "entire", "system", "architecture", "refactor",
		"integrate", "build", "implement", "create", "design",
	}
	
	simpleIndicators := []string{
		"single", "one", "quick", "simple", "just", "only",
	}
	
	if c.containsAny(request, complexityIndicators) {
		return "complex"
	}
	if c.containsAny(request, simpleIndicators) {
		return "simple"
	}
	
	return "medium"
}

// determineRequiredTools identifies what tools might be needed
func (c *RequestClassifier) determineRequiredTools(request string) []string {
	tools := []string{}
	
	// Always include basic tools
	tools = append(tools, "shell", "file_ops")
	
	if strings.Contains(request, "web") || strings.Contains(request, "search") || 
	   strings.Contains(request, "online") {
		tools = append(tools, "web_search")
	}
	
	if strings.Contains(request, "ui") || strings.Contains(request, "interface") ||
	   strings.Contains(request, "screenshot") {
		tools = append(tools, "ui_analysis")
	}
	
	return tools
}

// determineContextNeeds identifies what context modules are needed
func (c *RequestClassifier) determineContextNeeds(request string, primaryType string) []string {
	contexts := []string{}
	
	// Always include batch operations for efficiency
	contexts = append(contexts, "batch_operations")
	
	// Add debugging context for debugging requests or error-related tasks
	if primaryType == "debugging" || strings.Contains(request, "error") || 
	   strings.Contains(request, "fail") || strings.Contains(request, "broken") {
		contexts = append(contexts, "circuit_breakers")
	}
	
	// Add todo workflow for complex tasks
	complexity := c.determineComplexity(request)
	if complexity == "complex" || primaryType == "implementation" {
		// Check for implementation complexity indicators
		complexImplementationWords := []string{
			"implement", "build", "create", "refactor", "multiple files", 
			"system", "feature", "integration",
		}
		if c.containsAny(request, complexImplementationWords) {
			contexts = append(contexts, "todo_workflow")
		}
	}
	
	return contexts
}

// calculateConfidence estimates how confident we are in the classification
func (c *RequestClassifier) calculateConfidence(request string, classification RequestClassification) float64 {
	confidence := 0.5 // base confidence
	
	// Increase confidence for clear indicators
	clearIndicators := map[string][]string{
		"exploratory":    {"tell me", "explain", "what does", "how does"},
		"implementation": {"implement", "add", "create", "build"},
		"debugging":      {"fix", "debug", "error", "failing"},
	}
	
	if indicators, exists := clearIndicators[classification.PrimaryType]; exists {
		if c.containsAny(request, indicators) {
			confidence += 0.3
		}
	}
	
	// Increase confidence for specific task words
	if strings.Contains(request, classification.PrimaryType) {
		confidence += 0.2
	}
	
	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}
	
	return confidence
}

// containsAny checks if the request contains any of the given words/phrases
func (c *RequestClassifier) containsAny(request string, words []string) bool {
	for _, word := range words {
		if strings.Contains(request, word) {
			return true
		}
	}
	return false
}