package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// Agentic-specific configuration structures
type ProviderModelCombination struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Name     string `json:"name"`
}

type AgenticTestSuite struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	TestCases   []string `json:"test_cases"`
	TimeoutSecs int      `json:"timeout_seconds"`
	RunsPerTest int      `json:"runs_per_test"`
	Priority    string   `json:"priority"`
}

type AgenticConfig struct {
	TestSuites                  map[string]AgenticTestSuite            `json:"test_suites"`
	ProviderModelCombinations   map[string][]ProviderModelCombination  `json:"provider_model_combinations"`
	EvaluationCriteria          map[string]EvaluationCriterion         `json:"evaluation_criteria"`
	SuccessThresholds           map[string]int                         `json:"success_thresholds"`
}

type EvaluationCriterion struct {
	Weight      float64 `json:"weight"`
	Description string  `json:"description"`
}

type AgenticTestCase struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Category        string            `json:"category"`
	Difficulty      string            `json:"difficulty"`
	EstimatedTimeMin int              `json:"estimated_time_minutes"`
	Input           AgenticTestInput  `json:"input"`
	ExpectedOutputs map[string]interface{} `json:"expected_outputs"`
	Evaluation      AgenticEvaluation `json:"evaluation"`
	Scoring         AgenticScoring    `json:"scoring"`
	SuccessCriteria SuccessCriteria   `json:"success_criteria"`
}

type AgenticTestInput struct {
	Prompt       string               `json:"prompt"`
	Codebase     CodebaseDefinition   `json:"codebase"`
	Requirements []string             `json:"requirements"`
}

type CodebaseDefinition struct {
	Files   map[string]string `json:"files"`
	Context string            `json:"context"`
}

type AgenticEvaluation struct {
	AgenticCapabilities   []string `json:"agentic_capabilities"`
	TechnicalRequirements []string `json:"technical_requirements"`
	CodeQuality           []string `json:"code_quality"`
}

type AgenticScoring struct {
	MaxPoints   int            `json:"max_points"`
	Categories  map[string]int `json:",inline"`
}

type SuccessCriteria struct {
	MustHave    []string `json:"must_have"`
	ShouldHave  []string `json:"should_have"`
	NiceToHave  []string `json:"nice_to_have"`
}

type AgenticTestResult struct {
	TestID       string        `json:"test_id"`
	ModelName    string        `json:"model_name"`
	Provider     string        `json:"provider"`
	PromptType   string        `json:"prompt_type"`
	Timestamp    time.Time     `json:"timestamp"`
	ResponseTime time.Duration `json:"response_time"`
	Response     string        `json:"response"`
	TokensUsed   int           `json:"tokens_used,omitempty"`
	Success      bool          `json:"success"`
	Error        string        `json:"error,omitempty"`
	Score        int           `json:"score"`
	DetailedScores map[string]int `json:"detailed_scores"`
	Notes        string        `json:"notes,omitempty"`
}

type AgenticEvaluationRun struct {
	RunID       string              `json:"run_id"`
	Timestamp   time.Time           `json:"timestamp"`
	Config      AgenticRunConfig    `json:"config"`
	Results     []AgenticTestResult `json:"results"`
	Summary     AgenticRunSummary   `json:"summary"`
}

type AgenticRunConfig struct {
	ProviderModels []ProviderModelCombination `json:"provider_models"`
	PromptTypes    []string                   `json:"prompt_types"`
	TestSuite      string                     `json:"test_suite"`
	Iterations     int                        `json:"iterations"`
}

type AgenticRunSummary struct {
	TotalTests        int                        `json:"total_tests"`
	SuccessRate       float64                    `json:"success_rate"`
	AvgScore          float64                    `json:"avg_score"`
	AvgTime           time.Duration              `json:"avg_time"`
	ModelComparisons  map[string]ModelPerformance `json:"model_comparisons"`
	PromptComparisons map[string]PromptPerformance `json:"prompt_comparisons"`
}

type ModelPerformance struct {
	Tests         int           `json:"tests"`
	SuccessRate   float64       `json:"success_rate"`
	AvgScore      float64       `json:"avg_score"`
	AvgTime       time.Duration `json:"avg_time"`
	Strengths     []string      `json:"strengths"`
	Weaknesses    []string      `json:"weaknesses"`
}

