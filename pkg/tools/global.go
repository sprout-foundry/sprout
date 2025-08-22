package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

var (
	// globalExecutor is the global tool executor instance
	globalExecutor *Executor
	globalMutex    sync.RWMutex
)

// InitializeGlobalExecutor initializes the global tool executor
func InitializeGlobalExecutor(config *config.Config, logger *utils.Logger) {
	globalMutex.Lock()
	defer globalMutex.Unlock()

	registry := NewDefaultRegistry()

	// Create permission checker - for now, allow all operations
	// TODO: Make this configurable based on security settings
	permissions := NewSimplePermissionChecker([]string{
		PermissionReadFile,
		PermissionWriteFile,
		PermissionExecuteShell,
		PermissionNetworkAccess,
		PermissionUserPrompt,
		PermissionWorkspaceRead,
		PermissionWorkspaceWrite,
	})

	globalExecutor = NewExecutor(registry, permissions, logger, config)
}

// GetGlobalExecutor returns the global tool executor
func GetGlobalExecutor() *Executor {
	globalMutex.RLock()
	defer globalMutex.RUnlock()

	return globalExecutor
}

// ExecuteToolCall is a convenience function that uses the global executor
func ExecuteToolCall(ctx context.Context, toolCall types.ToolCall) (*Result, error) {
	executor := GetGlobalExecutor()
	if executor == nil {
		// Fallback: create a minimal executor
		config := &config.Config{SkipPrompt: true}
		logger := utils.GetLogger(true)
		InitializeGlobalExecutor(config, logger)
		executor = GetGlobalExecutor()
	}

	return executor.ExecuteToolCall(ctx, toolCall)
}

// ExecuteToolByName is a convenience function that uses the global executor
func ExecuteToolByName(ctx context.Context, toolName string, params Parameters) (*Result, error) {
	executor := GetGlobalExecutor()
	if executor == nil {
		// Fallback: create a minimal executor
		config := &config.Config{SkipPrompt: true}
		logger := utils.GetLogger(true)
		InitializeGlobalExecutor(config, logger)
		executor = GetGlobalExecutor()
	}

	return executor.ExecuteToolByName(ctx, toolName, params)
}

// RegisterTool registers a tool with the global registry
func RegisterTool(tool Tool) error {
	executor := GetGlobalExecutor()
	if executor == nil {
		return fmt.Errorf("global executor not initialized")
	}

	return executor.registry.RegisterTool(tool)
}

// GetTool retrieves a tool from the global registry
func GetTool(name string) (Tool, bool) {
	executor := GetGlobalExecutor()
	if executor == nil {
		return nil, false
	}

	return executor.registry.GetTool(name)
}

// ListTools returns all tools from the global registry
func ListTools() []Tool {
	executor := GetGlobalExecutor()
	if executor == nil {
		return []Tool{}
	}

	return executor.registry.ListTools()
}
