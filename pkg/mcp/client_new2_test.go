package mcp

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

func TestNewMCPClient_DefaultHealthInterval(t *testing.T) {
	config := MCPServerConfig{Name: "test"}
	client := NewMCPClient(config, utils.GetLogger(true))

	if client.config.Name != "test" {
		t.Errorf("expected config name 'test', got %q", client.config.Name)
	}
	if client.healthInterval != 30*time.Second {
		t.Errorf("expected default health interval 30s, got %v", client.healthInterval)
	}
	if client.running {
		t.Error("client should not be running initially")
	}
	if client.initialized {
		t.Error("client should not be initialized initially")
	}
}

func TestNewMCPClient_ShortTimeoutAdjustsHealthInterval(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test",
		Timeout: 10 * time.Second,
	}
	client := NewMCPClient(config, utils.GetLogger(true))

	// With timeout < 60s, health interval = timeout * 2
	if client.healthInterval != 20*time.Second {
		t.Errorf("expected health interval 20s (2x timeout), got %v", client.healthInterval)
	}
}

func TestNewMCPClient_LongTimeoutUsesDefaultHealthInterval(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test",
		Timeout: 120 * time.Second,
	}
	client := NewMCPClient(config, utils.GetLogger(true))

	// Timeout >= 60s uses default 30s health interval
	if client.healthInterval != 30*time.Second {
		t.Errorf("expected default health interval 30s, got %v", client.healthInterval)
	}
}

func TestMCPClient_GetNameAndConfig(t *testing.T) {
	config := MCPServerConfig{Name: "my-server", Command: "test-cmd"}
	client := NewMCPClient(config, utils.GetLogger(true))

	if client.GetName() != "my-server" {
		t.Errorf("GetName() = %q, want 'my-server'", client.GetName())
	}

	got := client.GetConfig()
	if got.Name != "my-server" {
		t.Errorf("GetConfig().Name = %q", got.Name)
	}
	if got.Command != "test-cmd" {
		t.Errorf("GetConfig().Command = %q", got.Command)
	}
}

func TestMCPClient_IsRunning_Initially(t *testing.T) {
	config := MCPServerConfig{Name: "test"}
	client := NewMCPClient(config, utils.GetLogger(true))

	if client.IsRunning() {
		t.Error("client should not be running initially")
	}
}

func TestMCPClient_CalculateBackoff(t *testing.T) {
	config := MCPServerConfig{Name: "test"}
	client := NewMCPClient(config, utils.GetLogger(true))

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 32 * time.Second},
		{7, 64 * time.Second},
		{8, 128 * time.Second},
		{20, 5 * time.Minute},
	}
	for _, tc := range tests {
		got := client.calculateBackoff(tc.attempt)
		if got != tc.want {
			t.Errorf("calculateBackoff(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestMCPClient_GetMaxRestarts_Default(t *testing.T) {
	config := MCPServerConfig{Name: "test"}
	client := NewMCPClient(config, utils.GetLogger(true))

	if client.getMaxRestarts() != 3 {
		t.Errorf("default max restarts should be 3, got %d", client.getMaxRestarts())
	}
}

func TestMCPClient_GetMaxRestarts_Custom(t *testing.T) {
	config := MCPServerConfig{Name: "test", MaxRestarts: 5}
	client := NewMCPClient(config, utils.GetLogger(true))

	if client.getMaxRestarts() != 5 {
		t.Errorf("custom max restarts should be 5, got %d", client.getMaxRestarts())
	}
}

func TestMCPClient_StartWhileReconnecting(t *testing.T) {
	config := MCPServerConfig{Name: "test"}
	client := NewMCPClient(config, utils.GetLogger(true))
	client.reconnecting = true

	err := client.Start(t.Context())
	if err == nil {
		t.Error("expected error when starting while reconnecting")
	}
}