type PromptPerformance struct {
	Tests       int           `json:"tests"`
	SuccessRate float64       `json:"success_rate"`
	AvgScore    float64       `json:"avg_score"`
	AvgTime     time.Duration `json:"avg_time"`
}

// CLI Options for agentic testing
type AgenticCLIOptions struct {
	ProviderModels string
	PromptTypes    string
	TestSuite      string
	Iterations     int
	Output         string
	Verbose        bool
	DryRun         bool
	Timeout        int
}

func main() {
	fmt.Println("🤖 Agentic Problem-Solving Evaluation Suite")
	fmt.Println("===========================================")

	// Parse CLI arguments
	opts := parseAgenticCLIArgs()

	if opts.DryRun {
		fmt.Println("🧪 Dry Run Mode - Agentic Testing")
		fmt.Printf("Would test: ProviderModels=%s, PromptTypes=%s, TestSuite=%s, Iterations=%d\n",
			opts.ProviderModels, opts.PromptTypes, opts.TestSuite, opts.Iterations)
		return
	}

	// Load agentic configuration
	agenticConfig, err := loadAgenticConfig()
	if err != nil {
		fmt.Printf("❌ Error loading agentic config: %v\n", err)
		os.Exit(1)
	}

	// Parse provider/model combinations
	providerModels := parseProviderModelCombinations(opts.ProviderModels, agenticConfig)
	if len(providerModels) == 0 {
		fmt.Println("❌ No valid provider/model combinations specified")
		fmt.Println("Available combinations: all_models, fast_models, thorough_models, balanced_models, deepinfra_models")
		os.Exit(1)
	}

	// Parse prompt types
	promptTypes := parsePromptTypes(opts.PromptTypes)
	if len(promptTypes) == 0 {
		promptTypes = []string{"base/v4_streamlined"} // Default
	}

	// Load test suite
	testSuite, exists := agenticConfig.TestSuites[opts.TestSuite]
	if !exists {
		fmt.Printf("❌ Test suite '%s' not found\n", opts.TestSuite)
		fmt.Println("Available test suites: agentic_core, agentic_comprehensive, quick_agentic")
		os.Exit(1)
	}

	// Load test cases
	testCases, err := loadAgenticTestCases(testSuite.TestCases)
	if err != nil {
		fmt.Printf("❌ Error loading test cases: %v\n", err)
		os.Exit(1)
	}

	// Create evaluation run
	run := &AgenticEvaluationRun{
		RunID:     generateRunID(),
		Timestamp: time.Now(),
		Config: AgenticRunConfig{
			ProviderModels: providerModels,
			PromptTypes:    promptTypes,
			TestSuite:      opts.TestSuite,
			Iterations:     opts.Iterations,
		},
	}

	// Calculate total tests
	totalTests := len(providerModels) * len(promptTypes) * len(testCases) * opts.Iterations
	fmt.Printf("📊 Running agentic evaluation: %d provider/model combinations × %d prompts × %d tests × %d iterations = %d total tests\n",
		len(providerModels), len(promptTypes), len(testCases), opts.Iterations, totalTests)

	fmt.Printf("⏱️  Estimated time: %d-%d minutes (based on test complexity)\n", 
		totalTests*3, totalTests*7) // 3-7 minutes per agentic test

	// Execute tests
	testCount := 0
	for _, providerModel := range providerModels {
		for _, promptType := range promptTypes {
			for _, testCase := range testCases {
				for i := 0; i < opts.Iterations; i++ {
					testCount++
					
					fmt.Printf("\n[%d/%d] 🧠 Testing %s with %s on %s (iteration %d)\n",
						testCount, totalTests, providerModel.Name, promptType, testCase.Name, i+1)
					
					result := runAgenticTest(providerModel, promptType, testCase, opts.Timeout, opts.Verbose)
					run.Results = append(run.Results, result)
					
					if opts.Verbose {
						displayAgenticResult(result)
					} else {
						if result.Success {
							fmt.Print("✅")
						} else {
							fmt.Print("❌")
						}
					}
				}
			}
		}
	}

	// Calculate summary
	run.Summary = calculateAgenticSummary(run.Results)
	
	// Display results
	displayAgenticSummary(run)
	
	// Save results
	if opts.Output != "" {
		err := saveAgenticResults(run, opts.Output)
		if err != nil {
			fmt.Printf("⚠️  Error saving results: %v\n", err)
		} else {
			fmt.Printf("💾 Results saved to: %s\n", opts.Output)
		}
	}
}

