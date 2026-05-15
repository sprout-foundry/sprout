package agent

import (
	"context"
	"fmt"
	"path/filepath"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
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

	a.debugLog("Generating repo map for directory: %s\n", rootDir)

	result, err := tools.GenerateRepoMap(ctx, rootDir)
	if err != nil {
		return "", fmt.Errorf("generate repo map: %w", err)
	}

	return result, nil
}
