package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// writeTools maps tool names that are file-write operations and should trigger
// embedding duplicate detection after successful execution.
var writeTools = map[string]bool{
	"write_file":            true,
	"edit_file":             true,
	"write_structured_file": true,
	"patch_structured_file": true,
}

// shouldCheckDuplicates determines whether the duplicate check should run
// for the given tool and agent. It requires:
//   - the tool is a file-write tool (write_file, edit_file, write_structured_file, patch_structured_file)
//   - the agent has embedding_index enabled in its config
//   - the agent has an EmbeddingManager initialized
func shouldCheckDuplicates(toolName string, agent *Agent) bool {
	if !writeTools[toolName] {
		return false
	}
	if agent == nil {
		return false
	}
	cfg := agent.GetConfig()
	if cfg == nil || cfg.EmbeddingIndex == nil || !cfg.EmbeddingIndex.Enabled {
		return false
	}
	if agent.GetEmbeddingManager() == nil {
		return false
	}
	return true
}

// runDuplicateCheck executes an embedding-based duplicate check on the file
// at filePath after it has been written. It reads the file from disk and
// checks against the index. Returns a warning string if duplicates are found,
// or empty string if not (or if the check fails).
func runDuplicateCheck(ctx context.Context, agent *Agent, filePath string) string {
	// Guard against nil agent.
	if agent == nil {
		return ""
	}

	// Validate path is within workspace before reading (MUST_FIX #2: path traversal).
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return ""
	}

	workspaceRoot := agent.GetWorkspaceRoot()
	if workspaceRoot == "" {
		workspaceRoot, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(absPath, absRoot+string(os.PathSeparator)) && absPath != absRoot {
		return ""
	}

	em := agent.GetEmbeddingManager()
	if em == nil {
		return ""
	}
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		// Silently skip — file read failure shouldn't block the write result
		return ""
	}
	content := string(contentBytes)
	result, err := em.CheckDuplicates(ctx, filePath, content)
	if err != nil {
		// Silently skip — embedding init/check failure shouldn't block the write result
		if agent.debug {
			agent.debugLog("[EMBEDDING] duplicate check failed for %s: %v\n", filePath, err)
		}
		return ""
	}
	if result != nil && result.WarningText != "" {
		return result.WarningText
	}
	return ""
}

// reindexFileAfterWrite refreshes the embedding index entry for filePath in
// the background. Without this, the index grows stale during a session and
// semantic_search returns hits against an old file body. The duplicate-check
// hook is the natural twin: it already runs after every successful write,
// and gating + path-resolution logic is shared.
//
// Runs in its own goroutine with a bounded context so a slow embed never
// stalls the agent's response. Errors are debug-only — staleness is annoying
// but never a hard failure for the user.
func reindexFileAfterWrite(agent *Agent, filePath string) {
	if agent == nil {
		return
	}
	em := agent.GetEmbeddingManager()
	if em == nil {
		return
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return
	}
	workspaceRoot := agent.GetWorkspaceRoot()
	if workspaceRoot == "" {
		if cwd, err := os.Getwd(); err == nil {
			workspaceRoot = cwd
		} else {
			return
		}
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return
	}
	if !strings.HasPrefix(absPath, absRoot+string(os.PathSeparator)) && absPath != absRoot {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := em.UpdateFile(ctx, absPath); err != nil && agent.debug {
			agent.debugLog("[EMBEDDING] reindex failed for %s: %v\n", absPath, err)
		}
	}()
}
