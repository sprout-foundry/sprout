//go:build !js

package webui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestCheckPortAvailable verifies port availability checking
func TestCheckPortAvailable(t *testing.T) {
	// Create actual server to bind port
	server := &http.Server{
		Addr: ":0", // Let OS pick available port
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatalf("Failed to bind listener: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// Start server
	go func() {
		_ = server.Serve(listener)
	}()

	// Give server time to bind
	time.Sleep(100 * time.Millisecond)

	// Port should be unavailable now
	if CheckPortAvailable(port) {
		t.Errorf("Expected port %d to be unavailable after binding", port)
	}

	// Shutdown server
	_ = server.Close()
	time.Sleep(200 * time.Millisecond)

	// On some systems, ports may stay in TIME_WAIT, check a few times
	available := false
	for i := 0; i < 3; i++ {
		if CheckPortAvailable(port) {
			available = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// If still not available after wait, log it but don't fail (system-dependent)
	if !available {
		t.Logf("Note: port %d not immediately available after close (acceptable - TIME_WAIT state)", port)
	}
}

// TestFindAvailablePort verifies port finding logic
func TestFindAvailablePort(t *testing.T) {
	// Get available port
	port, err := FindAvailablePort(DaemonPort)
	if err != nil {
		t.Fatalf("FindAvailablePort failed: %v", err)
	}

	if port < DaemonPort || port > DaemonPort+99 {
		t.Errorf("Expected port in range [%d, %d], got %d", DaemonPort, DaemonPort+99, port)
	}

	// Verify it's actually available
	if !CheckPortAvailable(port) {
		t.Errorf("Found port %d is not available", port)
	}
}

// TestStartFailsWhenPortAlreadyInUse verifies startup state remains consistent on bind failures.
func TestStartFailsWhenPortAlreadyInUse(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to reserve test port: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	server, err := NewReactWebServer(&agent.Agent{}, events.NewEventBus(), port, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = server.Start(ctx)
	if err == nil {
		t.Fatalf("expected Start to fail when port %d is already in use", port)
	}
	if server.IsRunning() {
		t.Fatalf("server should not report running after failed start on port %d", port)
	}
}

// TestMultipleServersOnDifferentPorts verifies multiple servers can start simultaneously
func TestMultipleServersOnDifferentPorts(t *testing.T) {
	// Skip if agent initialization would fail
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create mock agents for testing
	agent1 := &agent.Agent{}
	agent2 := &agent.Agent{}
	eventBus1 := events.NewEventBus()
	eventBus2 := events.NewEventBus()

	// Find two different ports
	port1, err := FindAvailablePort(DaemonPort)
	if err != nil {
		t.Fatalf("FindAvailablePort failed for port1: %v", err)
	}
	port2, err := FindAvailablePort(port1 + 1)
	if err != nil {
		t.Fatalf("FindAvailablePort failed for port2: %v", err)
	}

	if port1 == port2 {
		t.Fatalf("Expected different ports, got same port %d", port1)
	}

	// Create two web servers
	server1, err := NewReactWebServer(agent1, eventBus1, port1, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
		}
	server2, err := NewReactWebServer(agent2, eventBus2, port2, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
		}

	// Start both servers
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	if err := server1.Start(ctx1); err != nil {
		t.Fatalf("Failed to start server1: %v", err)
	}
	if err := server2.Start(ctx2); err != nil {
		t.Fatalf("Failed to start server2: %v", err)
	}

	// Give both servers time to start
	time.Sleep(200 * time.Millisecond)

	// Verify both servers are running
	if !server1.IsRunning() {
		t.Error("Server 1 is not running")
	}
	if !server2.IsRunning() {
		t.Error("Server 2 is not running")
	}

	// Verify both ports are bound
	if CheckPortAvailable(port1) {
		t.Errorf("Port %d should be bound to server1", port1)
	}
	if CheckPortAvailable(port2) {
		t.Errorf("Port %d should be bound to server2", port2)
	}

	// Verify health endpoints work
	resp1, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port1))
	if err != nil {
		t.Errorf("Failed to reach server1 health endpoint: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 from server1, got %d", resp1.StatusCode)
	}

	resp2, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port2))
	if err != nil {
		t.Errorf("Failed to reach server2 health endpoint: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 from server2, got %d", resp2.StatusCode)
	}

	// Shutdown both servers
	if err := server1.Shutdown(); err != nil {
		t.Errorf("Failed to shutdown server1: %v", err)
	}
	if err := server2.Shutdown(); err != nil {
		t.Errorf("Failed to shutdown server2: %v", err)
	}

	// Wait for shutdown to complete
	time.Sleep(100 * time.Millisecond)

	// Verify both servers stopped
	if server1.IsRunning() {
		t.Error("Server 1 should be stopped")
	}
	if server2.IsRunning() {
		t.Error("Server 2 should be stopped")
	}

	// Verify both ports are now available
	if !CheckPortAvailable(port1) {
		t.Errorf("Port %d should be available after shutdown", port1)
	}
	if !CheckPortAvailable(port2) {
		t.Errorf("Port %d should be available after shutdown", port2)
	}
}

// TestCustomBindAddress verifies the server binds to a custom address
func TestCustomBindAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Find an available port to avoid conflicts with running servers
	port, err := FindAvailablePort(DaemonPort + 500)
	if err != nil {
		t.Fatalf("FindAvailablePort failed: %v", err)
	}

	server, err := NewReactWebServer(&agent.Agent{}, events.NewEventBus(), port, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Shutdown()

	// Verify the server is using the correct bind address
	if server.bindAddr != "127.0.0.1" {
		t.Errorf("Server bindAddr = %s, want \"127.0.0.1\"", server.bindAddr)
	}

	// Verify the server is running
	if !server.IsRunning() {
		t.Error("Server should be running")
	}
}

// TestBindAddrStoredCorrectly verifies the bind address is correctly stored
// on the server after construction and start.
func TestBindAddrStoredCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use a non-zero port to avoid DaemonPort (56000) conflicts
	// This test verifies that the bindAddr is correctly stored, not dynamic port allocation
	port, err := FindAvailablePort(DaemonPort + 200)
	if err != nil {
		t.Fatalf("FindAvailablePort failed: %v", err)
	}

	server, err := NewReactWebServer(&agent.Agent{}, events.NewEventBus(), port, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Shutdown()

	// Port should match what we specified
	if server.GetPort() != port {
		t.Errorf("Server port = %d, want %d", server.GetPort(), port)
	}

	// Verify the server is using the correct bind address
	if server.bindAddr != "127.0.0.1" {
		t.Errorf("Server bindAddr = %s, want \"127.0.0.1\"", server.bindAddr)
	}
}

func TestDisplayAddr(t *testing.T) {
	tests := []struct{ input, want string }{
		{"127.0.0.1", "localhost"},
		{"0.0.0.0", "localhost"},
		{"::", "localhost"},
		{"::1", "localhost"},
		{"192.168.1.1", "192.168.1.1"},
		{"10.0.0.5", "10.0.0.5"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := DisplayAddr(tt.input); got != tt.want {
				t.Errorf("DisplayAddr(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatListenAddr(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{"127.0.0.1", 8080, "127.0.0.1:8080"},
		{"0.0.0.0", 443, "0.0.0.0:443"},
		{"::", 56000, "[::]:56000"},
		{"::1", 8080, "[::1]:8080"},
		{"fe80::1", 9090, "[fe80::1]:9090"},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s:%d", tt.host, tt.port)
		t.Run(name, func(t *testing.T) {
			if got := formatListenAddr(tt.host, tt.port); got != tt.want {
				t.Errorf("formatListenAddr(%q, %d) = %q, want %q", tt.host, tt.port, got, tt.want)
			}
		})
	}
}

// TestLooksLikeUserHome locks the heuristic that decides whether a daemonRoot
// candidate is plausibly a per-user home directory. The runtime uses this to
// trigger a /etc/passwd fallback when a stale launchd/systemd plist leaks a
// system path as $HOME (the failure mode that scoped the workspace browser
// to the wrong directory in service mode).
func TestLooksLikeUserHome(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// System paths a service manager may inherit — NOT a user home.
		{"", false},
		{"/", false},
		{"/var/root", false},
		{"/var/empty", false},
		{"/nonexistent", false},
		{"/usr", false},
		{"/etc", false},
		{"/tmp", false},
		{"/var", false},
		{"/private", false},

		// Real user homes — must be accepted on every platform.
		{"/Users/alice", true},
		{"/Users/alice/", true},
		{"/home/bob", true},
		{"/root", true},

		// Custom/container/NFS mounts — give the benefit of the doubt rather
		// than nuking a env-supplied path with a /etc/passwd lookup that may
		// itself be wrong on a non-standard mount.
		{"/workspace", true},
		{"/data/users/charlie", true},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := looksLikeUserHome(c.path); got != c.want {
				t.Errorf("looksLikeUserHome(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}
