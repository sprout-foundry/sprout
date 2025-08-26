package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/internal/domain/agent"
	"github.com/alantheprice/ledit/internal/domain/todo"
	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// WorkspaceAdapter provides a simple workspace adapter
type WorkspaceAdapter struct {
	rootPath string
}

// NewWorkspaceAdapter creates a new workspace adapter
func NewWorkspaceAdapter() *WorkspaceAdapter {
	cwd, _ := os.Getwd()
	return &WorkspaceAdapter{
		rootPath: cwd,
	}
}

// GetContext implements agent.WorkspaceProvider interface
func (a *WorkspaceAdapter) GetContext(ctx context.Context, intent string) (agent.WorkspaceContext, error) {
	files, err := a.scanWorkspaceFiles()
	if err != nil {
		return agent.WorkspaceContext{}, fmt.Errorf("failed to scan workspace: %w", err)
	}

	domainContext := agent.WorkspaceContext{
		RootPath:     a.rootPath,
		ProjectType:  a.detectProjectType(),
		Summary:      fmt.Sprintf("Go project with %d files", len(files)),
		Dependencies: map[string]string{},
	}

	// Convert file info (limit to first 10 files for demo)
	for i, file := range files {
		if i >= 10 {
			break
		}
		domainFile := agent.FileInfo{
			Path:      file,
			Type:      a.getFileType(file),
			Language:  a.getLanguage(file),
			Summary:   fmt.Sprintf("File: %s", filepath.Base(file)),
			Relevance: 0.5,
		}
		domainContext.Files = append(domainContext.Files, domainFile)
	}

	return domainContext, nil
}

// GetRelevantFiles implements agent.WorkspaceProvider interface
func (a *WorkspaceAdapter) GetRelevantFiles(ctx context.Context, intent string, maxFiles int) ([]agent.FileInfo, error) {
	files, err := a.scanWorkspaceFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to scan workspace: %w", err)
	}

	var domainFiles []agent.FileInfo
	count := 0
	for _, file := range files {
		if count >= maxFiles {
			break
		}

		domainFile := agent.FileInfo{
			Path:      file,
			Type:      a.getFileType(file),
			Language:  a.getLanguage(file),
			Summary:   fmt.Sprintf("File: %s", filepath.Base(file)),
			Relevance: a.calculateRelevance(file, intent),
		}
		domainFiles = append(domainFiles, domainFile)
		count++
	}

	return domainFiles, nil
}

// AnalyzeStructure implements agent.WorkspaceProvider interface
func (a *WorkspaceAdapter) AnalyzeStructure(ctx context.Context) (agent.WorkspaceContext, error) {
	return a.GetContext(ctx, "analyze workspace structure")
}

// TodoWorkspaceAdapter adapts workspace functionality for the todo domain
type TodoWorkspaceAdapter struct {
	adapter *WorkspaceAdapter
}

// NewTodoWorkspaceAdapter creates a new todo workspace adapter
func NewTodoWorkspaceAdapter(adapter *WorkspaceAdapter) *TodoWorkspaceAdapter {
	return &TodoWorkspaceAdapter{
		adapter: adapter,
	}
}

// GetWorkspaceContext converts agent workspace context to todo workspace context
func (a *TodoWorkspaceAdapter) GetWorkspaceContext(ctx context.Context, intent string) (todo.WorkspaceContext, error) {
	agentContext, err := a.adapter.GetContext(ctx, intent)
	if err != nil {
		return todo.WorkspaceContext{}, err
	}

	todoContext := todo.WorkspaceContext{
		RootPath:     agentContext.RootPath,
		ProjectType:  agentContext.ProjectType,
		Summary:      agentContext.Summary,
		Dependencies: agentContext.Dependencies,
	}

	// Convert file info
	for _, file := range agentContext.Files {
		todoFile := todo.FileInfo{
			Path:      file.Path,
			Type:      file.Type,
			Language:  file.Language,
			Summary:   file.Summary,
			Relevance: file.Relevance,
		}
		todoContext.Files = append(todoContext.Files, todoFile)
	}

	return todoContext, nil
}

