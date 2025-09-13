package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// Configuration structures
type ModelConfig struct {
	Provider      string            `json:"provider"`
	ModelID       string            `json:"model_id"`
	DisplayName   string            `json:"display_name"`
	ContextLimit  int               `json:"context_limit"`
	CostPer1KTokens map[string]float64 `json:"cost_per_1k_tokens"`
	Characteristics map[string]string `json:"characteristics"`
}

type ModelsConfig struct {
	Models      map[string]ModelConfig   `json:"models"`
	ModelGroups map[string][]string      `json:"model_groups"`
}

type TestCase struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Category    string      `json:"category"`
	Difficulty  string      `json:"difficulty"`
	Input       TestInput   `json:"input"`
	Evaluation  Evaluation  `json:"evaluation"`
	Scoring     Scoring     `json:"scoring"`
}

type TestInput struct {
	Prompt       string   `json:"prompt"`
	Language     string   `json:"language,omitempty"`
	Requirements []string `json:"requirements,omitempty"`
	Context      string   `json:"context,omitempty"`
}

type Evaluation struct {
	CorrectnessTests []CorrectnessTest `json:"correctness_tests,omitempty"`
	QualityChecks    []string          `json:"quality_checks,omitempty"`
	ContentChecks    []string          `json:"content_checks,omitempty"`
}

type CorrectnessTest struct {
	Input    interface{} `json:"input"`
	Expected interface{} `json:"expected"`
}

type Scoring struct {
	MaxPoints   int            `json:"max_points"`
	Categories  map[string]int `json:",inline"`
}

type TestResult struct {
	TestID       string        `json:"test_id"`
	ModelName    string        `json:"model_name"`
	PromptType   string        `json:"prompt_type"`
	Timestamp    time.Time     `json:"timestamp"`
	ResponseTime time.Duration `json:"response_time"`
	Response     string        `json:"response"`
	TokensUsed   int           `json:"tokens_used,omitempty"`
	Success      bool          `json:"success"`
	Error        string        `json:"error,omitempty"`
	Score        int           `json:"score"`
	Notes        string        `json:"notes,omitempty"`
}

type EvaluationRun struct {
	RunID       string       `json:"run_id"`
	Timestamp   time.Time    `json:"timestamp"`
	Config      RunConfig    `json:"config"`
	Results     []TestResult `json:"results"`
	Summary     RunSummary   `json:"summary"`
}

type RunConfig struct {
	Models     []string `json:"models"`
	Prompts    []string `json:"prompts"`
	TestSuite  string   `json:"test_suite"`
	Iterations int      `json:"iterations"`
}

type RunSummary struct {
	TotalTests    int            `json:"total_tests"`
	SuccessRate   float64        `json:"success_rate"`
	AvgScore      float64        `json:"avg_score"`
	AvgTime       time.Duration  `json:"avg_time"`
	ModelStats    map[string]ModelStats `json:"model_stats"`
}

type ModelStats struct {
	Tests       int           `json:"tests"`
	SuccessRate float64       `json:"success_rate"`
	AvgScore    float64       `json:"avg_score"`
	AvgTime     time.Duration `json:"avg_time"`
	TotalCost   float64       `json:"total_cost"`
}

// CLI Options
type CLIOptions struct {
	Model       string
	Models      string
	Prompt      string
	Prompts     string
	TestSuite   string
	TestFile    string
	Output      string
	Compare     bool
	Iterations  int
	Verbose     bool
	DryRun      bool
}