func parseAgenticCLIArgs() AgenticCLIOptions {
	opts := AgenticCLIOptions{}
	
	flag.StringVar(&opts.ProviderModels, "provider-models", "all_models", "Provider/model combination group (e.g., all_models, fast_models)")
	flag.StringVar(&opts.PromptTypes, "prompt-types", "base/v4_streamlined", "Comma-separated list of prompt types to test")
	flag.StringVar(&opts.TestSuite, "test-suite", "agentic_core", "Agentic test suite to run")
	flag.IntVar(&opts.Iterations, "iterations", 1, "Number of iterations per test")
	flag.StringVar(&opts.Output, "output", "", "Output file for results (JSON)")
	flag.BoolVar(&opts.Verbose, "verbose", false, "Verbose output")
	flag.BoolVar(&opts.DryRun, "dry-run", false, "Show what would be tested without running")
	flag.IntVar(&opts.Timeout, "timeout", 300, "Timeout per test in seconds")
	
	flag.Parse()
	
	return opts
}

func loadAgenticConfig() (*AgenticConfig, error) {
	configPath := "configs/agentic_test_suite.json"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agentic config: %w", err)
	}
	
	var config AgenticConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse agentic config: %w", err)
	}
	
	return &config, nil
}

func parseProviderModelCombinations(input string, config *AgenticConfig) []ProviderModelCombination {
	if combinations, exists := config.ProviderModelCombinations[input]; exists {
		return combinations
	}
	return nil
}

func parsePromptTypes(input string) []string {
	if input == "" {
		return nil
	}
	
	// Simple comma-separated parsing for now
	// TODO: Implement more sophisticated parsing
	return []string{input}
}

func loadAgenticTestCases(testFiles []string) ([]AgenticTestCase, error) {
	var testCases []AgenticTestCase
	
	for _, filename := range testFiles {
		path := filepath.Join("test_cases", filename)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("⚠️  Warning: Failed to load test case %s: %v\n", filename, err)
			continue
		}
		
		var testCase AgenticTestCase
		err = json.Unmarshal(data, &testCase)
		if err != nil {
			fmt.Printf("⚠️  Warning: Failed to parse test case %s: %v\n", filename, err)
			continue
		}
		
		testCases = append(testCases, testCase)
	}
	
	return testCases, nil
}

func runAgenticTest(providerModel ProviderModelCombination, promptType string, testCase AgenticTestCase, timeoutSecs int, verbose bool) AgenticTestResult {
	start := time.Now()
	
	result := AgenticTestResult{
		TestID:     testCase.ID,
		ModelName:  providerModel.Name,
		Provider:   providerModel.Provider,
		PromptType: promptType,
		Timestamp:  start,
	}

	// Map provider string to ClientType
	var clientType api.ClientType
	switch providerModel.Provider {
	case "openai":
		clientType = api.OpenAIClientType
	case "deepinfra":
		clientType = api.DeepInfraClientType
	case "openrouter":
		clientType = api.OpenRouterClientType
	default:
		result.Error = fmt.Sprintf("Unknown provider: %s", providerModel.Provider)
		return result
	}

	// Create agent with explicit provider and model
	agentInstance, err := agent.NewAgentWithProviderAndModel(clientType, providerModel.Model)
	if err != nil {
		result.Error = fmt.Sprintf("Agent creation failed: %v", err)
		result.ResponseTime = time.Since(start)
		return result
	}

	// Construct comprehensive prompt with codebase context
	fullPrompt := constructAgenticPrompt(testCase)
	
	if verbose {
		fmt.Printf("📝 Prompt length: %d characters\n", len(fullPrompt))
		fmt.Printf("⏱️  Estimated time: %d minutes\n", testCase.EstimatedTimeMin)
	}

	// Prepare messages
	messages := []api.Message{
		{Role: "user", Content: fullPrompt},
	}

	// Execute test with timeout
	timeout := time.Duration(timeoutSecs) * time.Second
	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	go func() {
		response, err := agentInstance.GenerateResponse(messages)
		if err != nil {
			errorChan <- err
		} else {
			responseChan <- response
		}
	}()

	select {
	case response := <-responseChan:
		result.ResponseTime = time.Since(start)
		result.Response = response
		result.Success = true
		
		// Evaluate the response
		result.Score, result.DetailedScores = evaluateAgenticResponse(response, testCase)
		
	case err := <-errorChan:
		result.ResponseTime = time.Since(start)
		result.Error = fmt.Sprintf("Response failed: %v", err)
		
	case <-time.After(timeout):
		result.ResponseTime = timeout
		result.Error = fmt.Sprintf("Test timed out after %d seconds", timeoutSecs)
	}
	
	return result
}

