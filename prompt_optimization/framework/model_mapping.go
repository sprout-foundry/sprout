package framework

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ModelPromptMapping manages the optimal prompts for each model
type ModelPromptMapping struct {
	ModelMappings map[string]*ModelOptimalPrompts `json:"model_mappings"`
	LastUpdated   time.Time                       `json:"last_updated"`
	Version       string                          `json:"version"`
}

// ModelOptimalPrompts stores the best prompt for each task type per model
type ModelOptimalPrompts struct {
	ModelName        string                          `json:"model_name"`
	PromptMappings   map[PromptType]*PromptCandidate `json:"prompt_mappings"`
	PerformanceData  map[PromptType]*ModelMetrics    `json:"performance_data"`
	LastOptimized    time.Time                       `json:"last_optimized"`
	OptimizationRuns int                             `json:"optimization_runs"`
}

// ModelMetrics tracks performance metrics for a specific model-prompt combination
type ModelMetrics struct {
	SuccessRate     float64       `json:"success_rate"`
	AverageCost     float64       `json:"average_cost"`
	AverageLatency  time.Duration `json:"average_latency"`
	QualityScore    float64       `json:"quality_score"`
	TestCount       int           `json:"test_count"`
	LastTested      time.Time     `json:"last_tested"`
	ConfidenceLevel float64       `json:"confidence_level"` // Based on test count and consistency
}

// ModelPromptManager handles model-specific prompt optimization and selection
type ModelPromptManager struct {
	mappings     *ModelPromptMapping
	mappingsFile string
	promptsDir   string
}

// NewModelPromptManager creates a new model-prompt mapping manager
func NewModelPromptManager(mappingsFile, promptsDir string) *ModelPromptManager {
	return &ModelPromptManager{
		mappingsFile: mappingsFile,
		promptsDir:   promptsDir,
		mappings: &ModelPromptMapping{
			ModelMappings: make(map[string]*ModelOptimalPrompts),
			Version:       "1.0",
		},
	}
}

// LoadMappings loads existing model-prompt mappings from file
func (mpm *ModelPromptManager) LoadMappings() error {
	if _, err := os.Stat(mpm.mappingsFile); os.IsNotExist(err) {
		// File doesn't exist, start with empty mappings
		mpm.mappings.LastUpdated = time.Now()
		return nil
	}

	data, err := os.ReadFile(mpm.mappingsFile)
	if err != nil {
		return fmt.Errorf("failed to read mappings file: %w", err)
	}

	err = json.Unmarshal(data, mpm.mappings)
	if err != nil {
		return fmt.Errorf("failed to parse mappings file: %w", err)
	}

	return nil
}

// SaveMappings saves the current mappings to file
func (mpm *ModelPromptManager) SaveMappings() error {
	mpm.mappings.LastUpdated = time.Now()

	// Ensure directory exists
	dir := filepath.Dir(mpm.mappingsFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create mappings directory: %w", err)
	}

	data, err := json.MarshalIndent(mpm.mappings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mappings: %w", err)
	}

	err = os.WriteFile(mpm.mappingsFile, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write mappings file: %w", err)
	}

	return nil
}

// GetOptimalPrompt retrieves the best prompt for a given model and task type
func (mpm *ModelPromptManager) GetOptimalPrompt(modelName string, promptType PromptType) (*PromptCandidate, *ModelMetrics, error) {
	// Normalize model name
	normalizedModel := mpm.normalizeModelName(modelName)

	modelPrompts, exists := mpm.mappings.ModelMappings[normalizedModel]
	if !exists {
		return nil, nil, fmt.Errorf("no optimized prompts found for model %s", normalizedModel)
	}

	prompt, exists := modelPrompts.PromptMappings[promptType]
	if !exists {
		return nil, nil, fmt.Errorf("no optimized prompt found for model %s and prompt type %s", normalizedModel, promptType)
	}

	metrics := modelPrompts.PerformanceData[promptType]
	return prompt, metrics, nil
}

