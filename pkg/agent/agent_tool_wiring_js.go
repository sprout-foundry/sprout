//go:build js

package agent

import (
	"context"
	"fmt"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// wireHostOnlyToolFuncs is the WASM (browser) implementation. PR creation
// and automate workflows require host-only infrastructure — git
// operations, filesystem access, and subprocess spawning — none of which
// are available in the browser environment. Even when isProduction is
// true, we wire clear-error stubs rather than the real handlers so that,
// if these tools are ever invoked in WASM mode, the model receives an
// actionable message instead of a nil-pointer "agent integration not
// initialized" error or a cryptic syscall failure. See AUDIT-C2.
func wireHostOnlyToolFuncs(_ *Agent, _ bool) {
	tools.RunAutomateFunc = func(_ context.Context, _ map[string]any) (string, error) {
		return "", fmt.Errorf("run_automate is not available in browser mode")
	}
	tools.CreatePullRequestFunc = func(_ context.Context, _ map[string]any) (string, error) {
		return "", fmt.Errorf("create_pull_request is not available in browser mode")
	}
}
