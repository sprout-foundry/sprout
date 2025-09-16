package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// Real-world test configuration structures
type GitSetup struct {
	BaseBranch        string            `json:"base_branch"`
	TestBranch        string            `json:"test_branch"`
	TempDirectory     string            `json:"temp_directory"`
	RepositoryURL     string            `json:"repository_url"`
	IntroducedChanges IntroducedChanges `json:"introduced_changes"`
}

type IntroducedChanges struct {
	FilesToModify     []string `json:"files_to_modify"`
	IssuesToIntroduce []string `json:"issues_to_introduce"`
}

type RealWorldTestCase struct {
	ID               string                    `json:"id"`
	Name             string                    `json:"name"`
	Description      string                    `json:"description"`
	Category         string                    `json:"category"`
	Difficulty       string                    `json:"difficulty"`
	EstimatedTimeMin int                       `json:"estimated_time_minutes"`
	GitSetup         GitSetup                  `json:"git_setup"`
	Input            RealWorldTestInput        `json:"input"`
	ExpectedOutputs  map[string]interface{}    `json:"expected_outputs"`
	Evaluation       RealWorldEvaluation       `json:"evaluation"`
	Scoring          map[string]int            `json:"scoring"`
	SuccessCriteria  map[string][]string       `json:"success_criteria"`
	Validation       map[string]string         `json:"validation"`
}

type RealWorldTestInput struct {
	Prompt           string `json:"prompt"`
	WorkingDirectory string `json:"working_directory"`
	Context          string `json:"context"`
}

type RealWorldEvaluation struct {
	AgenticCapabilities     []string `json:"agentic_capabilities"`
	TechnicalRequirements   []string `json:"technical_requirements"`
	ImplementationQuality   []string `json:"implementation_quality"`
}

// Test execution and management
type RealWorldTestRunner struct {
	config           *AgenticConfig
	outputFile       string
	verbose          bool
	dryRun          bool
	timeout         time.Duration
	iterations      int
	projectRoot     string
	tempDirs        []string
}

func NewRealWorldTestRunner(configPath, outputFile string, verbose, dryRun bool, timeout time.Duration, iterations int) (*RealWorldTestRunner, error) {
	config, err := loadAgenticConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	projectRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	return &RealWorldTestRunner{
		config:      config,
		outputFile:  outputFile,
		verbose:     verbose,
		dryRun:     dryRun,
		timeout:     timeout,
		iterations:  iterations,
		projectRoot: projectRoot,
		tempDirs:    []string{},
	}, nil
}

func (r *RealWorldTestRunner) SetupTestBranch(testCase *RealWorldTestCase) (string, error) {
	// Create unique temp directory
	timestamp := time.Now().Format("20060102_150405")
	tempDir := strings.Replace(testCase.GitSetup.TempDirectory, "{timestamp}", timestamp, -1)
	
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	r.tempDirs = append(r.tempDirs, tempDir)

	// Clone the current repository to temp location
	cmd := exec.Command("git", "clone", ".", tempDir)
	cmd.Dir = r.projectRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to clone repository: %w, output: %s", err, output)
	}

	// Create and checkout test branch
	cmd = exec.Command("git", "checkout", "-b", testCase.GitSetup.TestBranch, testCase.GitSetup.BaseBranch)
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to create test branch: %w, output: %s", err, output)
	}

	// Introduce realistic issues into the codebase
	if err := r.introduceIssues(tempDir, &testCase.GitSetup.IntroducedChanges); err != nil {
		return "", fmt.Errorf("failed to introduce issues: %w", err)
	}

	// Commit the introduced issues
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to stage changes: %w, output: %s", err, output)
	}

	cmd = exec.Command("git", "commit", "-m", fmt.Sprintf("Introduce test issues for %s", testCase.ID))
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to commit changes: %w, output: %s", err, output)
	}

	return tempDir, nil
}

