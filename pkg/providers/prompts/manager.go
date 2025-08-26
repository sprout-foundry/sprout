package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/alantheprice/ledit/pkg/interfaces"
)

// Manager implements the PromptProvider interface
type Manager struct {
	mu             sync.RWMutex
	promptsDir     string
	templates      map[string]*template.Template
	rawPrompts     map[string]string
	watchCallbacks []func(string)
}

// NewManager creates a new prompt manager
func NewManager(promptsDir string) *Manager {
	if promptsDir == "" {
		home, _ := os.UserHomeDir()
		promptsDir = filepath.Join(home, ".ledit", "prompts")
	}

	return &Manager{
		promptsDir: promptsDir,
		templates:  make(map[string]*template.Template),
		rawPrompts: make(map[string]string),
	}
}

// LoadPrompt loads a prompt by name
func (m *Manager) LoadPrompt(name string) (string, error) {
	m.mu.RLock()
	content, exists := m.rawPrompts[name]
	m.mu.RUnlock()

	if exists {
		return content, nil
	}

	// Try to load from disk
	return m.loadPromptFromDisk(name)
}

// LoadPromptWithVariables loads a prompt with variable substitution
func (m *Manager) LoadPromptWithVariables(name string, variables map[string]string) (string, error) {
	m.mu.RLock()
	tmpl, exists := m.templates[name]
	m.mu.RUnlock()

	if !exists {
		// Load and parse template
		content, err := m.LoadPrompt(name)
		if err != nil {
			return "", err
		}

		tmpl, err = template.New(name).Parse(content)
		if err != nil {
			return "", fmt.Errorf("failed to parse template '%s': %w", name, err)
		}

		m.mu.Lock()
		m.templates[name] = tmpl
		m.mu.Unlock()
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, variables); err != nil {
		return "", fmt.Errorf("failed to execute template '%s': %w", name, err)
	}

	return buf.String(), nil
}

// ListPrompts returns a list of available prompt names
func (m *Manager) ListPrompts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get cached prompts
	names := make([]string, 0, len(m.rawPrompts))
	for name := range m.rawPrompts {
		names = append(names, name)
	}

	// Also scan disk for additional prompts
	diskPrompts := m.scanPromptsFromDisk()
	for _, name := range diskPrompts {
		found := false
		for _, existing := range names {
			if existing == name {
				found = true
				break
			}
		}
		if !found {
			names = append(names, name)
		}
	}

	return names
}

// SavePrompt saves a prompt template
func (m *Manager) SavePrompt(name, content string) error {
	if err := m.ensurePromptsDir(); err != nil {
		return err
	}

	filename := filepath.Join(m.promptsDir, name+".txt")
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to save prompt '%s': %w", name, err)
	}

	// Update cache
	m.mu.Lock()
	m.rawPrompts[name] = content
	delete(m.templates, name) // Invalidate template cache
	m.mu.Unlock()

	// Notify watchers
	m.notifyWatchers(name)

	return nil
}

// DeletePrompt deletes a prompt template
func (m *Manager) DeletePrompt(name string) error {
	filename := filepath.Join(m.promptsDir, name+".txt")

	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete prompt '%s': %w", name, err)
	}

	// Remove from cache
	m.mu.Lock()
	delete(m.rawPrompts, name)
	delete(m.templates, name)
	m.mu.Unlock()

	// Notify watchers
	m.notifyWatchers(name)

	return nil
}

// ValidatePrompt validates a prompt template
func (m *Manager) ValidatePrompt(content string) error {
	_, err := template.New("validation").Parse(content)
	if err != nil {
		return fmt.Errorf("invalid template syntax: %w", err)
	}
	return nil
}

// WatchPrompts watches for changes to prompt files and reloads them
func (m *Manager) WatchPrompts(callback func(name string)) error {
	m.mu.Lock()
	m.watchCallbacks = append(m.watchCallbacks, callback)
	m.mu.Unlock()

	// TODO: Implement file system watcher
	// For now, this is a placeholder that would use fsnotify or similar
	return nil
}

