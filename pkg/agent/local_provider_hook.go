//go:build !js

package agent

import (
	"context"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

// init wires the local LLM hooks. On Apple Silicon, the sprout-local
// provider runs entirely in-process via MLX (no HTTP server, no separate
// process). On other platforms, the provider stub returns clear errors.
func init() {
	providers.LocalServerHook = func(providerID string) error {
		if providerID != string(api.SproutLocalClientType) {
			return nil
		}
		return ensureLocalServer()
	}
	providers.LocalActivityHook = localmodel.TouchActivity
}

// ensureLocalServer loads the local model into GPU memory if it isn't
// already loaded. On MLX platforms this is a direct function call — no
// HTTP server, no process spawning.
func ensureLocalServer() error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return localmodel.EnsureServerForProviderWithCheck(ctx, string(api.SproutLocalClientType))
}

// EnsureLocalServer pre-loads the local model so the first chat request
// doesn't pay the load latency. Called when the user switches to
// sprout-local via /provider.
func (a *Agent) EnsureLocalServer() error {
	return ensureLocalServer()
}
