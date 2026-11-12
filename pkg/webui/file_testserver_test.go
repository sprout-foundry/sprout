//go:build !js

package webui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// newWorkspaceTestServer is the shared server constructor for the file
// handler tests: a real server on a loopback bind with no auth token and no
// agent, rooted at workspaceRoot — the shape the other pkg/webui handler
// tests use. Extracted so each test does not repeat the literal argument list
// (which gosec's G101 otherwise evaluates per-site on new code).
func newWorkspaceTestServer(t *testing.T, workspaceRoot string) *ReactWebServer {
	t.Helper()
	const (
		testBind   = "127.0.0.1"
		testToken  = ""
		testSocket = ""
	)
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, testBind, testSocket, testToken)
	require.NoError(t, err)
	server.workspaceRoot = workspaceRoot
	server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = workspaceRoot
	return server
}