// SimpleWorkspaceProvider implements interfaces.WorkspaceProvider
type SimpleWorkspaceProvider struct {
	rootPath string
}

// NewSimpleWorkspaceProvider creates a new simple workspace provider
func NewSimpleWorkspaceProvider() interfaces.WorkspaceProvider {
	cwd, _ := os.Getwd()
	return &SimpleWorkspaceProvider{
		rootPath: cwd,
	}
}

// AnalyzeWorkspace implements interfaces.WorkspaceProvider
func (s *SimpleWorkspaceProvider) AnalyzeWorkspace(ctx context.Context, path string) (*types.WorkspaceContext, error) {
	return &types.WorkspaceContext{
		Files:     []types.FileInfo{},
		Summary:   "Simple workspace analysis",
		Language:  "go",
		Framework: "none",
		Metadata:  map[string]any{},
	}, nil
}

// GetFileContent implements interfaces.WorkspaceProvider
func (s *SimpleWorkspaceProvider) GetFileContent(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// ListFiles implements interfaces.WorkspaceProvider
func (s *SimpleWorkspaceProvider) ListFiles(patterns []string) ([]types.FileInfo, error) {
	return []types.FileInfo{}, nil
}

// FindFiles implements interfaces.WorkspaceProvider
func (s *SimpleWorkspaceProvider) FindFiles(query string) ([]types.FileInfo, error) {
	return []types.FileInfo{}, nil
}

// GetWorkspaceSummary implements interfaces.WorkspaceProvider
func (s *SimpleWorkspaceProvider) GetWorkspaceSummary() (string, error) {
	return "Simple workspace summary", nil
}

// IsIgnored implements interfaces.WorkspaceProvider
func (s *SimpleWorkspaceProvider) IsIgnored(path string) bool {
	return strings.Contains(path, ".git") || strings.Contains(path, "node_modules")
}

// WatchWorkspace implements interfaces.WorkspaceProvider
func (s *SimpleWorkspaceProvider) WatchWorkspace(callback func(path string)) error {
	// For now, return not implemented - would integrate with file watcher
	return fmt.Errorf("workspace watching not implemented in simple provider")
}

// Helper methods

// scanWorkspaceFiles scans for workspace files
func (a *WorkspaceAdapter) scanWorkspaceFiles() ([]string, error) {
	var files []string
	err := filepath.Walk(a.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip directories and hidden/temp files
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") || strings.HasSuffix(path, "~") {
			return nil
		}

		// Skip large files
		if info.Size() > 1024*1024 { // 1MB
			return nil
		}

		files = append(files, path)
		return nil
	})

	return files, err
}

// detectProjectType detects the project type
func (a *WorkspaceAdapter) detectProjectType() string {
	if _, err := os.Stat(filepath.Join(a.rootPath, "go.mod")); err == nil {
		return "go"
	}
	if _, err := os.Stat(filepath.Join(a.rootPath, "package.json")); err == nil {
		return "nodejs"
	}
	if _, err := os.Stat(filepath.Join(a.rootPath, "requirements.txt")); err == nil {
		return "python"
	}
	return "unknown"
}

// getFileType returns the file type
func (a *WorkspaceAdapter) getFileType(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".go":
		return "source"
	case ".md":
		return "documentation"
	case ".json", ".yaml", ".yml":
		return "config"
	case ".test.go", "_test.go":
		return "test"
	default:
		return "other"
	}
}

// getLanguage returns the programming language
func (a *WorkspaceAdapter) getLanguage(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".go":
		return "go"
	case ".js", ".ts":
		return "javascript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	default:
		return "text"
	}
}

// calculateRelevance calculates file relevance to an intent
func (a *WorkspaceAdapter) calculateRelevance(path, intent string) float64 {
	// Simple relevance: Go files get higher relevance
	if strings.HasSuffix(path, ".go") {
		return 0.8
	}
	if strings.Contains(strings.ToLower(intent), strings.ToLower(filepath.Base(path))) {
		return 0.9
	}
	return 0.3
}
