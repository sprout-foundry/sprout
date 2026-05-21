package mcp

import (
	"fmt"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// client.go — Constructor and basic accessors
// ---------------------------------------------------------------------------

func TestNewMCPClient_ZC(t *testing.T) {
	t.Parallel()
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	c := NewMCPClient(config, nil)
	if c == nil {
		t.Fatal("NewMCPClient returned nil")
	}
	if c.GetName() != "test-server" {
		t.Errorf("expected 'test-server', got %q", c.GetName())
	}
	if c.IsRunning() {
		t.Error("new client should not be running")
	}
	gotConfig := c.GetConfig()
	if gotConfig.Name != "test-server" {
		t.Errorf("config name mismatch: %q", gotConfig.Name)
	}
}

// ---------------------------------------------------------------------------
// client.go — calculateBackoff
// ---------------------------------------------------------------------------

func TestMCPClientCalculateBackoff_ZC(t *testing.T) {
	t.Parallel()
	c := NewMCPClient(MCPServerConfig{Name: "test"}, nil)

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 32 * time.Second},
		{7, 64 * time.Second},
		{10, 5 * time.Minute}, // capped at 5 minutes
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			t.Parallel()
			got := c.calculateBackoff(tt.attempt)
			if got != tt.expected {
				t.Errorf("calculateBackoff(%d) = %v, want %v", tt.attempt, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// client.go — getMaxRestarts
// ---------------------------------------------------------------------------

func TestMCPClientGetMaxRestarts_ZC(t *testing.T) {
	t.Parallel()
	t.Run("default", func(t *testing.T) {
		c := NewMCPClient(MCPServerConfig{Name: "test"}, nil)
		if got := c.getMaxRestarts(); got != 3 {
			t.Errorf("default should be 3, got %d", got)
		}
	})
	t.Run("custom", func(t *testing.T) {
		c := NewMCPClient(MCPServerConfig{Name: "test", MaxRestarts: 5}, nil)
		if got := c.getMaxRestarts(); got != 5 {
			t.Errorf("custom should be 5, got %d", got)
		}
	})
}

// ---------------------------------------------------------------------------
// client.go — IsRunning
// ---------------------------------------------------------------------------

func TestMCPClientIsRunning_ZC(t *testing.T) {
	t.Parallel()
	c := NewMCPClient(MCPServerConfig{Name: "test"}, nil)
	if c.IsRunning() {
		t.Error("new client should not be running")
	}
}

// ---------------------------------------------------------------------------
// client.go — GetName
// ---------------------------------------------------------------------------

func TestMCPClientGetName_ZC(t *testing.T) {
	t.Parallel()
	c := NewMCPClient(MCPServerConfig{Name: "my-server"}, nil)
	if c.GetName() != "my-server" {
		t.Errorf("expected 'my-server', got %q", c.GetName())
	}
}

// ---------------------------------------------------------------------------
// client.go — GetConfig
// ---------------------------------------------------------------------------

func TestMCPClientGetConfig_ZC(t *testing.T) {
	t.Parallel()
	config := MCPServerConfig{
		Name:    "server1",
		Command: "node",
		Args:    []string{"server.js"},
	}
	c := NewMCPClient(config, nil)
	got := c.GetConfig()
	if got.Command != "node" {
		t.Errorf("expected 'node', got %q", got.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "server.js" {
		t.Errorf("args mismatch: %v", got.Args)
	}
}
