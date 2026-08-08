//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTestServer creates, starts, and returns a ReactWebServer listening on a
// random OS-assigned port. It returns a cleanup function that cancels the
// context and shuts down the server.
func startTestServer(t *testing.T, ag *agent.Agent) (*ReactWebServer, func()) {
	t.Helper()

	port, err := FindAvailablePort(DaemonPort + 1000)
	require.NoError(t, err, "FindAvailablePort failed")

	srv, err := NewReactWebServer(ag, events.NewEventBus(), port, "127.0.0.1", "", "")
	require.NoError(t, err, "NewReactWebServer failed")

	ctx, cancel := context.WithCancel(context.Background())

	err = srv.Start(ctx)
	require.NoError(t, err, "server Start failed")

	// Wait for the server to report running.
	for i := 0; i < 50 && !srv.IsRunning(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	require.True(t, srv.IsRunning(), "server did not become ready in time")

	cleanup := func() {
		cancel()
		_ = srv.Shutdown()
	}

	return srv, cleanup
}

// TestHealthEndpoint_ReturnsOK validates the response shape and field types of
// the /health endpoint.
func TestHealthEndpoint_ReturnsOK(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv, cleanup := startTestServer(t, nil)
	defer cleanup()

	// In the constructor port 0 becomes DaemonPort; FindAvailablePort gives a
	// real ephemeral port. After Start() the actual port may have been updated
	// from the listener, so use GetPort() for the URL.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", srv.GetPort()))
	require.NoError(t, err, "GET /health failed")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "expected 200 OK")

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body), "failed to decode JSON")

	// status
	assert.Equal(t, "ok", body["status"], "status should be \"ok\"")

	// port matches the actual assigned port
	expectedPort := float64(srv.GetPort()) // JSON numbers decode as float64
	assert.Equal(t, expectedPort, body["port"], "port should match server.GetPort()")

	// uptime is a non-empty string
	uptime, ok := body["uptime"].(string)
	require.True(t, ok, "uptime should be a string")
	assert.NotEmpty(t, uptime, "uptime should not be empty")

	// agent_available — daemon mode (nil agent) defaults to false (serviceMode
	// is false by default)
	require.Contains(t, body, "agent_available", "agent_available should be present")

	// active_queries — should be present and zero at start
	require.Contains(t, body, "active_queries", "active_queries should be present")
	queries, ok := body["active_queries"].(float64) // JSON numbers decode as float64
	require.True(t, ok, "active_queries should be a number")
	assert.Equal(t, float64(0), queries, "active_queries should be 0 at start")
}

// TestHealthEndpoint_DaemonMode_AgentAvailableFalse verifies that with a nil
// agent (daemon mode) the agent_available field is false because serviceMode
// defaults to false.
func TestHealthEndpoint_DaemonMode_AgentAvailableFalse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv, cleanup := startTestServer(t, nil)
	defer cleanup()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", srv.GetPort()))
	require.NoError(t, err, "GET /health failed")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	agentAvail, ok := body["agent_available"].(bool)
	require.True(t, ok, "agent_available should be a boolean")
	assert.False(t, agentAvail, "agent_available should be false in daemon mode (nil agent, !serviceMode)")
}

// TestHealthEndpoint_SharedMode_AgentAvailableTrue verifies that when the
// server is constructed with a live agent (shared mode) the agent_available
// field is true.
func TestHealthEndpoint_SharedMode_AgentAvailableTrue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockAgent := &agent.Agent{}
	srv, cleanup := startTestServer(t, mockAgent)
	defer cleanup()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", srv.GetPort()))
	require.NoError(t, err, "GET /health failed")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	agentAvail, ok := body["agent_available"].(bool)
	require.True(t, ok, "agent_available should be a boolean")
	assert.True(t, agentAvail, "agent_available should be true when ws.agent != nil")
}
