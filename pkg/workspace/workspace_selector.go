package workspace

import (
	"fmt"
	"os" // New import
	"path/filepath" // New import
	"strings"

	"github.com/alantheprice/ledit/pkg/utils" // New import for logger
)



// normalizeLLMPath attempts to correct an LLM-returned file path to match a key in workspace.Files.
// It returns the corrected path and a boolean indicating if a correction was made.
func normalizeLLMPath(llmPath string, cwd string, gitRoot string, workspaceFiles map[string]WorkspaceFileInfo, logger *utils.Logger) (string, bool) {
	// 1. Direct Match: Check if llmPath exists directly as a key in workspaceFiles.
	if _, ok := workspaceFiles[llmPath]; ok {
		return llmPath, false // No correction needed
	}

	// 2. Try removing CWD name prefix (e.g., "tools/file.html" -> "file.html" if CWD is "tools")
	cwdBase := filepath.Base(cwd)
	if cwdBase != "." && strings.HasPrefix(llmPath, cwdBase+string(os.PathSeparator)) {
		strippedPath := strings.TrimPrefix(llmPath, cwdBase+string(os.PathSeparator))
		if _, ok := workspaceFiles[strippedPath]; ok {
			logger.LogUserInteraction(fmt.Sprintf("Corrected LLM path '%s' to '%s' (removed CWD name prefix).", llmPath, strippedPath))
			return strippedPath, true
		}
	}

	// 3. Try removing Git Root name prefix (e.g., "ledit/cmd/code.go" -> "cmd/code.go" if Git Root is "ledit")
	if gitRoot != "" {
		gitRootBase := filepath.Base(gitRoot)
		if gitRootBase != "." && strings.HasPrefix(llmPath, gitRootBase+string(os.PathSeparator)) {
			strippedPath := strings.TrimPrefix(llmPath, gitRootBase+string(os.PathSeparator))
			// Now, this strippedPath is relative to the Git root. Convert it to CWD relative.
			absPathFromStripped := filepath.Join(gitRoot, strippedPath)
			relPathToCWD, err := filepath.Rel(cwd, absPathFromStripped)
			if err == nil {
				if _, ok := workspaceFiles[relPathToCWD]; ok {
					logger.LogUserInteraction(fmt.Sprintf("Corrected LLM path '%s' to '%s' (removed Git root name prefix and converted to CWD relative).", llmPath, relPathToCWD))
					return relPathToCWD, true
				}
			}
		}
	}

	// 4. Try resolving relative to Git Root, then converting to CWD relative
	if gitRoot != "" {
		absPathFromGitRoot := filepath.Join(gitRoot, llmPath)
		relPathToCWD, err := filepath.Rel(cwd, absPathFromGitRoot)
		if err == nil {
			if _, ok := workspaceFiles[relPathToCWD]; ok {
				logger.LogUserInteraction(fmt.Sprintf("Corrected LLM path '%s' to '%s' (resolved relative to Git root then CWD).", llmPath, relPathToCWD))
				return relPathToCWD, true
			}
		}
	}

	// 5. Try resolving as absolute path, then converting to CWD relative
	if filepath.IsAbs(llmPath) {
		relPathToCWD, err := filepath.Rel(cwd, llmPath)
		if err == nil {
			if _, ok := workspaceFiles[relPathToCWD]; ok {
				logger.LogUserInteraction(fmt.Sprintf("Corrected LLM path '%s' to '%s' (resolved as absolute path then CWD relative).", llmPath, relPathToCWD))
				return relPathToCWD, true
			}
		}
	}

	// No correction found, return original path. Caller should check if it exists in workspaceFiles.
	return llmPath, false
}



