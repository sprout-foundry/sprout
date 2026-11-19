//go:build js

package tools

import (
	"context"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// visionModelServed — WASM variant. The browser build has no provider model
// listing to consult, so every configured model is presumed available; the
// send path still verifies on the wire.
func visionModelServed(_ context.Context, _ api.ClientInterface, _ string) bool {
	return true
}