func main() {
	// Parse command line arguments
	opts := parseCLIArgs()
	
	if opts.DryRun {
		fmt.Println("🧪 Prompt Evaluation Tool - Dry Run Mode")
		fmt.Printf("Would test: Models=%s, Prompts=%s, TestSuite=%s\n", 
			getModelList(opts), getPromptList(opts), opts.TestSuite)
		return
	}

	fmt.Println("🧪 Prompt Evaluation Tool")
	fmt.Println("=========================")

	// Load configuration
	modelsConfig, err := loadModelsConfig()
	if err != nil {
		fmt.Printf("❌ Error loading models config: %v\n", err)
		os.Exit(1)
	}

	// Parse models and prompts to test
	models := parseModelList(opts.Models, opts.Model, modelsConfig)
	prompts := parsePromptList(opts.Prompts, opts.Prompt)
	
	if len(models) == 0 {
		fmt.Println("❌ No valid models specified")
		os.Exit(1)
	}
	
	if len(prompts) == 0 {
		fmt.Println("❌ No valid prompts specified")
		os.Exit(1)
	}

	// Load test cases
	var testCases []TestCase
	if opts.TestFile != "" {
		testCase, err := loadTestCase(opts.TestFile)
		if err != nil {
			fmt.Printf("❌ Error loading test case: %v\n", err)
			os.Exit(1)
		}
		testCases = []TestCase{testCase}
	} else if opts.TestSuite != "" {
		testCases, err = loadTestSuite(opts.TestSuite)
		if err != nil {
			fmt.Printf("❌ Error loading test suite: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("❌ No test suite or test file specified")
		os.Exit(1)
	}

	// Run evaluation
	run := &EvaluationRun{
		RunID:     generateRunID(),
		Timestamp: time.Now(),
		Config: RunConfig{
			Models:     models,
			Prompts:    prompts,
			TestSuite:  opts.TestSuite,
			Iterations: opts.Iterations,
		},
	}

	fmt.Printf("📊 Running evaluation: %d models × %d prompts × %d tests × %d iterations\n",
		len(models), len(prompts), len(testCases), opts.Iterations)

	// Execute tests
	for _, modelName := range models {
		for _, promptType := range prompts {
			for _, testCase := range testCases {
				for i := 0; i < opts.Iterations; i++ {
					result := runSingleTest(modelName, promptType, testCase, modelsConfig, opts.Verbose)
					run.Results = append(run.Results, result)
					
					if opts.Verbose {
						displayResult(result)
					} else {
						fmt.Print(".")
					}
				}
			}
		}
	}

	// Calculate summary
	run.Summary = calculateSummary(run.Results)
	
	// Display results
	displaySummary(run)
	
	// Save results
	if opts.Output != "" {
		err := saveResults(run, opts.Output)
		if err != nil {
			fmt.Printf("⚠️  Error saving results: %v\n", err)
		} else {
			fmt.Printf("💾 Results saved to: %s\n", opts.Output)
		}
	}
}

func parseCLIArgs() CLIOptions {
	opts := CLIOptions{}
	
	flag.StringVar(&opts.Model, "model", "", "Single model to test (e.g., gpt-5-mini)")
	flag.StringVar(&opts.Models, "models", "", "Comma-separated list of models to test")
	flag.StringVar(&opts.Prompt, "prompt", "", "Single prompt to test (e.g., base/v4_streamlined)")
	flag.StringVar(&opts.Prompts, "prompts", "", "Comma-separated list of prompts to test")
	flag.StringVar(&opts.TestSuite, "test-suite", "", "Test suite to run (e.g., coding_basic)")
	flag.StringVar(&opts.TestFile, "test", "", "Single test case file to run")
	flag.StringVar(&opts.Output, "output", "", "Output file for results (JSON)")
	flag.BoolVar(&opts.Compare, "compare", false, "Run comparative analysis")
	flag.IntVar(&opts.Iterations, "iterations", 1, "Number of iterations per test")
	flag.BoolVar(&opts.Verbose, "verbose", false, "Verbose output")
	flag.BoolVar(&opts.DryRun, "dry-run", false, "Show what would be tested without running")
	
	flag.Parse()
	
	return opts
}

func loadModelsConfig() (*ModelsConfig, error) {
	configPath := "prompt_optimization/configs/models.json"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read models config: %w", err)
	}
	
	var config ModelsConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse models config: %w", err)
	}
	
	return &config, nil
}

