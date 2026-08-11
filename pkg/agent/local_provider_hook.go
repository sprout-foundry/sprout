//go:build !js

package agent

import (
	"context"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

// init wires the local LLM server lifecycle hook so that when a chat
// request to the sprout-local provider fails with a connection error,
// the agent automatically starts the local server and retries.
//
// This is the integration point between the generic provider layer
// (which has no build tags) and the localmodel package (which requires
// a real OS to spawn processes). The hook is nil on WASM builds.
func init() {
	providers.LocalServerHook = func(providerID string) error {
		if providerID != string(api.SproutLocalClientType) {
			return nil
		}
		return ensureLocalServer()
	}
	providers.LocalActivityHook = localmodel.TouchActivity
}

// ensureLocalServer starts the local LLM server if it's not already running.
// Used both by the provider hook (on connection error) and by
// EnsureLocalServer (proactive start during provider switch).
func ensureLocalServer() error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return localmodel.EnsureServerForProviderWithCheck(ctx, string(api.SproutLocalClientType))
}

// EnsureLocalServer starts the local LLM server for the sprout-local
// provider. Called proactively when the user switches to sprout-local
// so the first chat request doesn't hit a connection-refused error.
// Returns nil if the server is already running.
func (a *Agent) EnsureLocalServer() error {
	return ensureLocalServer()
}
