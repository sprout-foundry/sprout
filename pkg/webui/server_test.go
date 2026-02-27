package webui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/events"
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
	port := FindAvailablePort(54321)

	if port < 54321 || port > 54321+100 {
		t.Errorf("Expected port in range [54321, 54421], got %d", port)
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
	server := NewReactWebServer(&agent.Agent{}, events.NewEventBus(), port)

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
	port1 := FindAvailablePort(54321)
	port2 := FindAvailablePort(port1 + 1)

	if port1 == port2 {
		t.Fatalf("Expected different ports, got same port %d", port1)
	}

	// Create two web servers
	server1 := NewReactWebServer(agent1, eventBus1, port1)
	server2 := NewReactWebServer(agent2, eventBus2, port2)

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
