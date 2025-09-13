package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModuleLoader handles loading and assembling prompt modules
type ModuleLoader struct {
	basePath string
}

// NewModuleLoader creates a new module loader with the given base path
func NewModuleLoader(basePath string) *ModuleLoader {
	return &ModuleLoader{
		basePath: basePath,
	}
}

// LoadModule loads a specific module file and returns its content
func (ml *ModuleLoader) LoadModule(modulePath string) (string, error) {
	fullPath := filepath.Join(ml.basePath, modulePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to load module %s: %w", modulePath, err)
	}
	return string(content), nil
}

// AssemblePrompt creates a complete prompt based on the classification
func (ml *ModuleLoader) AssemblePrompt(classification RequestClassification) (string, error) {
	sections := []string{}
	
	// Always include base prompt
	baseContent, err := ml.LoadModule("base.md")
	if err != nil {
		return "", fmt.Errorf("failed to load base module: %w", err)
	}
	sections = append(sections, baseContent)
	
	// Include primary type module
	primaryModulePath := fmt.Sprintf("modules/%s.md", classification.PrimaryType)
	primaryContent, err := ml.LoadModule(primaryModulePath)
	if err != nil {
		// If specific module doesn't exist, use a default approach
		if classification.PrimaryType == "testing" {
			// Use debugging module for testing requests
			primaryContent, err = ml.LoadModule("modules/debugging.md")
			if err != nil {
				return "", fmt.Errorf("failed to load fallback debugging module: %w", err)
			}
		} else {
			return "", fmt.Errorf("failed to load primary module %s: %w", primaryModulePath, err)
		}
	}
	sections = append(sections, primaryContent)
	
	// Include context modules
	for _, contextModule := range classification.ContextNeeds {
		contextPath := fmt.Sprintf("context/%s.md", contextModule)
		contextContent, err := ml.LoadModule(contextPath)
		if err != nil {
			// Log warning but continue - context modules are optional
			fmt.Printf("Warning: failed to load context module %s: %v\n", contextPath, err)
			continue
		}
		sections = append(sections, contextContent)
	}
	
	// Include capability modules if needed
	for _, tool := range classification.RequiredTools {
		capabilityPath := fmt.Sprintf("capabilities/%s.md", tool)
		capabilityContent, err := ml.LoadModule(capabilityPath)
		if err != nil {
			// Capabilities are optional - continue if not found
			continue
		}
		sections = append(sections, capabilityContent)
	}
	
	// Join all sections with separators
	prompt := strings.Join(sections, "\n\n---\n\n")
	
	// Add metadata comment at the end
	metadata := fmt.Sprintf("\n\n<!-- Prompt assembled for %s request (complexity: %s, confidence: %.2f) -->", 
		classification.PrimaryType, classification.Complexity, classification.Confidence)
	prompt += metadata
	
	return prompt, nil
}

// GetAvailableModules returns lists of available modules by type
func (ml *ModuleLoader) GetAvailableModules() (map[string][]string, error) {
	modules := map[string][]string{
		"modules":      {},
		"context":      {},
		"capabilities": {},
	}
	
	for moduleType := range modules {
		dirPath := filepath.Join(ml.basePath, moduleType)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			continue // Skip if directory doesn't exist
		}
		
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				// Remove .md extension
				moduleName := strings.TrimSuffix(entry.Name(), ".md")
				modules[moduleType] = append(modules[moduleType], moduleName)
			}
		}
	}
	
	return modules, nil
}

// ValidateModules checks if all expected modules exist
func (ml *ModuleLoader) ValidateModules() error {
	requiredModules := []string{
		"base.md",
		"modules/exploratory.md",
		"modules/implementation.md", 
		"modules/debugging.md",
		"context/batch_operations.md",
		"context/circuit_breakers.md",
		"context/todo_workflow.md",
	}
	
	for _, module := range requiredModules {
		_, err := ml.LoadModule(module)
		if err != nil {
			return fmt.Errorf("required module missing: %s", module)
		}
	}
	
	return nil
}