//go:build !js

package webui

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestNewReactWebServer_SocketPath verifies that when socketPath is provided,
// the TCP security check (non-localhost + no auth token) is skipped.
func TestNewReactWebServer_SocketPath(t *testing.T) {
	eventBus := events.NewEventBus()

	// A non-localhost bindAddr would normally fail without AUTH_TOKEN.
	// But with socketPath set, it should succeed because Unix sockets are local-only.
	server, err := NewReactWebServer(nil, eventBus, 0, "0.0.0.0", "/tmp/sprout_test_socket_"+t.Name(), "")
	if err != nil {
		t.Fatalf("NewReactWebServer with socketPath should skip TCP security check: %v", err)
	}
	if server.socketPath != "/tmp/sprout_test_socket_"+t.Name() {
		t.Errorf("socketPath not stored correctly, got %q", server.socketPath)
	}
}

// TestNewReactWebServer_SocketPathOverridesTCP verifies that socketPath overrides
// port and bindAddr when provided.
func TestNewReactWebServer_SocketPathOverridesTCP(t *testing.T) {
	eventBus := events.NewEventBus()

	server, err := NewReactWebServer(nil, eventBus, 8080, "10.0.0.1", "/tmp/sprout_test_override_"+t.Name(), "")
	if err != nil {
		t.Fatalf("NewReactWebServer with socketPath: %v", err)
	}
	if server.socketPath == "" {
		t.Error("socketPath should be set")
	}
	// In socket mode, port should be zeroed and bindAddr cleared
	if server.port != 0 {
		t.Errorf("port should be 0 in socket mode, got %d", server.port)
	}
	if server.bindAddr != "" {
		t.Errorf("bindAddr should be empty in socket mode, got %q", server.bindAddr)
	}
}

// TestServer_StartUnixSocket verifies that the server can start on a Unix socket,
// accept connections, and clean up the socket file on shutdown.
func TestServer_StartUnixSocket(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "sprout.sock")

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "", socketPath, "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- server.Start(ctx)
	}()

	// Wait for server to start
	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start on unix socket: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start on unix socket")
	}

	if !server.IsRunning() {
		t.Fatal("server should be running")
	}

	// Verify socket file exists and has correct permissions
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("socket file should exist: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("socket file permissions should be 0600, got %o", perm)
	}

	// Test that we can connect to the socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to connect to unix socket: %v", err)
	}
	conn.Close()

	// Make an HTTP request to the socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
	req, _ := http.NewRequest("GET", "http://localhost/", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HTTP request to unix socket: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Shutdown and verify socket file is removed
	cancel()
	time.Sleep(500 * time.Millisecond)

	// Check that socket file is cleaned up
	if _, err := os.Stat(socketPath); err == nil {
		t.Error("socket file should be removed after shutdown")
	}
}

// TestServer_StartUnixSocket_ExistingSocket verifies that if a stale socket file
// exists but nothing is listening, it is cleaned up and the server starts.
func TestServer_StartUnixSocket_ExistingSocket(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "sprout.sock")
	if len(socketPath) > 100 {
		t.Skipf("socket path too long for Unix domain socket (%d chars): %s", len(socketPath), socketPath)
	}

	// Create a stale socket file
	err := os.WriteFile(socketPath, []byte("stale"), 0600)
	if err != nil {
		t.Fatalf("failed to create stale socket file: %v", err)
	}

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "", socketPath, "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- server.Start(ctx)
	}()

	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start on unix socket with stale file: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start on unix socket")
	}

	if !server.IsRunning() {
		t.Fatal("server should be running after removing stale socket")
	}
}

// TestServer_StartUnixSocket_InUse verifies that if something IS listening
// on the socket, the server returns an error.
func TestServer_StartUnixSocket_InUse(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "sprout.sock")
	if len(socketPath) > 100 {
		t.Skipf("socket path too long for Unix domain socket (%d chars): %s", len(socketPath), socketPath)
	}

	// Start a real listener on the socket to simulate "address in use"
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "", socketPath, "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- server.Start(ctx)
	}()

	select {
	case err := <-startErr:
		if err == nil {
			t.Fatal("expected error when socket is in use")
		}
		// Check the error message mentions "address already in use"
		if !strings.Contains(err.Error(), "address already in use") {
			t.Errorf("expected 'address already in use' error, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for error")
	}
}

// TestNewReactWebServer_AuthTokenParam verifies that the authToken parameter
// takes precedence over the SPROUT_AUTH_TOKEN env var.
func TestNewReactWebServer_AuthTokenParam(t *testing.T) {
	eventBus := events.NewEventBus()

	// Test with explicit auth token
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "my-secret-token")
	if err != nil {
		t.Fatalf("NewReactWebServer with authToken: %v", err)
	}
	if server.authToken != "my-secret-token" {
		t.Errorf("authToken should be 'my-secret-token', got %q", server.authToken)
	}

	// Test without auth token - should fall back to env (empty in tests)
	server2, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer without authToken: %v", err)
	}
	if server2.authToken != "" {
		t.Errorf("authToken should be empty when param and env are empty, got %q", server2.authToken)
	}
}