func (r *RealWorldTestRunner) introduceIssues(tempDir string, changes *IntroducedChanges) error {
	// This would contain logic to introduce realistic issues based on the test case
	// For now, we'll implement a simple example for the modular architecture case
	
	for _, file := range changes.FilesToModify {
		filePath := filepath.Join(tempDir, file)
		
		// Read the file
		content, err := os.ReadFile(filePath)
		if err != nil {
			if r.verbose {
				fmt.Printf("⚠️  Could not read file %s (may not exist): %v\n", file, err)
			}
			continue
		}

		// Introduce issues based on the file type and issues list
		modifiedContent := r.introduceIssuesInFile(string(content), file, changes.IssuesToIntroduce)
		
		// Write back the modified content
		if err := os.WriteFile(filePath, []byte(modifiedContent), 0644); err != nil {
			return fmt.Errorf("failed to write modified file %s: %w", file, err)
		}

		if r.verbose {
			fmt.Printf("📝 Modified %s to introduce test issues\n", file)
		}
	}

	return nil
}

func (r *RealWorldTestRunner) introduceIssuesInFile(content, filename string, issues []string) string {
	// Simple example implementations - in practice this would be more sophisticated
	
	if strings.Contains(filename, "agent.go") {
		// Add circular dependency
		if contains(issues, "Add circular import between agent and orchestration") {
			if !strings.Contains(content, "github.com/alantheprice/ledit/pkg/orchestration") {
				// Add import
				content = strings.Replace(content, 
					"import (",
					"import (\n\t\"github.com/alantheprice/ledit/pkg/orchestration\"", 1)
				
				// Add problematic usage
				content = strings.Replace(content,
					"func (a *Agent) ExecuteTask",
					"func (a *Agent) ExecuteTask(coord *orchestration.Coordinator",
					1)
			}
		}
	}

	if strings.Contains(filename, "interface.go") {
		// Remove interface abstractions
		if contains(issues, "Remove interface abstractions from agent_api") {
			// Replace interface definitions with concrete types
			content = strings.Replace(content, "type AgentAPI interface", "type AgentAPI struct", -1)
		}
	}

	// Add hardcoded configuration
	if contains(issues, "Hardcode configuration values instead of using config") {
		content = strings.Replace(content, "config.GetTimeout()", "30*time.Second", -1)
		content = strings.Replace(content, "config.GetMaxRetries()", "3", -1)
	}

	return content
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (r *RealWorldTestRunner) RunTest(testCase *RealWorldTestCase, provider string, model string) (*AgenticTestResult, error) {
	if r.dryRun {
		fmt.Printf("Would run real-world test: %s with %s/%s\n", testCase.ID, provider, model)
		return &AgenticTestResult{
			TestCaseID: testCase.ID,
			Success:    true,
			Score:      85,
			Duration:   time.Second * 30,
		}, nil
	}

	// Setup test environment
	tempDir, err := r.SetupTestBranch(testCase)
	if err != nil {
		return nil, fmt.Errorf("failed to setup test branch: %w", err)
	}

	// Create agent for testing
	agentClient, err := createAgent(provider, model)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// Prepare the test input with actual working directory
	testInput := testCase.Input
	testInput.WorkingDirectory = tempDir

	// Build the full prompt with context
	fullPrompt := fmt.Sprintf("%s\n\nContext: %s\n\nWorking Directory: %s", 
		testInput.Prompt, testInput.Context, testInput.WorkingDirectory)

	startTime := time.Now()

	if r.verbose {
		fmt.Printf("📂 Working in: %s\n", tempDir)
		fmt.Printf("📝 Prompt length: %d characters\n", len(fullPrompt))
	}

	// Execute the agent task
	response, err := agentClient.ProcessQuery(fullPrompt)

	duration := time.Since(startTime)

	if err != nil {
		return &AgenticTestResult{
			TestCaseID:   testCase.ID,
			Success:      false,
			Score:        0,
			Duration:     duration,
			ErrorMessage: err.Error(),
		}, nil
	}

	// Evaluate the results
	score := r.evaluateRealWorldResult(testCase, tempDir, response)

	// Run validation checks
	validationPassed := r.runValidationChecks(testCase, tempDir)

	return &AgenticTestResult{
		TestCaseID: testCase.ID,
		Success:    validationPassed && score >= 40,
		Score:      score,
		Duration:   duration,
		Response:   response,
		Details:    map[string]interface{}{
			"validation_passed": validationPassed,
			"working_directory": tempDir,
		},
	}, nil
}

func (r *RealWorldTestRunner) runValidationChecks(testCase *RealWorldTestCase, workingDir string) bool {
	if r.verbose {
		fmt.Printf("🔍 Running validation checks in %s\n", workingDir)
	}

	allPassed := true

	for checkName, checkCommand := range testCase.Validation {
		if r.verbose {
			fmt.Printf("   Running %s: %s\n", checkName, checkCommand)
		}

		parts := strings.Fields(checkCommand)
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Dir = workingDir

		output, err := cmd.CombinedOutput()
		
		if err != nil {
			if r.verbose {
				fmt.Printf("   ❌ %s failed: %v\n", checkName, err)
				fmt.Printf("   Output: %s\n", string(output))
			}
			allPassed = false
		} else {
			if r.verbose {
				fmt.Printf("   ✅ %s passed\n", checkName)
			}
		}
	}

	return allPassed
}

func (r *RealWorldTestRunner) evaluateRealWorldResult(testCase *RealWorldTestCase, workingDir string, response string) int {
	// Implement sophisticated evaluation based on:
	// 1. Code changes made
	// 2. Validation checks passed
	// 3. Quality of architectural improvements
	// 4. Response analysis

	score := 0
	maxScore := 100

	// Basic response analysis (simplified)
	if len(response) > 1000 {
		score += 20 // Detailed response
	}

	// Check if key concepts are mentioned
	expectedConcepts := []string{
		"interface", "dependency injection", "circular", "refactor", "architecture",
		"modular", "testable", "error handling", "configuration",
	}

	conceptsFound := 0
	responseLower := strings.ToLower(response)
	for _, concept := range expectedConcepts {
		if strings.Contains(responseLower, concept) {
			conceptsFound++
		}
	}

	score += (conceptsFound * 60) / len(expectedConcepts)

	// File system checks could be added here
	// Check if files were actually modified, new interfaces created, etc.

	if score > maxScore {
		score = maxScore
	}

	return score
}

func (r *RealWorldTestRunner) Cleanup() {
	for _, tempDir := range r.tempDirs {
		if err := os.RemoveAll(tempDir); err != nil {
			fmt.Printf("⚠️  Failed to cleanup temp directory %s: %v\n", tempDir, err)
		} else if r.verbose {
			fmt.Printf("🧹 Cleaned up %s\n", tempDir)
		}
	}
}

func loadRealWorldTestCase(filePath string) (*RealWorldTestCase, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read test case file: %w", err)
	}

	var testCase RealWorldTestCase
	if err := json.Unmarshal(data, &testCase); err != nil {
		return nil, fmt.Errorf("failed to parse test case: %w", err)
	}

	return &testCase, nil
}

