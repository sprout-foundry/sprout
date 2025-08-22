package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ToolManager manages the lifecycle of tools and provides high-level operations
type ToolManager struct {
	registry ToolRegistry
	executor ToolExecutor
	logger   *utils.Logger
	config   *config.Config
}

// NewToolManager creates a new tool manager
func NewToolManager(logger *utils.Logger, cfg *config.Config) *ToolManager {
	registry := NewToolRegistry()

	// Create permission checker based on configuration
	permissions := NewSimplePermissionChecker(
		[]string{"read", "write", "execute"}, // Default allowed permissions
		true,                                 // Allow all by default
	)

	executor := NewToolExecutor(registry, permissions, logger, cfg)

	manager := &ToolManager{
		registry: registry,
		executor: executor,
		logger:   logger,
		config:   cfg,
	}

	// Register default tools
	manager.registerDefaultTools()

	return manager
}

// registerDefaultTools registers the built-in tools
func (tm *ToolManager) registerDefaultTools() {
	// Workspace tools
	tm.registry.Register(NewWorkspaceInfoTool())
	tm.registry.Register(NewListFilesTool(10))
	tm.registry.Register(NewGrepSearchTool())

	// Edit tools
	tm.registry.Register(NewMicroEditTool(20, 5, 3)) // maxTotal=20, maxHunk=5, maxHunks=3

	if tm.logger != nil {
		tools := tm.registry.List()
		tm.logger.LogProcessStep(fmt.Sprintf("Registered %d tools", len(tools)))
		for _, tool := range tools {
			tm.logger.LogProcessStep(fmt.Sprintf("  - %s: %s", tool.Name(), tool.Description()))
		}
	}
}

// GetRegistry returns the tool registry
func (tm *ToolManager) GetRegistry() ToolRegistry {
	return tm.registry
}

// GetExecutor returns the tool executor
func (tm *ToolManager) GetExecutor() ToolExecutor {
	return tm.executor
}

// ExecuteWorkspaceInfo executes the workspace info tool
func (tm *ToolManager) ExecuteWorkspaceInfo(ctx context.Context, agentCtx *AgentContext) (*ToolResult, error) {
	params := ToolParameters{
		Context: agentCtx,
		Config:  tm.config,
		Logger:  tm.logger,
		Timeout: 100 * time.Millisecond,
	}

	return tm.executor.ExecuteToolByName(ctx, "workspace_info", params)
}

// ExecuteListFiles executes the list files tool
func (tm *ToolManager) ExecuteListFiles(ctx context.Context, agentCtx *AgentContext, limit int) (*ToolResult, error) {
	params := ToolParameters{
		Args:    []string{fmt.Sprintf("%d", limit)},
		Context: agentCtx,
		Config:  tm.config,
		Logger:  tm.logger,
		Timeout: 50 * time.Millisecond,
	}

	return tm.executor.ExecuteToolByName(ctx, "list_files", params)
}

// ExecuteGrepSearch executes the grep search tool
func (tm *ToolManager) ExecuteGrepSearch(ctx context.Context, agentCtx *AgentContext, terms []string) (*ToolResult, error) {
	params := ToolParameters{
		Args:    terms,
		Context: agentCtx,
		Config:  tm.config,
		Logger:  tm.logger,
		Timeout: 200 * time.Millisecond,
	}

	return tm.executor.ExecuteToolByName(ctx, "grep_search", params)
}

// ExecuteMicroEdit executes the micro edit tool
func (tm *ToolManager) ExecuteMicroEdit(ctx context.Context, agentCtx *AgentContext) (*ToolResult, error) {
	params := ToolParameters{
		Context: agentCtx,
		Config:  tm.config,
		Logger:  tm.logger,
		Timeout: 5 * time.Second,
	}

	return tm.executor.ExecuteToolByName(ctx, "micro_edit", params)
}

// ExecuteTool executes a tool by name with custom parameters
func (tm *ToolManager) ExecuteTool(ctx context.Context, name string, params ToolParameters) (*ToolResult, error) {
	return tm.executor.ExecuteToolByName(ctx, name, params)
}

// CanExecuteTool checks if a tool can be executed
func (tm *ToolManager) CanExecuteTool(ctx context.Context, name string, params ToolParameters) bool {
	tool, exists := tm.registry.Get(name)
	if !exists {
		return false
	}

	return tm.executor.CanExecute(ctx, tool, params)
}

// GetAvailableTools returns all available tools
func (tm *ToolManager) GetAvailableTools() []Tool {
	return tm.registry.List()
}

// GetToolsByCategory returns tools filtered by category
func (tm *ToolManager) GetToolsByCategory(category string) []Tool {
	return tm.registry.ListByCategory(category)
}

// GetToolCategories returns all available tool categories
func (tm *ToolManager) GetToolCategories() []string {
	registry := tm.registry.(*DefaultToolRegistry)
	return registry.GetToolCategories()
}

// IsToolAvailable checks if a tool is available
func (tm *ToolManager) IsToolAvailable(name string) bool {
	tool, exists := tm.registry.Get(name)
	if !exists {
		return false
	}
	return tool.IsAvailable()
}

// GetToolInfo returns information about a tool
func (tm *ToolManager) GetToolInfo(name string) (ToolInfo, bool) {
	tool, exists := tm.registry.Get(name)
	if !exists {
		return ToolInfo{}, false
	}

	return ToolInfo{
		Name:                tool.Name(),
		Description:         tool.Description(),
		Category:            tool.Category(),
		RequiredPermissions: tool.RequiredPermissions(),
		EstimatedDuration:   tool.EstimatedDuration(),
		IsAvailable:         tool.IsAvailable(),
	}, true
}

// ToolInfo contains information about a tool
type ToolInfo struct {
	Name                string        `json:"name"`
	Description         string        `json:"description"`
	Category            string        `json:"category"`
	RequiredPermissions []string      `json:"required_permissions"`
	EstimatedDuration   time.Duration `json:"estimated_duration"`
	IsAvailable         bool          `json:"is_available"`
}

// GetToolStats returns statistics about the tool system
func (tm *ToolManager) GetToolStats() ToolStats {
	tools := tm.registry.List()
	categories := make(map[string]int)
	available := 0

	for _, tool := range tools {
		categories[tool.Category()]++
		if tool.IsAvailable() {
			available++
		}
	}

	return ToolStats{
		TotalTools:     len(tools),
		AvailableTools: available,
		Categories:     categories,
	}
}

// ToolStats contains statistics about the tool system
type ToolStats struct {
	TotalTools     int            `json:"total_tools"`
	AvailableTools int            `json:"available_tools"`
	Categories     map[string]int `json:"categories"`
}

// RegisterCustomTool registers a custom tool
func (tm *ToolManager) RegisterCustomTool(tool Tool) error {
	return tm.registry.Register(tool)
}

// UnregisterTool unregisters a tool
func (tm *ToolManager) UnregisterTool(name string) error {
	return tm.registry.Unregister(name)
}