func parseModelList(models, model string, config *ModelsConfig) []string {
	var result []string
	
	if models != "" {
		for _, m := range strings.Split(models, ",") {
			m = strings.TrimSpace(m)
			if group, exists := config.ModelGroups[m]; exists {
				result = append(result, group...)
			} else if _, exists := config.Models[m]; exists {
				result = append(result, m)
			}
		}
	} else if model != "" {
		if _, exists := config.Models[model]; exists {
			result = append(result, model)
		}
	}
	
	return result
}

func parsePromptList(prompts, prompt string) []string {
	var result []string
	
	if prompts != "" {
		return strings.Split(prompts, ",")
	} else if prompt != "" {
		return []string{prompt}
	}
	
	return result
}

func loadTestCase(filename string) (TestCase, error) {
	var testCase TestCase
	
	path := filepath.Join("prompt_optimization/test_cases", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return testCase, err
	}
	
	err = json.Unmarshal(data, &testCase)
	return testCase, err
}

func loadTestSuite(suiteName string) ([]TestCase, error) {
	// Load test suite configuration
	suitePath := "prompt_optimization/configs/test_suites.json"
	data, err := os.ReadFile(suitePath)
	if err != nil {
		return nil, err
	}
	
	var suiteConfig map[string]interface{}
	err = json.Unmarshal(data, &suiteConfig)
	if err != nil {
		return nil, err
	}
	
	// Extract test cases for the specified suite
	testSuites := suiteConfig["test_suites"].(map[string]interface{})
	suite := testSuites[suiteName].(map[string]interface{})
	testFiles := suite["test_cases"].([]interface{})
	
	var testCases []TestCase
	for _, file := range testFiles {
		filename := file.(string)
		testCase, err := loadTestCase(filename)
		if err != nil {
			fmt.Printf("⚠️  Warning: Failed to load test case %s: %v\n", filename, err)
			continue
		}
		testCases = append(testCases, testCase)
	}
	
	return testCases, nil
}

func runSingleTest(modelName, promptType string, testCase TestCase, config *ModelsConfig, verbose bool) TestResult {
	start := time.Now()
	
	result := TestResult{
		TestID:     testCase.ID,
		ModelName:  modelName,
		PromptType: promptType,
		Timestamp:  start,
	}

	// Get model configuration
	modelConfig := config.Models[modelName]
	
	// Map model name to provider type
	var clientType api.ClientType
	switch modelConfig.Provider {
	case "openai":
		clientType = api.OpenAIClientType
	case "deepinfra":
		clientType = api.DeepInfraClientType
	case "openrouter":
		clientType = api.OpenRouterClientType
	default:
		result.Error = fmt.Sprintf("Unknown provider: %s", modelConfig.Provider)
		return result
	}

	// Create agent with explicit provider and model
	var agentInstance *agent.Agent
	var err error
	
	// Load custom prompt if specified
	if promptType != "default" {
		// For now, use default agent creation
		// TODO: Implement prompt override system
		agentInstance, err = agent.NewAgentWithProviderAndModel(clientType, modelConfig.ModelID)
	} else {
		agentInstance, err = agent.NewAgentWithProviderAndModel(clientType, modelConfig.ModelID)
	}
	
	if err != nil {
		result.Error = fmt.Sprintf("Agent creation failed: %v", err)
		result.ResponseTime = time.Since(start)
		return result
	}

	// Prepare messages
	messages := []api.Message{
		{Role: "user", Content: testCase.Input.Prompt},
	}

	// Execute test
	response, err := agentInstance.GenerateResponse(messages)
	result.ResponseTime = time.Since(start)

	if err != nil {
		result.Error = fmt.Sprintf("Response failed: %v", err)
		return result
	}

	result.Response = response
	result.Success = true
	
	// Basic scoring (placeholder - implement proper evaluation)
	result.Score = evaluateResponse(response, testCase)
	
	return result
}