// loadPromptFromDisk loads a prompt from disk
func (m *Manager) loadPromptFromDisk(name string) (string, error) {
	filename := filepath.Join(m.promptsDir, name+".txt")

	content, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("prompt '%s' not found", name)
		}
		return "", fmt.Errorf("failed to read prompt '%s': %w", name, err)
	}

	contentStr := string(content)

	// Cache the content
	m.mu.Lock()
	m.rawPrompts[name] = contentStr
	m.mu.Unlock()

	return contentStr, nil
}

// scanPromptsFromDisk scans the prompts directory for available prompts
func (m *Manager) scanPromptsFromDisk() []string {
	var names []string

	entries, err := os.ReadDir(m.promptsDir)
	if err != nil {
		return names
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if strings.HasSuffix(filename, ".txt") {
			name := strings.TrimSuffix(filename, ".txt")
			names = append(names, name)
		}
	}

	return names
}

// ensurePromptsDir ensures the prompts directory exists
func (m *Manager) ensurePromptsDir() error {
	return os.MkdirAll(m.promptsDir, 0755)
}

// notifyWatchers notifies all registered watchers
func (m *Manager) notifyWatchers(name string) {
	m.mu.RLock()
	callbacks := make([]func(string), len(m.watchCallbacks))
	copy(callbacks, m.watchCallbacks)
	m.mu.RUnlock()

	for _, callback := range callbacks {
		go callback(name) // Call asynchronously
	}
}

// LoadEmbeddedPrompts loads prompts from embedded assets
func (m *Manager) LoadEmbeddedPrompts(embeddedPrompts map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, content := range embeddedPrompts {
		m.rawPrompts[name] = content
	}

	return nil
}

// ClearCache clears the prompt cache
func (m *Manager) ClearCache() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.rawPrompts = make(map[string]string)
	m.templates = make(map[string]*template.Template)
}

// GetPromptInfo returns information about a prompt
func (m *Manager) GetPromptInfo(name string) (*PromptInfo, error) {
	content, err := m.LoadPrompt(name)
	if err != nil {
		return nil, err
	}

	// Try to parse as template to check for variables
	_, parseErr := template.New(name).Parse(content)
	variables := []string{}

	if parseErr == nil {
		// Extract variable names from template
		// This is a simplified approach - a more robust solution would parse the AST
		variables = extractTemplateVariables(content)
	}

	return &PromptInfo{
		Name:       name,
		Content:    content,
		Size:       len(content),
		Variables:  variables,
		IsTemplate: parseErr == nil && len(variables) > 0,
	}, nil
}

// PromptInfo contains information about a prompt
type PromptInfo struct {
	Name       string   `json:"name"`
	Content    string   `json:"content"`
	Size       int      `json:"size"`
	Variables  []string `json:"variables"`
	IsTemplate bool     `json:"is_template"`
}

// extractTemplateVariables extracts variable names from template content
func extractTemplateVariables(content string) []string {
	variables := []string{}

	// Simple regex-based extraction - could be improved
	// Look for {{.Variable}} patterns
	parts := strings.Split(content, "{{")
	for _, part := range parts[1:] { // Skip first part before first {{
		if endIdx := strings.Index(part, "}}"); endIdx >= 0 {
			varExpr := strings.TrimSpace(part[:endIdx])
			if strings.HasPrefix(varExpr, ".") {
				varName := strings.TrimPrefix(varExpr, ".")
				// Remove any function calls or other operations
				if spaceIdx := strings.Index(varName, " "); spaceIdx >= 0 {
					varName = varName[:spaceIdx]
				}
				variables = append(variables, varName)
			}
		}
	}

	return variables
}

// Verify Manager implements PromptProvider interface
var _ interfaces.PromptProvider = (*Manager)(nil)
