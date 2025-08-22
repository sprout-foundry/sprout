package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// WorkspaceInfoTool provides workspace information
type WorkspaceInfoTool struct {
	*BaseTool
}

// NewWorkspaceInfoTool creates a new workspace info tool
func NewWorkspaceInfoTool() *WorkspaceInfoTool {
	base := NewBaseTool(
		"workspace_info",
		"Gathers and logs lightweight workspace information",
		"workspace",
		[]string{"read"},
		100*time.Millisecond,
	)

	return &WorkspaceInfoTool{BaseTool: base}
}

// Execute runs the workspace info tool
func (t *WorkspaceInfoTool) Execute(ctx context.Context, params ToolParameters) (*ToolResult, error) {
	if params.Context == nil {
		return &ToolResult{
			Success: false,
			Errors:  []string{"agent context is required"},
		}, nil
	}

	startTime := time.Now()

	info, err := buildWorkspaceStructure(params.Logger)
	if err != nil {
		return &ToolResult{
			Success:       false,
			Errors:        []string{fmt.Sprintf("failed to build workspace structure: %v", err)},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	summary := fmt.Sprintf("Workspace: type=%s files=%d dirs=%d", info.ProjectType, len(info.AllFiles), len(info.FilesByDir))

	// Update agent context
	params.Context.ExecutedOperations = append(params.Context.ExecutedOperations, summary)
	params.Logger.LogProcessStep(summary)

	return &ToolResult{
		Success: true,
		Output:  summary,
		Data: map[string]interface{}{
			"project_type": info.ProjectType,
			"file_count":   len(info.AllFiles),
			"dir_count":    len(info.FilesByDir),
		},
		Files:         info.AllFiles,
		ExecutionTime: time.Since(startTime),
	}, nil
}

// CanExecute checks if the tool can execute
func (t *WorkspaceInfoTool) CanExecute(ctx context.Context, params ToolParameters) bool {
	return params.Context != nil && params.Logger != nil
}

// ListFilesTool lists workspace files
type ListFilesTool struct {
	*BaseTool
	limit int
}

// NewListFilesTool creates a new list files tool
func NewListFilesTool(limit int) *ListFilesTool {
	base := NewBaseTool(
		"list_files",
		fmt.Sprintf("Lists a maximum of %d workspace files for quick orientation", limit),
		"workspace",
		[]string{"read"},
		50*time.Millisecond,
	)

	return &ListFilesTool{
		BaseTool: base,
		limit:    limit,
	}
}

// Execute runs the list files tool
func (t *ListFilesTool) Execute(ctx context.Context, params ToolParameters) (*ToolResult, error) {
	if params.Context == nil {
		return &ToolResult{
			Success: false,
			Errors:  []string{"agent context is required"},
		}, nil
	}

	startTime := time.Now()

	info, err := buildWorkspaceStructure(params.Logger)
	if err != nil {
		return &ToolResult{
			Success:       false,
			Errors:        []string{fmt.Sprintf("failed to build workspace structure: %v", err)},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	limit := t.limit
	if len(params.Args) > 0 {
		// Allow overriding limit via args
		if customLimit := parseIntArg(params.Args[0]); customLimit > 0 {
			limit = customLimit
		}
	}

	files := info.AllFiles
	if len(files) > limit {
		files = files[:limit]
	}

	summary := fmt.Sprintf("Files (%d): %s", len(files), strings.Join(files, ", "))

	// Update agent context
	params.Context.ExecutedOperations = append(params.Context.ExecutedOperations, summary)
	params.Logger.LogProcessStep(summary)

	return &ToolResult{
		Success: true,
		Output:  summary,
		Data: map[string]interface{}{
			"files": files,
			"total": len(info.AllFiles),
			"shown": len(files),
		},
		Files:         files,
		ExecutionTime: time.Since(startTime),
	}, nil
}

// CanExecute checks if the tool can execute
func (t *ListFilesTool) CanExecute(ctx context.Context, params ToolParameters) bool {
	return params.Context != nil && params.Logger != nil
}

// parseIntArg parses a string argument as int, returns 0 on error
func parseIntArg(arg string) int {
	if limit, err := strconv.Atoi(arg); err == nil && limit > 0 {
		return limit
	}
	return 0
}

// GrepSearchTool performs content search
type GrepSearchTool struct {
	*BaseTool
}

// NewGrepSearchTool creates a new grep search tool
func NewGrepSearchTool() *GrepSearchTool {
	base := NewBaseTool(
		"grep_search",
		"Performs quick content search for provided terms",
		"search",
		[]string{"read"},
		200*time.Millisecond,
	)

	return &GrepSearchTool{BaseTool: base}
}

// Execute runs the grep search tool
func (t *GrepSearchTool) Execute(ctx context.Context, params ToolParameters) (*ToolResult, error) {
	if params.Context == nil {
		return &ToolResult{
			Success: false,
			Errors:  []string{"agent context is required"},
		}, nil
	}

	startTime := time.Now()

	var terms []string

	// Extract terms from args
	if len(params.Args) > 0 {
		terms = params.Args
	}

	// Extract terms from kwargs if available
	if termInterface, exists := params.Kwargs["terms"]; exists {
		if termSlice, ok := termInterface.([]string); ok {
			terms = append(terms, termSlice...)
		}
	}

	if len(terms) == 0 {
		return &ToolResult{
			Success:       false,
			Errors:        []string{"no search terms provided"},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	joined := strings.Join(terms, " ")
	found := findFilesUsingShellCommands(joined, &WorkspaceInfo{ProjectType: "other"}, params.Logger)

	var summary string
	if len(found) == 0 {
		summary = "grep_search: no matches found"
		params.Logger.LogProcessStep(summary)
	} else {
		summary = fmt.Sprintf("grep_search: %d files: %s", len(found), strings.Join(found, ", "))
		params.Context.ExecutedOperations = append(params.Context.ExecutedOperations, summary)
		params.Logger.LogProcessStep(summary)
	}

	return &ToolResult{
		Success: true,
		Output:  summary,
		Data: map[string]interface{}{
			"terms":       terms,
			"found":       found,
			"match_count": len(found),
		},
		Files:         found,
		ExecutionTime: time.Since(startTime),
	}, nil
}

// CanExecute checks if the tool can execute
func (t *GrepSearchTool) CanExecute(ctx context.Context, params ToolParameters) bool {
	return params.Context != nil && params.Logger != nil
}
