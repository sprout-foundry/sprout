package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

func handleRepoMap(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	rootDir := "."
	if v, ok := args["directory"].(string); ok && v != "" {
		rootDir = v
	}

	// Resolve relative paths against the agent's workspace root.
	if !filepath.IsAbs(rootDir) {
		if wd := filesystem.WorkspaceRootFromContext(ctx); wd != "" {
			rootDir = filepath.Join(wd, rootDir)
		}
	}

	// Resolve to absolute path.
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", agenterrors.NewTool("repo_map", "resolve directory", err)
	}

	// Verify that the resolved directory is within the workspace root.
	workspaceRoot := filesystem.WorkspaceRootFromContext(ctx)
	if workspaceRoot == "" {
		workspaceRoot, err = os.Getwd()
		if err != nil {
			return "", agenterrors.NewTool("repo_map", "get working directory", err)
		}
	}
	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", agenterrors.NewTool("repo_map", "resolve workspace root", err)
	}
	// Allow exact match or a proper subdirectory (with separator).
	if absRoot != absWorkspace && !strings.HasPrefix(absRoot, absWorkspace+string(filepath.Separator)) {
		return "", fmt.Errorf("directory %q is outside workspace root", rootDir)
	}

	a.Logger().Debug("Generating repo map for directory: %s\n", rootDir)

	result, err := tools.GenerateRepoMap(ctx, rootDir)
	if err != nil {
		return "", agenterrors.NewTool("repo_map", "generate repo map", err)
	}

	return result, nil
}
