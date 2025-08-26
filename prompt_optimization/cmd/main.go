package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/prompt_optimization/framework"
)

func main() {
	// Command line flags
	var (
		promptFile   = flag.String("prompt", "", "Path to prompt file")
		testCasesDir = flag.String("test-cases", "test_cases", "Directory containing test cases")
		resultsDir   = flag.String("results", "results", "Directory to save results")
		models       = flag.String("models", "deepinfra:google/gemini-2.5-flash", "Comma-separated list of models")
		verbose      = flag.Bool("verbose", false, "Verbose output")
		optimize     = flag.Bool("optimize", false, "Run optimization instead of just testing")
		iterations   = flag.Int("iterations", 5, "Maximum optimization iterations")
		target       = flag.Float64("target", 0.95, "Target success rate (0.0-1.0)")
		promptType   = flag.String("type", "", "Type of prompt for optimization")
	)
	flag.Parse()

	if *promptFile == "" && *promptType == "" {
		fmt.Println("Usage: prompt_optimizer --prompt <file> OR --type <prompt_type>")
		fmt.Println("  --prompt <file>         Path to prompt file to test")
		fmt.Println("  --type <type>           Type of prompt to optimize (text_replacement, code_generation, etc.)")
		fmt.Println("  --test-cases <dir>      Directory containing test cases (default: test_cases)")
		fmt.Println("  --results <dir>         Directory to save results (default: results)")
		fmt.Println("  --models <models>       Comma-separated list of models")
		fmt.Println("  --verbose               Verbose output")
		fmt.Println("  --optimize              Run optimization instead of just testing")
		fmt.Println("  --iterations <n>        Maximum optimization iterations (default: 5)")
		fmt.Println("  --target <rate>         Target success rate 0.0-1.0 (default: 0.95)")
		os.Exit(1)
	}

	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Create framework components
	tester := framework.NewPromptTester(cfg)
	optimizer := framework.NewPromptOptimizer(tester)

	// Load test cases
	err = loadTestCases(tester, *testCasesDir)
	if err != nil {
		log.Fatalf("Error loading test cases: %v", err)
	}

	modelList := strings.Split(*models, ",")

	if *optimize && *promptType != "" {
		// Run optimization mode
		fmt.Printf("🔄 Running optimization for %s prompts\n", *promptType)

		config := framework.OptimizationConfig{
			PromptType:        framework.PromptType(*promptType),
			MaxIterations:     *iterations,
			SuccessThreshold:  *target,
			Models:            modelList,
			OptimizationGoals: []string{"accuracy", "cost"},
		}

		// Create a basic prompt to start with
		basePrompt := &framework.PromptCandidate{
			ID:          fmt.Sprintf("%s_base", *promptType),
			PromptType:  framework.PromptType(*promptType),
			Content:     "Replace the specified text with the new text.",
			Description: "Basic text replacement prompt",
			Author:      "system",
			CreatedAt:   time.Now(),
		}

		result, err := optimizer.OptimizePrompt(ctx, basePrompt, config)
		if err != nil {
			log.Fatalf("Optimization error: %v", err)
		}

		// Save results
		saveOptimizationResult(result, *resultsDir)

	} else if *promptFile != "" {
		// Test single prompt mode
		prompt, err := loadPrompt(*promptFile)
		if err != nil {
			log.Fatalf("Error loading prompt: %v", err)
		}

		fmt.Printf("🧪 Testing prompt: %s\n", prompt.ID)

		var allResults []*framework.TestResult
		for _, model := range modelList {
			fmt.Printf("📝 Testing with model: %s\n", model)

			results, err := tester.TestPrompt(ctx, prompt, model)
			if err != nil {
				fmt.Printf("❌ Error testing with %s: %v\n", model, err)
				continue
			}

			allResults = append(allResults, results...)
		}

		// Save results
		saveTestResults(allResults, *resultsDir, prompt.ID)

		// Print summary
		printSummary(allResults)
	}
}

func loadTestCases(tester *framework.PromptTester, dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var testCase framework.TestCase
		if err := json.Unmarshal(data, &testCase); err != nil {
			continue
		}

		tester.AddTestCase(&testCase)
	}

	return nil
}

func loadPrompt(file string) (*framework.PromptCandidate, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))

	return &framework.PromptCandidate{
		ID:          base,
		PromptType:  framework.PromptTypeTextReplacement,
		Content:     string(content),
		Description: fmt.Sprintf("Loaded from %s", file),
		Author:      "user",
		CreatedAt:   time.Now(),
	}, nil
}

func saveTestResults(results []*framework.TestResult, dir, promptID string) error {
	os.MkdirAll(dir, 0755)

	filename := filepath.Join(dir, fmt.Sprintf("test_results_%s_%d.json", promptID, time.Now().Unix()))

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

func saveOptimizationResult(result *framework.OptimizationResult, dir string) error {
	os.MkdirAll(dir, 0755)

	filename := filepath.Join(dir, fmt.Sprintf("optimization_%s_%d.json",
		result.OriginalPrompt.PromptType, time.Now().Unix()))

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

func printSummary(results []*framework.TestResult) {
	if len(results) == 0 {
		fmt.Println("No results to summarize")
		return
	}

	successful := 0
	totalCost := 0.0

	for _, result := range results {
		if result.Success {
			successful++
		}
		totalCost += result.Cost
	}

	successRate := float64(successful) / float64(len(results)) * 100
	avgCost := totalCost / float64(len(results))

	fmt.Printf("\n📊 Test Summary:\n")
	fmt.Printf("   Total tests: %d\n", len(results))
	fmt.Printf("   Successful: %d (%.1f%%)\n", successful, successRate)
	fmt.Printf("   Total cost: $%.4f\n", totalCost)
	fmt.Printf("   Average cost: $%.4f\n", avgCost)
}
