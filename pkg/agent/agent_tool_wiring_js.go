//go:build js

package agent

import (
	"context"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
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
		return "", agenterrors.NewConfig("run_automate is not available in browser mode", nil)
	}
	tools.CreatePullRequestFunc = func(_ context.Context, _ map[string]any) (string, error) {
		return "", agenterrors.NewConfig("create_pull_request is not available in browser mode", nil)
	}
}
