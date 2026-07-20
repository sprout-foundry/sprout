//go:build windows && !js

package automate

import "github.com/sprout-foundry/sprout/pkg/utils/pidalive"

// IsProcessAlive reports whether the given PID currently names a running
// process. Thin wrapper around pidalive.IsAlive; preserved as the canonical
// entry point so existing callers (cmd/automate_*.go, pkg/webui/automations_api.go,
// etc.) don't need to change their import. SP-112-3.
func IsProcessAlive(pid int) bool {
	return pidalive.IsAlive(pid)
}