// UpdateOptimalPrompt updates the best prompt for a model-task combination
func (mpm *ModelPromptManager) UpdateOptimalPrompt(modelName string, promptType PromptType,
	prompt *PromptCandidate, metrics *ModelMetrics) error {

	normalizedModel := mpm.normalizeModelName(modelName)

	// Ensure model entry exists
	if _, exists := mpm.mappings.ModelMappings[normalizedModel]; !exists {
		mpm.mappings.ModelMappings[normalizedModel] = &ModelOptimalPrompts{
			ModelName:        normalizedModel,
			PromptMappings:   make(map[PromptType]*PromptCandidate),
			PerformanceData:  make(map[PromptType]*ModelMetrics),
			LastOptimized:    time.Now(),
			OptimizationRuns: 0,
		}
	}

	modelPrompts := mpm.mappings.ModelMappings[normalizedModel]

	// Check if this is better than existing prompt
	if existingMetrics, exists := modelPrompts.PerformanceData[promptType]; exists {
		if !mpm.isImprovement(metrics, existingMetrics) {
			return fmt.Errorf("new prompt does not improve on existing metrics")
		}
	}

	// Update the optimal prompt and metrics
	modelPrompts.PromptMappings[promptType] = prompt
	modelPrompts.PerformanceData[promptType] = metrics
	modelPrompts.LastOptimized = time.Now()
	modelPrompts.OptimizationRuns++

	return mpm.SaveMappings()
}

// GetAvailableModels returns all models with optimized prompts
func (mpm *ModelPromptManager) GetAvailableModels() []string {
	models := make([]string, 0, len(mpm.mappings.ModelMappings))
	for model := range mpm.mappings.ModelMappings {
		models = append(models, model)
	}
	return models
}

// GetModelSummary provides a summary of optimization status for a model
func (mpm *ModelPromptManager) GetModelSummary(modelName string) (*ModelOptimalPrompts, error) {
	normalizedModel := mpm.normalizeModelName(modelName)

	modelPrompts, exists := mpm.mappings.ModelMappings[normalizedModel]
	if !exists {
		return nil, fmt.Errorf("no data found for model %s", normalizedModel)
	}

	return modelPrompts, nil
}

// IdentifyBestModelForTask suggests the best model for a specific task type
func (mpm *ModelPromptManager) IdentifyBestModelForTask(promptType PromptType) (string, *ModelMetrics, error) {
	var bestModel string
	var bestMetrics *ModelMetrics
	bestScore := 0.0

	for modelName, modelPrompts := range mpm.mappings.ModelMappings {
		if metrics, exists := modelPrompts.PerformanceData[promptType]; exists {
			// Calculate composite score (success rate weighted by confidence and cost efficiency)
			costEfficiency := 1.0 / (metrics.AverageCost + 0.001) // Avoid division by zero
			score := metrics.SuccessRate * metrics.ConfidenceLevel * costEfficiency * 0.1

			if score > bestScore {
				bestScore = score
				bestModel = modelName
				bestMetrics = metrics
			}
		}
	}

	if bestModel == "" {
		return "", nil, fmt.Errorf("no models found with data for prompt type %s", promptType)
	}

	return bestModel, bestMetrics, nil
}

// CreateModelSpecificPrompts generates different prompt variants optimized for different model families
func (mpm *ModelPromptManager) CreateModelSpecificPrompts(basePrompt *PromptCandidate, models []string) []*PromptCandidate {
	var variants []*PromptCandidate

	for _, model := range models {
		variant := mpm.adaptPromptForModel(basePrompt, model)
		if variant != nil {
			variants = append(variants, variant)
		}
	}

	return variants
}

