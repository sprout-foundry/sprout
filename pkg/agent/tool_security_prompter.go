package agent

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// PromptFileAccess implements tools.FileAccessPrompter. Handlers in
// pkg/agent_tools call it when PrecheckFileAccess returns "prompt"; it
// re-enters the shared interactive approval flow (WebUI dialog or CLI
// prompt, session elevation, session folder allowlists, unsafe mode)
// by delegating to handleFileSecurityError with the mode-appropriate
// sentinel error.
func (a *Agent) PromptFileAccess(ctx context.Context, toolName, filePath, resolvedPath, mode string) (context.Context, bool) {
	if a == nil {
		return ctx, false
	}
	var sentinel error
	if mode == "write" {
		sentinel = filesystem.ErrWriteOutsideWorkingDirectory
	} else {
		sentinel = filesystem.ErrOutsideWorkingDirectory
	}
	return handleFileSecurityError(ctx, a, toolName, filePath, resolvedPath, fmt.Errorf("access outside workspace: %w", sentinel))
}