func evaluateResponse(response string, testCase TestCase) int {
	// Placeholder scoring logic
	// TODO: Implement proper evaluation based on test case criteria
	
	score := 0
	maxScore := testCase.Scoring.MaxPoints
	
	// Basic heuristics
	if len(response) > 0 {
		score += 20 // Basic completion
	}
	
	if strings.Contains(strings.ToLower(response), "func") && testCase.Category == "coding" {
		score += 30 // Contains function definition for coding tasks
	}
	
	if len(response) > 200 {
		score += 20 // Reasonable detail
	}
	
	if len(response) < 2000 {
		score += 20 // Not overly verbose
	}
	
	// Cap at max score
	if score > maxScore {
		score = maxScore
	}
	
	return score
}

func calculateSummary(results []TestResult) RunSummary {
	summary := RunSummary{
		TotalTests: len(results),
		ModelStats: make(map[string]ModelStats),
	}
	
	totalScore := 0
	totalTime := time.Duration(0)
	successCount := 0
	
	modelCounts := make(map[string]int)
	modelSuccesses := make(map[string]int)
	modelScores := make(map[string]int)
	modelTimes := make(map[string]time.Duration)
	
	for _, result := range results {
		if result.Success {
			successCount++
			modelSuccesses[result.ModelName]++
		}
		
		totalScore += result.Score
		totalTime += result.ResponseTime
		
		modelCounts[result.ModelName]++
		modelScores[result.ModelName] += result.Score
		modelTimes[result.ModelName] += result.ResponseTime
	}
	
	if len(results) > 0 {
		summary.SuccessRate = float64(successCount) / float64(len(results))
		summary.AvgScore = float64(totalScore) / float64(len(results))
		summary.AvgTime = totalTime / time.Duration(len(results))
	}
	
	// Calculate per-model stats
	for model, count := range modelCounts {
		stats := ModelStats{
			Tests:       count,
			SuccessRate: float64(modelSuccesses[model]) / float64(count),
			AvgScore:    float64(modelScores[model]) / float64(count),
			AvgTime:     modelTimes[model] / time.Duration(count),
		}
		summary.ModelStats[model] = stats
	}
	
	return summary
}

func displayResult(result TestResult) {
	status := "✅"
	if !result.Success {
		status = "❌"
	}
	
	fmt.Printf("%s %s | %s | %s | %dms | Score: %d\n",
		status, result.ModelName, result.PromptType, result.TestID,
		result.ResponseTime.Milliseconds(), result.Score)
}

func displaySummary(run *EvaluationRun) {
	fmt.Printf("\n\n📊 EVALUATION SUMMARY\n")
	fmt.Printf("=====================\n")
	fmt.Printf("Run ID: %s\n", run.RunID)
	fmt.Printf("Tests: %d | Success Rate: %.1f%% | Avg Score: %.1f | Avg Time: %dms\n\n",
		run.Summary.TotalTests, run.Summary.SuccessRate*100, 
		run.Summary.AvgScore, run.Summary.AvgTime.Milliseconds())
	
	fmt.Printf("Model Performance:\n")
	fmt.Printf("------------------\n")
	for model, stats := range run.Summary.ModelStats {
		fmt.Printf("%-15s | %d tests | %.1f%% success | %.1f score | %4dms\n",
			model, stats.Tests, stats.SuccessRate*100, stats.AvgScore, stats.AvgTime.Milliseconds())
	}
}

func saveResults(run *EvaluationRun, filename string) error {
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	
	// Ensure results directory exists
	resultsDir := "prompt_optimization/results/raw"
	os.MkdirAll(resultsDir, 0755)
	
	path := filepath.Join(resultsDir, filename)
	return os.WriteFile(path, data, 0644)
}

func generateRunID() string {
	return fmt.Sprintf("run_%d", time.Now().Unix())
}

func getModelList(opts CLIOptions) string {
	if opts.Models != "" {
		return opts.Models
	}
	return opts.Model
}

func getPromptList(opts CLIOptions) string {
	if opts.Prompts != "" {
		return opts.Prompts
	}
	return opts.Prompt
}