// adaptPromptForModel creates a model-specific variant of a prompt
func (mpm *ModelPromptManager) adaptPromptForModel(basePrompt *PromptCandidate, modelName string) *PromptCandidate {
	normalizedModel := mpm.normalizeModelName(modelName)
	content := basePrompt.Content

	// Model-specific optimizations based on known model characteristics
	switch {
	case strings.Contains(normalizedModel, "qwen"):
		// Qwen models prefer clear, structured instructions
		content = mpm.addQwenOptimizations(content)

	case strings.Contains(normalizedModel, "deepseek"):
		// DeepSeek models work well with reasoning-based prompts
		content = mpm.addDeepSeekOptimizations(content)

	case strings.Contains(normalizedModel, "claude"):
		// Claude models prefer conversational, detailed instructions
		content = mpm.addClaudeOptimizations(content)

	case strings.Contains(normalizedModel, "gpt"):
		// GPT models work well with examples and clear formatting
		content = mpm.addGPTOptimizations(content)

	case strings.Contains(normalizedModel, "gemini"):
		// Gemini models prefer structured, step-by-step instructions
		content = mpm.addGeminiOptimizations(content)
	}

	return &PromptCandidate{
		ID:          fmt.Sprintf("%s_%s_%d", basePrompt.ID, normalizedModel, time.Now().Unix()),
		Version:     fmt.Sprintf("%s_adapted", basePrompt.Version),
		PromptType:  basePrompt.PromptType,
		Content:     content,
		Description: fmt.Sprintf("%s (adapted for %s)", basePrompt.Description, normalizedModel),
		Author:      "model_prompt_manager",
		CreatedAt:   time.Now(),
		Parent:      basePrompt.ID,
	}
}

// Model-specific optimization functions
func (mpm *ModelPromptManager) addQwenOptimizations(content string) string {
	return fmt.Sprintf(`## Task Instructions

%s

## Important Guidelines:
- Follow instructions precisely
- Output only the requested content
- Maintain original formatting
- Do not add explanations unless requested`, content)
}

func (mpm *ModelPromptManager) addDeepSeekOptimizations(content string) string {
	return fmt.Sprintf(`Let me think step by step about this task:

1. **Understanding**: %s

2. **Execution**:
   - I will carefully analyze the input
   - Apply the transformation as specified
   - Verify the result meets requirements

3. **Output**: I will provide only the requested result without additional commentary.

Execute:`, content)
}

func (mpm *ModelPromptManager) addClaudeOptimizations(content string) string {
	return fmt.Sprintf(`I need to help you with the following task:

%s

I'll approach this systematically:
- First, I'll understand exactly what needs to be done
- Then I'll perform the task carefully
- Finally, I'll provide the result in the exact format you need

Here's my response:`, content)
}

func (mpm *ModelPromptManager) addGPTOptimizations(content string) string {
	return fmt.Sprintf(`Task: %s

Format: Provide only the final result
Requirements:
- Be precise and accurate
- Follow the instructions exactly
- Do not include additional commentary

Result:`, content)
}

func (mpm *ModelPromptManager) addGeminiOptimizations(content string) string {
	return fmt.Sprintf(`**Step 1: Task Analysis**
%s

**Step 2: Execution**
I will now perform this task following the specified requirements.

**Step 3: Result**
[The result will be provided below]

---

`, content)
}

// Helper functions
func (mpm *ModelPromptManager) normalizeModelName(modelName string) string {
	// Remove provider prefixes and normalize
	name := strings.ToLower(modelName)

	// Remove common prefixes
	prefixes := []string{"deepinfra:", "ollama:", "openai:", "anthropic:", "google:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			name = strings.TrimPrefix(name, prefix)
			break
		}
	}

	// Replace slashes and special characters
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "-", "_")

	return name
}

func (mpm *ModelPromptManager) isImprovement(newMetrics, existingMetrics *ModelMetrics) bool {
	// A new prompt is better if:
	// 1. Higher success rate (primary factor)
	// 2. Similar success rate but lower cost
	// 3. Similar performance but higher confidence (more test data)

	if newMetrics.SuccessRate > existingMetrics.SuccessRate {
		return true
	}

	if abs(newMetrics.SuccessRate-existingMetrics.SuccessRate) < 0.05 {
		// Similar success rates, check cost efficiency
		if newMetrics.AverageCost < existingMetrics.AverageCost {
			return true
		}

		// Similar cost, check confidence
		if abs(newMetrics.AverageCost-existingMetrics.AverageCost) < 0.001 &&
			newMetrics.ConfidenceLevel > existingMetrics.ConfidenceLevel {
			return true
		}
	}

	return false
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
