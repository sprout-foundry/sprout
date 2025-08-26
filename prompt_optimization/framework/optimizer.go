package framework

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// PromptOptimizer handles iterative prompt improvement
type PromptOptimizer struct {
	tester *PromptTester
}

// NewPromptOptimizer creates a new prompt optimizer
func NewPromptOptimizer(tester *PromptTester) *PromptOptimizer {
	return &PromptOptimizer{
		tester: tester,
	}
}

// OptimizePrompt iteratively improves a prompt based on test results
func (po *PromptOptimizer) OptimizePrompt(ctx context.Context,
	basePrompt *PromptCandidate,
	config OptimizationConfig) (*OptimizationResult, error) {

	result := &OptimizationResult{
		OriginalPrompt: basePrompt,
		CreatedAt:      time.Now(),
	}

	currentPrompt := basePrompt
	bestPrompt := basePrompt
	var bestMetrics PromptMetrics
	var allResults []*TestResult

	fmt.Printf("🎯 Starting optimization for %s prompt (ID: %s)\n", config.PromptType, basePrompt.ID)
	fmt.Printf("📊 Target success rate: %.1f%%, Max iterations: %d\n",
		config.SuccessThreshold*100, config.MaxIterations)

	// Test the original prompt
	fmt.Printf("\n1️⃣ Testing original prompt...\n")
	startTime := time.Now()

	for i := 0; i < config.MaxIterations; i++ {
		// Test current prompt against all models
		var iterationResults []*TestResult
		for _, model := range config.Models {
			results, err := po.tester.TestPrompt(ctx, currentPrompt, model)
			if err != nil {
				fmt.Printf("❌ Error testing prompt with %s: %v\n", model, err)
				continue
			}
			iterationResults = append(iterationResults, results...)
		}

		allResults = append(allResults, iterationResults...)

		// Calculate metrics for this iteration
		metrics := po.calculateMetrics(iterationResults, currentPrompt.ID)

		fmt.Printf("📈 Iteration %d: Success rate %.1f%%, Avg cost $%.4f\n",
			i+1, metrics.SuccessRate*100, metrics.AverageCost)

		// Check if this is our best result so far
		if po.isBetter(metrics, bestMetrics, config.OptimizationGoals) {
			bestMetrics = metrics
			bestPrompt = currentPrompt
			fmt.Printf("✨ New best result! Success: %.1f%%\n", metrics.SuccessRate*100)
		}

		// Check if we've met our success threshold
		if metrics.SuccessRate >= config.SuccessThreshold {
			fmt.Printf("🎉 Success threshold reached! (%.1f%% >= %.1f%%)\n",
				metrics.SuccessRate*100, config.SuccessThreshold*100)
			break
		}

		// Generate improvements based on failures
		if i < config.MaxIterations-1 {
			fmt.Printf("🔄 Generating improvements for next iteration...\n")

			// Analyze failures and generate improvement suggestions
			failures := po.analyzeFailures(iterationResults)
			improvements := po.generateImprovements(failures, config.OptimizationGoals)

			// Create a new prompt variant with improvements
			newPrompt, err := po.generateVariation(currentPrompt, improvements)
			if err != nil {
				fmt.Printf("⚠️ Error generating prompt variation: %v\n", err)
				continue
			}

			currentPrompt = newPrompt
			fmt.Printf("📝 Generated new prompt variant: %s\n", newPrompt.ID)
		}
	}

	// Finalize result
	result.BestPrompt = bestPrompt
	result.AllResults = allResults
	result.TotalDuration = time.Since(startTime)
	result.Iterations = len(allResults) / len(config.Models)
	result.TotalCost = po.calculateTotalCost(allResults)
	result.SuccessRate = bestMetrics.SuccessRate

	// Calculate improvements
	if len(allResults) > 0 {
		originalResults := allResults[:len(config.Models)]
		originalMetrics := po.calculateMetrics(originalResults, basePrompt.ID)
		result.QualityImprovement = bestMetrics.QualityScore - originalMetrics.QualityScore
		if originalMetrics.AverageCost > 0 {
			result.CostReduction = (originalMetrics.AverageCost - bestMetrics.AverageCost) / originalMetrics.AverageCost
		}
	}

	fmt.Printf("\n🎯 Optimization complete!\n")
	fmt.Printf("📊 %s\n", po.generateOptimizationSummary(result))

	return result, nil
}

// calculateMetrics computes metrics for a set of test results
func (po *PromptOptimizer) calculateMetrics(results []*TestResult, promptID string) PromptMetrics {
	if len(results) == 0 {
		return PromptMetrics{PromptID: promptID}
	}

	var successCount, totalCost float64
	var qualityScore float64

	for _, result := range results {
		if result.Success {
			successCount++
		}
		totalCost += result.Cost
		qualityScore += calculateQualityScore(result.ValidationResults)
	}

	return PromptMetrics{
		PromptID:     promptID,
		SuccessRate:  successCount / float64(len(results)),
		AverageCost:  totalCost / float64(len(results)),
		QualityScore: qualityScore / float64(len(results)),
		TotalTests:   len(results),
	}
}

// generateVariation creates a new prompt based on improvement suggestions
func (po *PromptOptimizer) generateVariation(currentPrompt *PromptCandidate, improvements []string) (*PromptCandidate, error) {
	// Apply improvements to create new content
	newContent := po.applyImprovements(currentPrompt.Content, improvements)

	// Create new prompt candidate
	newPrompt := &PromptCandidate{
		ID:          fmt.Sprintf("%s_v%d", currentPrompt.ID, time.Now().Unix()),
		Version:     fmt.Sprintf("auto_%d", time.Now().Unix()),
		PromptType:  currentPrompt.PromptType,
		Content:     newContent,
		Description: fmt.Sprintf("Auto-generated variation of %s", currentPrompt.ID),
		Author:      "prompt_optimizer",
		CreatedAt:   time.Now(),
		Parent:      currentPrompt.ID,
	}

	return newPrompt, nil
}

