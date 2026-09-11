//go:build !js

package webui

import (
	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/updatecheck"
)

// updateCheckSkipped reports whether the daemon should skip the passive
// release refresh. A nil agent or nil config fails closed: no config, no
// network call.
func (ws *ReactWebServer) updateCheckSkipped() bool {
	disabled := true
	if ws.agent != nil {
		if cfg := ws.agent.GetConfig(); cfg != nil {
			disabled = cfg.DisableUpdateCheck
		}
	}
	return updatecheck.Skipped(buildinfo.Version, disabled)
}
