package providers

import (
	"strings"
)

// LocalServerHook is called when a local provider (sprout-local) gets a
// connection error on its first request. If set, the provider attempts to
// start the local LLM server and retries the request once. This decouples
// the agent_providers package (no build tags, compiles for WASM) from the
// localmodel package (which requires !js and platform-specific code).
//
// The hook should:
//   - Check if the server is already running (fast path)
//   - Find the appropriate model for the machine
//   - Spawn the server process if needed
//   - Wait for health
//   - Return nil on success, error if the server can't be started
//
// Set this to nil (the default) to disable auto-start behavior entirely.
var LocalServerHook func(providerID string) error

// isLocalProvider returns true if the provider config points at the local
// LLM server (sprout-local or any provider whose endpoint is localhost:18081).
func (p *GenericProvider) isLocalProvider() bool {
	if p.config == nil {
		return false
	}
	if p.config.Name == "sprout-local" {
		return true
	}
	endpoint := strings.ToLower(p.config.Endpoint)
	return strings.Contains(endpoint, "127.0.0.1:18081") || strings.Contains(endpoint, "localhost:18081")
}

// tryLocalServerRecovery checks whether this is a local provider that needs
// the server started, attempts recovery, and returns true if the caller
// should retry the request. Returns false (no retry) for non-local providers
// or when the hook isn't set. Also touches the activity timer on success,
// resetting the idle-shutdown clock.
func (p *GenericProvider) tryLocalServerRecovery() bool {
	if !p.isLocalProvider() {
		return false
	}
	if LocalServerHook == nil {
		return false
	}
	if err := LocalServerHook(p.config.Name); err != nil {
		return false
	}
	if LocalActivityHook != nil {
		LocalActivityHook()
	}
	return true
}

// LocalActivityHook is called after every successful local server
// interaction to reset the idle timer. Set by the agent package.
var LocalActivityHook func()