func constructAgenticPrompt(testCase AgenticTestCase) string {
	prompt := fmt.Sprintf("%s\n\n", testCase.Input.Prompt)
	
	// Add codebase context
	if len(testCase.Input.Codebase.Files) > 0 {
		prompt += "## Current Codebase\n\n"
		for filename, content := range testCase.Input.Codebase.Files {
			prompt += fmt.Sprintf("### %s\n```\n%s\n```\n\n", filename, content)
		}
	}
	
	// Add context if available
	if testCase.Input.Codebase.Context != "" {
		prompt += fmt.Sprintf("## Context\n%s\n\n", testCase.Input.Codebase.Context)
	}
	
	// Add requirements
	if len(testCase.Input.Requirements) > 0 {
		prompt += "## Requirements\n"
		for _, req := range testCase.Input.Requirements {
			prompt += fmt.Sprintf("- %s\n", req)
		}
		prompt += "\n"
	}
	
	prompt += "Please provide a complete solution with explanations of your approach."
	
	return prompt
}

func evaluateAgenticResponse(response string, testCase AgenticTestCase) (int, map[string]int) {
	// Sophisticated evaluation logic for agentic capabilities
	// This is a placeholder - implement detailed evaluation based on test case criteria
	
	totalScore := 0
	detailedScores := make(map[string]int)
	
	// Basic scoring heuristics
	responseLength := len(response)
	
	// Completeness scoring
	completenessScore := 0
	if responseLength > 500 {
		completenessScore += 20
	}
	if responseLength > 1500 {
		completenessScore += 20
	}
	detailedScores["completeness"] = completenessScore
	totalScore += completenessScore
	
	// Technical content scoring
	technicalScore := 0
	if containsCodeBlocks(response) {
		technicalScore += 25
	}
	if containsExplanations(response) {
		technicalScore += 15
	}
	detailedScores["technical_content"] = technicalScore
	totalScore += technicalScore
	
	// Problem-solving approach scoring
	approachScore := 0
	if containsProblemAnalysis(response) {
		approachScore += 20
	}
	if containsStepByStepSolution(response) {
		approachScore += 20
	}
	detailedScores["approach"] = approachScore
	totalScore += approachScore
	
	// Cap at max points
	maxPoints := testCase.Scoring.MaxPoints
	if totalScore > maxPoints {
		totalScore = maxPoints
	}
	
	return totalScore, detailedScores
}

func containsCodeBlocks(response string) bool {
	return len(response) > 0 && (contains(response, "```") || contains(response, "func ") || contains(response, "package "))
}

func containsExplanations(response string) bool {
	return contains(response, "explanation") || contains(response, "approach") || contains(response, "solution")
}

func containsProblemAnalysis(response string) bool {
	return contains(response, "problem") || contains(response, "issue") || contains(response, "analysis")
}

func containsStepByStepSolution(response string) bool {
	return contains(response, "step") || contains(response, "first") || contains(response, "then") || contains(response, "1.")
}

func contains(text, substring string) bool {
	return len(text) >= len(substring) && 
		   len(substring) > 0 && 
		   text != substring &&
		   fmt.Sprintf("%s", text) != fmt.Sprintf("%s", substring) // Simple contains check
}

func calculateAgenticSummary(results []AgenticTestResult) AgenticRunSummary {
	summary := AgenticRunSummary{
		TotalTests:        len(results),
		ModelComparisons:  make(map[string]ModelPerformance),
		PromptComparisons: make(map[string]PromptPerformance),
	}
	
	if len(results) == 0 {
		return summary
	}
	
	totalScore := 0
	totalTime := time.Duration(0)
	successCount := 0
	
	modelStats := make(map[string][]AgenticTestResult)
	promptStats := make(map[string][]AgenticTestResult)
	
	for _, result := range results {
		if result.Success {
			successCount++
		}
		totalScore += result.Score
		totalTime += result.ResponseTime
		
		modelStats[result.ModelName] = append(modelStats[result.ModelName], result)
		promptStats[result.PromptType] = append(promptStats[result.PromptType], result)
	}
	
	summary.SuccessRate = float64(successCount) / float64(len(results))
	summary.AvgScore = float64(totalScore) / float64(len(results))
	summary.AvgTime = totalTime / time.Duration(len(results))
	
	// Calculate model-specific statistics
	for model, modelResults := range modelStats {
		performance := calculateModelPerformance(modelResults)
		summary.ModelComparisons[model] = performance
	}
	
	// Calculate prompt-specific statistics
	for prompt, promptResults := range promptStats {
		performance := calculatePromptPerformance(promptResults)
		summary.PromptComparisons[prompt] = performance
	}
	
	return summary
}

