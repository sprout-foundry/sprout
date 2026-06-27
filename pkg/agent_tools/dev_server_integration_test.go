//go:build !js

package tools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TestIntegration_DevServerStartAndStop — Full lifecycle: start HTTP server,
// verify it responds, stop it, verify port is released.
// =============================================================================

func TestIntegration_DevServerStartAndStop(t *testing.T) {
	// NOTE: not parallel — uses a real network port, so concurrent runs
	// could conflict.

	// Skip if python3 is not available
	if !python3Available() {
		t.Skip("python3 is not installed")
	}

	// Skip in short mode — this is a real network integration test
	if testing.Short() {
		t.Skip("skipping dev server integration test in short mode")
	}

	// Find a free port dynamically to avoid conflicts with parallel tests
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "should find a free port")
	testPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	bpm := NewBackgroundProcessManager()

	// Ensure cleanup regardless of test outcome (prevents orphaned servers)
	t.Cleanup(func() {
		bpm.StopAll()
		bpm.Close()
	})

	cmd := fmt.Sprintf("python3 -u -m http.server %d", testPort)

	// Use -u for unbuffered output so "Serving HTTP" appears in the output file
	// promptly. Without -u, Python's I/O is fully buffered when not connected
	// to a TTY, and the message may never flush to the file during the test.
	sessionID, err := bpm.Start(context.Background(), cmd, "")
	require.NoError(t, err, "should start python3 http.server in background")
	require.NotEmpty(t, sessionID, "should return a non-empty session ID")

	// Poll for "Serving HTTP" in output (up to 15 seconds, 500ms intervals)
	var foundServing bool
	for i := 0; i < 30; i++ {
		output, status, err := bpm.CheckOutput(sessionID)
		require.NoError(t, err)

		if strings.Contains(output, "Serving HTTP") {
			foundServing = true
			assert.Equal(t, "running", status, "process should still be running after output appears")
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.True(t, foundServing, "should see 'Serving HTTP' in server output within 15 seconds")

	// Give the server a moment to fully bind
	time.Sleep(500 * time.Millisecond)

	// Verify the server is responding to HTTP requests
	url := fmt.Sprintf("http://127.0.0.1:%d", testPort)
	resp, err := http.Get(url)
	require.NoError(t, err, "http server should be reachable on port %d", testPort)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "http server should return 200 OK")

	// Stop the session
	err = bpm.Stop(sessionID, 2*time.Second)
	require.NoError(t, err, "stop should succeed")

	// Wait a moment for the port to be released
	time.Sleep(1 * time.Second)

	// Verify port is no longer reachable
	assert.False(t, canDialPort(testPort),
		"port %d should no longer be reachable after stop", testPort)
}

// python3Available checks if python3 can be executed
func python3Available() bool {
	err := exec.Command("python3", "--version").Run()
	return err == nil
}

// canDialPort attempts a TCP connection to localhost:port
func canDialPort(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