// analyzeFailures identifies common failure patterns in test results
func (po *PromptOptimizer) analyzeFailures(results []*TestResult) map[string]int {
	failurePatterns := make(map[string]int)

	for _, result := range results {
		if !result.Success {
			for _, validation := range result.ValidationResults {
				if !validation.Passed {
					failurePatterns[validation.Check]++
				}
			}
		}
	}

	return failurePatterns
}

// generateImprovements suggests improvements based on failure patterns
func (po *PromptOptimizer) generateImprovements(failures map[string]int, goals []string) []string {
	var improvements []string

	// Common improvement strategies based on failure patterns
	for failure := range failures {
		switch {
		case strings.Contains(failure, "contains_"):
			missing := strings.TrimPrefix(failure, "contains_")
			improvements = append(improvements,
				fmt.Sprintf("Add explicit instruction to include '%s'", missing))

		case strings.Contains(failure, "format_"):
			improvements = append(improvements,
				"Add format specification and examples")

		case strings.Contains(failure, "length_"):
			improvements = append(improvements,
				"Add length constraints to instructions")

		case strings.Contains(failure, "code_"):
			improvements = append(improvements,
				"Add specific code structure requirements")
		}
	}

	// Goal-specific improvements
	for _, goal := range goals {
		switch goal {
		case "accuracy":
			improvements = append(improvements, "Add verification step")
		case "cost":
			improvements = append(improvements, "Make instructions more concise")
		case "speed":
			improvements = append(improvements, "Reduce prompt complexity")
		}
	}

	return improvements
}

// applyImprovements modifies prompt content based on improvement suggestions
func (po *PromptOptimizer) applyImprovements(content string, improvements []string) string {
	newContent := content

	for _, improvement := range improvements {
		switch {
		case strings.Contains(improvement, "include"):
			// Extract the required element and add instruction
			re := regexp.MustCompile(`include '([^']+)'`)
			matches := re.FindStringSubmatch(improvement)
			if len(matches) > 1 {
				required := matches[1]
				addition := fmt.Sprintf("\n\nIMPORTANT: Your response must include '%s'.", required)
				newContent += addition
			}

		case strings.Contains(improvement, "format"):
			newContent += "\n\nFormat your response exactly as specified, with proper structure and syntax."

		case strings.Contains(improvement, "length"):
			newContent += "\n\nEnsure your response meets the specified length requirements."

		case strings.Contains(improvement, "verification"):
			newContent += "\n\nBefore finalizing your response, verify it meets all requirements."

		case strings.Contains(improvement, "concise"):
			// Try to make the prompt more concise
			newContent = po.makeMoreConcise(newContent)
		}
	}

	return newContent
}

// makeMoreConcise attempts to reduce prompt length while preserving meaning
func (po *PromptOptimizer) makeMoreConcise(content string) string {
	// Simple conciseness improvements
	content = strings.ReplaceAll(content, "please ", "")
	content = strings.ReplaceAll(content, "I would like you to ", "")
	content = strings.ReplaceAll(content, "Could you ", "")
	content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
	return strings.TrimSpace(content)
}

// isBetter determines if new metrics are better than current best
func (po *PromptOptimizer) isBetter(newMetrics, bestMetrics PromptMetrics, goals []string) bool {
	if bestMetrics.PromptID == "" {
		return true // First result is always better than nothing
	}

	// Calculate weighted score based on optimization goals
	score := 0.0
	bestScore := 0.0

	for _, goal := range goals {
		switch goal {
		case "accuracy":
			score += newMetrics.SuccessRate * 0.4
			bestScore += bestMetrics.SuccessRate * 0.4
		case "cost":
			// Lower cost is better, so invert the score
			if bestMetrics.AverageCost > 0 {
				score += (1.0 - (newMetrics.AverageCost / bestMetrics.AverageCost)) * 0.3
			}
		case "quality":
			score += newMetrics.QualityScore * 0.3
			bestScore += bestMetrics.QualityScore * 0.3
		}
	}

	return score > bestScore
}

// calculateTotalCost sums up all test costs
func (po *PromptOptimizer) calculateTotalCost(results []*TestResult) float64 {
	total := 0.0
	for _, result := range results {
		total += result.Cost
	}
	return total
}

// generateOptimizationSummary creates a human-readable summary
func (po *PromptOptimizer) generateOptimizationSummary(result *OptimizationResult) string {
	summary := fmt.Sprintf(
		"Optimization completed in %d iterations over %v.\n"+
			"Success rate improved from baseline to %.1f%%.\n"+
			"Total cost: $%.4f across %d tests.\n",
		result.Iterations,
		result.TotalDuration.Round(time.Second),
		result.SuccessRate*100,
		result.TotalCost,
		len(result.AllResults),
	)

	if result.QualityImprovement > 0 {
		summary += fmt.Sprintf("Quality improved by %.2f points.\n", result.QualityImprovement)
	}

	if result.CostReduction > 0 {
		summary += fmt.Sprintf("Cost reduced by %.1f%%.\n", result.CostReduction*100)
	}

	return summary
}

// calculateQualityScore computes quality score from validation results
func calculateQualityScore(validations []ValidationResult) float64 {
	if len(validations) == 0 {
		return 0.0
	}

	totalScore := 0.0
	for _, v := range validations {
		if v.Passed {
			totalScore += v.Score
		}
	}
	return totalScore / float64(len(validations))
}