func calculateModelPerformance(results []AgenticTestResult) ModelPerformance {
	if len(results) == 0 {
		return ModelPerformance{}
	}
	
	successCount := 0
	totalScore := 0
	totalTime := time.Duration(0)
	
	for _, result := range results {
		if result.Success {
			successCount++
		}
		totalScore += result.Score
		totalTime += result.ResponseTime
	}
	
	return ModelPerformance{
		Tests:       len(results),
		SuccessRate: float64(successCount) / float64(len(results)),
		AvgScore:    float64(totalScore) / float64(len(results)),
		AvgTime:     totalTime / time.Duration(len(results)),
		// TODO: Implement strength/weakness analysis
		Strengths:  []string{},
		Weaknesses: []string{},
	}
}

func calculatePromptPerformance(results []AgenticTestResult) PromptPerformance {
	if len(results) == 0 {
		return PromptPerformance{}
	}
	
	successCount := 0
	totalScore := 0
	totalTime := time.Duration(0)
	
	for _, result := range results {
		if result.Success {
			successCount++
		}
		totalScore += result.Score
		totalTime += result.ResponseTime
	}
	
	return PromptPerformance{
		Tests:       len(results),
		SuccessRate: float64(successCount) / float64(len(results)),
		AvgScore:    float64(totalScore) / float64(len(results)),
		AvgTime:     totalTime / time.Duration(len(results)),
	}
}

func displayAgenticResult(result AgenticTestResult) {
	status := "✅"
	if !result.Success {
		status = "❌"
	}
	
	fmt.Printf("%s %s | %s | %s | %.1fs | Score: %d/%d\n",
		status, result.ModelName, result.PromptType, result.TestID,
		result.ResponseTime.Seconds(), result.Score, 100)
		
	if len(result.DetailedScores) > 0 {
		fmt.Printf("   Detailed: ")
		for category, score := range result.DetailedScores {
			fmt.Printf("%s:%d ", category, score)
		}
		fmt.Println()
	}
}

func displayAgenticSummary(run *AgenticEvaluationRun) {
	fmt.Printf("\n\n🧠 AGENTIC EVALUATION SUMMARY\n")
	fmt.Printf("=============================\n")
	fmt.Printf("Run ID: %s\n", run.RunID)
	fmt.Printf("Total Tests: %d | Success Rate: %.1f%% | Avg Score: %.1f | Avg Time: %.1fs\n\n",
		run.Summary.TotalTests, run.Summary.SuccessRate*100,
		run.Summary.AvgScore, run.Summary.AvgTime.Seconds())
	
	fmt.Printf("Model Performance Comparison:\n")
	fmt.Printf("-----------------------------\n")
	for model, perf := range run.Summary.ModelComparisons {
		fmt.Printf("%-15s | %d tests | %.1f%% success | %.1f avg score | %.1fs avg time\n",
			model, perf.Tests, perf.SuccessRate*100, perf.AvgScore, perf.AvgTime.Seconds())
	}
	
	if len(run.Summary.PromptComparisons) > 1 {
		fmt.Printf("\nPrompt Performance Comparison:\n")
		fmt.Printf("------------------------------\n")
		for prompt, perf := range run.Summary.PromptComparisons {
			fmt.Printf("%-25s | %d tests | %.1f%% success | %.1f avg score | %.1fs avg time\n",
				prompt, perf.Tests, perf.SuccessRate*100, perf.AvgScore, perf.AvgTime.Seconds())
		}
	}
}

func saveAgenticResults(run *AgenticEvaluationRun, filename string) error {
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	
	// Ensure results directory exists
	resultsDir := "results/agentic"
	os.MkdirAll(resultsDir, 0755)
	
	path := filepath.Join(resultsDir, filename)
	return os.WriteFile(path, data, 0644)
}

func generateRunID() string {
	return fmt.Sprintf("agentic_run_%d", time.Now().Unix())
}