// TestNewReactWebServer_AuthTokenParamOverridesEnv verifies that explicit
// authToken param overrides SPROUT_AUTH_TOKEN env var.
func TestNewReactWebServer_AuthTokenParamOverridesEnv(t *testing.T) {
	eventBus := events.NewEventBus()

	t.Setenv("SPROUT_AUTH_TOKEN", "env-token")
	// Explicit param should take precedence
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "param-token")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	if server.authToken != "param-token" {
		t.Errorf("authToken should be 'param-token' (param overrides env), got %q", server.authToken)
	}
}

// TestNewReactWebServer_SocketPath_SkipsSecurityCheck verifies that socket mode
// skips the non-localhost security check that would normally require AUTH_TOKEN.
func TestNewReactWebServer_SocketPath_SkipsSecurityCheck(t *testing.T) {
	eventBus := events.NewEventBus()

	// Without socketPath, binding to 0.0.0.0 without AUTH_TOKEN should fail
	_, err := NewReactWebServer(nil, eventBus, 0, "0.0.0.0", "", "")
	if err == nil {
		t.Fatal("expected error when binding to 0.0.0.0 without auth token")
	}

	// With socketPath, the same bindAddr should succeed
	server, err := NewReactWebServer(nil, eventBus, 0, "0.0.0.0", "/tmp/sprout_test_security_"+t.Name(), "")
	if err != nil {
		t.Fatalf("socket mode should skip TCP security check: %v", err)
	}
	if server.socketPath == "" {
		t.Error("socketPath should be set")
	}
}

// TestServer_GetPort_SocketMode verifies that GetPort() returns 0 in socket mode,
// regardless of what port was originally passed to NewReactWebServer.
func TestServer_GetPort_SocketMode(t *testing.T) {
	eventBus := events.NewEventBus()

	// Pass a non-zero port; in socket mode it should be zeroed out.
	server, err := NewReactWebServer(nil, eventBus, 8080, "10.0.0.1", "/tmp/sprout_test_port_"+t.Name(), "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	if server.GetPort() != 0 {
		t.Errorf("GetPort() should return 0 in socket mode, got %d", server.GetPort())
	}

	// Pass port 0 (default); should still be 0.
	server2, err := NewReactWebServer(nil, eventBus, 0, "", "/tmp/sprout_test_port2_"+t.Name(), "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	if server2.GetPort() != 0 {
		t.Errorf("GetPort() should return 0 in socket mode, got %d", server2.GetPort())
	}
}

// TestNewReactWebServer_AuthTokenEnvFallback verifies that when no auth token
// param is given, the SPROUT_AUTH_TOKEN env var is used as fallback.
func TestNewReactWebServer_AuthTokenEnvFallback(t *testing.T) {
	eventBus := events.NewEventBus()

	t.Setenv("SPROUT_AUTH_TOKEN", "env-fallback-token")

	// No explicit param — should fall back to env var
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	if server.authToken != "env-fallback-token" {
		t.Errorf("authToken should fall back to env var, got %q", server.authToken)
	}
}

// TestNewReactWebServer_AuthToken_SecurityBypass verifies that providing an
// auth token allows binding to a non-localhost address.
func TestNewReactWebServer_AuthToken_SecurityBypass(t *testing.T) {
	eventBus := events.NewEventBus()

	// Without auth token, binding to 0.0.0.0 should fail.
	_, err := NewReactWebServer(nil, eventBus, 0, "0.0.0.0", "", "")
	if err == nil {
		t.Fatal("expected error when binding to 0.0.0.0 without auth token")
	}

	// With auth token (via param), binding to 0.0.0.0 should succeed.
	server, err := NewReactWebServer(nil, eventBus, 0, "0.0.0.0", "", "secret-token")
	if err != nil {
		t.Fatalf("auth token should allow non-localhost binding: %v", err)
	}
	if server.authToken != "secret-token" {
		t.Errorf("authToken should be 'secret-token', got %q", server.authToken)
	}

	// With auth token (via env var), binding to 0.0.0.0 should succeed.
	t.Setenv("SPROUT_AUTH_TOKEN", "env-secret")
	server2, err := NewReactWebServer(nil, eventBus, 0, "0.0.0.0", "", "")
	if err != nil {
		t.Fatalf("env auth token should allow non-localhost binding: %v", err)
	}
	if server2.authToken != "env-secret" {
		t.Errorf("authToken should be 'env-secret', got %q", server2.authToken)
	}
}