// Reuse existing structures and functions from agentic_eval.go
type AgenticTestResult struct {
	TestCaseID   string                 `json:"test_case_id"`
	Success      bool                   `json:"success"`
	Score        int                    `json:"score"`
	Duration     time.Duration          `json:"duration"`
	Response     string                 `json:"response"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	Details      map[string]interface{} `json:"details,omitempty"`
}

type AgenticConfig struct {
	TestSuites                map[string]AgenticTestSuite           `json:"test_suites"`
	ProviderModelCombinations map[string][]ProviderModelCombination `json:"provider_model_combinations"`
	EvaluationCriteria        map[string]EvaluationCriterion        `json:"evaluation_criteria"`
	SuccessThresholds         map[string]int                        `json:"success_thresholds"`
}

type AgenticTestSuite struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	TestCases          []string `json:"test_cases"`
	TimeoutSecs        int    `json:"timeout_seconds"`
	RunsPerTest        int    `json:"runs_per_test"`
	Priority           string `json:"priority"`
	RequiresGitCheckout bool  `json:"requires_git_checkout,omitempty"`
	CleanupBranches     bool  `json:"cleanup_branches,omitempty"`
}

type ProviderModelCombination struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Name     string `json:"name"`
}

type EvaluationCriterion struct {
	Weight      float64 `json:"weight"`
	Description string  `json:"description"`
}

func loadAgenticConfig(configPath string) (*AgenticConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config AgenticConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func createAgent(provider, model string) (*agent.Agent, error) {
	var clientType api.ClientType
	switch provider {
	case "openai":
		clientType = api.OpenAIClientType
	case "deepinfra":
		clientType = api.DeepInfraClientType
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	return agent.NewAgentWithModel(model)
}

func main() {
	var (
		testCase    = flag.String("test-case", "", "Path to real-world test case JSON file")
		provider    = flag.String("provider", "deepinfra", "AI provider to use")
		model       = flag.String("model", "Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo", "Model to use")
		outputFile  = flag.String("output", "real_world_results.json", "Output file for results")
		verbose     = flag.Bool("verbose", false, "Enable verbose output")
		dryRun      = flag.Bool("dry-run", false, "Show what would be tested without execution")
		timeoutSecs = flag.Int("timeout", 900, "Timeout in seconds for each test")
		iterations  = flag.Int("iterations", 1, "Number of iterations to run")
	)
	flag.Parse()

	if *testCase == "" {
		fmt.Println("❌ Please specify a test case file with -test-case")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Println("🌍 Real-World Agentic Problem-Solving Evaluation")
	fmt.Println("================================================")

	// Load test case
	testCaseData, err := loadRealWorldTestCase(*testCase)
	if err != nil {
		fmt.Printf("❌ Failed to load test case: %v\n", err)
		os.Exit(1)
	}

	// Create test runner
	runner, err := NewRealWorldTestRunner("configs/agentic_test_suite.json", *outputFile, *verbose, *dryRun, time.Duration(*timeoutSecs)*time.Second, *iterations)
	if err != nil {
		fmt.Printf("❌ Failed to create test runner: %v\n", err)
		os.Exit(1)
	}
	defer runner.Cleanup()

	if *dryRun {
		fmt.Printf("👀 Dry run - would test: %s with %s/%s\n", testCaseData.Name, *provider, *model)
		fmt.Printf("   Working directory: %s\n", strings.Replace(testCaseData.GitSetup.TempDirectory, "{timestamp}", "TIMESTAMP", -1))
		fmt.Printf("   Estimated time: %d minutes\n", testCaseData.EstimatedTimeMin)
		return
	}

	fmt.Printf("🧠 Testing %s with %s on %s\n", *model, *provider, testCaseData.Name)
	fmt.Printf("⏱️  Estimated time: %d minutes\n", testCaseData.EstimatedTimeMin)

	// Run the test
	result, err := runner.RunTest(testCaseData, *provider, *model)
	if err != nil {
		fmt.Printf("❌ Test execution failed: %v\n", err)
		os.Exit(1)
	}

	// Display results
	status := "❌"
	if result.Success {
		status = "✅"
	}

	fmt.Printf("%s %s | %s | Score: %d/100 | %v\n", 
		status, *model, testCaseData.ID, result.Score, result.Duration.Round(time.Millisecond*100))

	if result.ErrorMessage != "" {
		fmt.Printf("   Error: %s\n", result.ErrorMessage)
	}

	// Save results
	if err := saveResults(*outputFile, []*AgenticTestResult{result}); err != nil {
		fmt.Printf("⚠️  Failed to save results: %v\n", err)
	} else {
		fmt.Printf("💾 Results saved to: %s\n", *outputFile)
	}
}

func saveResults(filename string, results []*AgenticTestResult) error {
	data, err := json.MarshalIndent(map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"results":   results,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}