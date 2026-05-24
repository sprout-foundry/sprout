//go:build js && wasm

package main

import (
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// injectWasmStreamingClient wraps the provider's HTTP clients with WASM streaming support.
// For GenericProvider instances, it replaces both HTTP and streaming clients with the
// WASM Fetch API-based streaming transport. Other client types (e.g., TestClient) are
// left unchanged.
func injectWasmStreamingClient(client api.ClientInterface) {
	gp, ok := client.(*providers.GenericProvider)
	if !ok {
		return
	}
	wasmClient := NewWasmStreamingHTTPClient()
	gp.SetHTTPClient(wasmClient)
	gp.SetStreamingClient(wasmClient)
}
