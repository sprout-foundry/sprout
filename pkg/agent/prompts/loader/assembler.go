package loader

import (
	"fmt"
	"path/filepath"
)

// PromptAssembler coordinates the classification and assembly of prompts
type PromptAssembler struct {
	classifier   *RequestClassifier
	moduleLoader *ModuleLoader
	basePath     string
}

// NewPromptAssembler creates a new prompt assembler
func NewPromptAssembler(promptsBasePath string) *PromptAssembler {
	return &PromptAssembler{
		classifier:   NewRequestClassifier(),
		moduleLoader: NewModuleLoader(promptsBasePath),
		basePath:     promptsBasePath,
	}
}

// AssemblePromptForRequest takes a user request and returns an optimized prompt
func (pa *PromptAssembler) AssemblePromptForRequest(request string) (string, RequestClassification, error) {
	// Classify the request
	classification := pa.classifier.ClassifyRequest(request)
	
	// Validate that we have the required modules
	if err := pa.moduleLoader.ValidateModules(); err != nil {
		return "", classification, fmt.Errorf("module validation failed: %w", err)
	}
	
	// Assemble the prompt
	prompt, err := pa.moduleLoader.AssemblePrompt(classification)
	if err != nil {
		return "", classification, fmt.Errorf("prompt assembly failed: %w", err)
	}
	
	return prompt, classification, nil
}

// GetPromptInfo returns information about available modules and current setup
func (pa *PromptAssembler) GetPromptInfo() (map[string]interface{}, error) {
	availableModules, err := pa.moduleLoader.GetAvailableModules()
	if err != nil {
		return nil, err
	}
	
	info := map[string]interface{}{
		"base_path":         pa.basePath,
		"available_modules": availableModules,
		"classifier_info": map[string]string{
			"primary_types": "exploratory, implementation, debugging, testing",
			"complexity":    "simple, medium, complex",
		},
	}
	
	return info, nil
}

// TestClassification tests the classifier with example requests
func (pa *PromptAssembler) TestClassification(testRequests []string) []RequestClassification {
	results := []RequestClassification{}
	
	for _, request := range testRequests {
		classification := pa.classifier.ClassifyRequest(request)
		results = append(results, classification)
	}
	
	return results
}

// DefaultAssembler creates a default assembler with the standard prompts path
func DefaultAssembler() (*PromptAssembler, error) {
	// Assume prompts are in pkg/agent/prompts relative to current working directory
	promptsPath, err := filepath.Abs("pkg/agent/prompts")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve prompts path: %w", err)
	}
	
	assembler := NewPromptAssembler(promptsPath)
	
	// Validate the setup
	if err := assembler.moduleLoader.ValidateModules(); err != nil {
		return nil, fmt.Errorf("failed to validate modular prompt setup: %w", err)
	}
	
	return assembler, nil
}