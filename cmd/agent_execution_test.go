// Tests for agent_execution.go - specifically the --port hidden alias
package cmd

import (
	"strings"
	"testing"
)

// =============================================================================
// Tests for --port hidden alias (Docker/cloud entrypoint compatibility)
// =============================================================================

func TestPortAlias_HiddenFlag(t *testing.T) {
	// Verify that the --port flag is registered but hidden
	flag := agentCmd.Flags().Lookup("port")
	if flag == nil {
		t.Fatal("expected --port flag to be registered on agentCmd")
	}

	if !flag.Hidden {
		t.Error("--port flag should be hidden (not appear in help text)")
	}
}

func TestPortAlias_HasSameValueAsWebPort(t *testing.T) {
	// Save original values
	origWebPort := webPort
	defer func() { webPort = origWebPort }()

	// Test with --web-port flag
	webPort = 0
	err := agentCmd.Flags().Set("web-port", "56000")
	if err != nil {
		t.Fatalf("failed to set --web-port flag: %v", err)
	}
	webPortValue := webPort

	// Reset and test with --port flag
	webPort = 0
	err = agentCmd.Flags().Set("port", "56000")
	if err != nil {
		t.Fatalf("failed to set --port flag: %v", err)
	}
	portValue := webPort

	// Both should set webPort to the same value
	if webPortValue != 56000 {
		t.Errorf("after setting --web-port 56000, webPort = %d, want 56000", webPortValue)
	}
	if portValue != 56000 {
		t.Errorf("after setting --port 56000, webPort = %d, want 56000", portValue)
	}
	if webPortValue != portValue {
		t.Errorf("--web-port and --port should set the same value: --web-port=%d, --port=%d",
			webPortValue, portValue)
	}
}

func TestPortAlias_DifferentPortNumber(t *testing.T) {
	// Save original values
	origWebPort := webPort
	defer func() { webPort = origWebPort }()

	// Test with a different port number using --port
	webPort = 0
	testPort := 8080
	err := agentCmd.Flags().Set("port", "8080")
	if err != nil {
		t.Fatalf("failed to set --port flag: %v", err)
	}

	if webPort != testPort {
		t.Errorf("after setting --port 8080, webPort = %d, want %d", webPort, testPort)
	}
}

func TestPortAlias_WorksWithDaemonFlag(t *testing.T) {
	// Save original values
	origWebPort := webPort
	origDaemonMode := daemonMode
	defer func() {
		webPort = origWebPort
		daemonMode = origDaemonMode
	}()

	// Reset values
	webPort = 0
	daemonMode = false

	// Set both --port and --daemon flags
	err := agentCmd.Flags().Set("port", "55000")
	if err != nil {
		t.Fatalf("failed to set --port flag: %v", err)
	}

	err = agentCmd.Flags().Set("daemon", "true")
	if err != nil {
		t.Fatalf("failed to set --daemon flag: %v", err)
	}

	// Verify both flags are set correctly
	if webPort != 55000 {
		t.Errorf("after setting --port 55000, webPort = %d, want 55000", webPort)
	}
	if !daemonMode {
		t.Error("after setting --daemon, daemonMode should be true")
	}
}

func TestPortAlias_WebPortFlagNotHidden(t *testing.T) {
	// Verify that the --web-port flag is NOT hidden
	flag := agentCmd.Flags().Lookup("web-port")
	if flag == nil {
		t.Fatal("expected --web-port flag to be registered on agentCmd")
	}

	if flag.Hidden {
		t.Error("--web-port flag should NOT be hidden (it's the primary flag)")
	}
}

func TestPortAlias_FlagsRegistered(t *testing.T) {
	// Verify both flags are registered
	portFlag := agentCmd.Flags().Lookup("port")
	webPortFlag := agentCmd.Flags().Lookup("web-port")

	if portFlag == nil {
		t.Error("--port flag should be registered on agentCmd")
	}

	if webPortFlag == nil {
		t.Error("--web-port flag should be registered on agentCmd")
	}

	// Both flags should have the same type (int)
	if portFlag != nil && webPortFlag != nil {
		if portFlag.Value.Type() != "int" {
			t.Errorf("--port flag type = %s, want \"int\"", portFlag.Value.Type())
		}
		if webPortFlag.Value.Type() != "int" {
			t.Errorf("--web-port flag type = %s, want \"int\"", webPortFlag.Value.Type())
		}
	}
}

func TestPortAlias_WebPortDefault(t *testing.T) {
	// Verify default value is 0 for both flags
	portFlag := agentCmd.Flags().Lookup("port")
	webPortFlag := agentCmd.Flags().Lookup("web-port")

	if portFlag == nil || webPortFlag == nil {
		t.Fatal("both --port and --web-port flags should be registered")
	}

	// The default value should be 0 for both
	if portFlag.DefValue != "0" {
		t.Errorf("--port flag default value = %s, want \"0\"", portFlag.DefValue)
	}
	if webPortFlag.DefValue != "0" {
		t.Errorf("--web-port flag default value = %s, want \"0\"", webPortFlag.DefValue)
	}
}

func TestPortAlias_InvalidValue(t *testing.T) {
	// Verify that non-integer values are rejected
	err := agentCmd.Flags().Set("port", "abc")
	if err == nil {
		t.Error("expected error for non-integer --port value")
	}
}

func TestPortAlias_PortFlagUsage(t *testing.T) {
	// Verify --port has empty usage (it's hidden)
	portFlag := agentCmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Fatal("expected --port flag to be registered on agentCmd")
	}

	// The hidden alias has empty usage string
	if portFlag.Usage != "" {
		t.Errorf("--port flag usage should be empty for hidden flag, got %q", portFlag.Usage)
	}

	// --web-port should have a non-empty usage string
	webPortFlag := agentCmd.Flags().Lookup("web-port")
	if webPortFlag == nil {
		t.Fatal("expected --web-port flag to be registered on agentCmd")
	}

	if webPortFlag.Usage == "" {
		t.Error("--web-port flag should have a non-empty usage string")
	}
}

// =============================================================================
// Tests for --bind flag (bind address for web UI)
// =============================================================================

func TestBindFlag_IsRegistered(t *testing.T) {
	// Verify that the --bind flag is registered
	flag := agentCmd.Flags().Lookup("bind")
	if flag == nil {
		t.Fatal("expected --bind flag to be registered on agentCmd")
	}

	// Verify it's a string flag
	if flag.Value.Type() != "string" {
		t.Errorf("--bind flag type = %s, want \"string\"", flag.Value.Type())
	}
}

func TestBindFlag_DefaultValue(t *testing.T) {
	// Verify default value is empty string (will resolve to 127.0.0.1)
	flag := agentCmd.Flags().Lookup("bind")
	if flag == nil {
		t.Fatal("expected --bind flag to be registered on agentCmd")
	}

	if flag.DefValue != "" {
		t.Errorf("--bind flag default value = %s, want \"\"", flag.DefValue)
	}
}

func TestBindFlag_SetValue(t *testing.T) {
	// Save original values
	origWebBindAddr := webBindAddr
	defer func() { webBindAddr = origWebBindAddr }()

	// Test setting the flag
	err := agentCmd.Flags().Set("bind", "0.0.0.0")
	if err != nil {
		t.Fatalf("failed to set --bind flag: %v", err)
	}

	if webBindAddr != "0.0.0.0" {
		t.Errorf("after setting --bind 0.0.0.0, webBindAddr = %s, want \"0.0.0.0\"", webBindAddr)
	}
}

func TestBindFlag_HasUsageText(t *testing.T) {
	// Verify that the --bind flag has usage text
	flag := agentCmd.Flags().Lookup("bind")
	if flag == nil {
		t.Fatal("expected --bind flag to be registered on agentCmd")
	}

	if flag.Usage == "" {
		t.Error("--bind flag should have a non-empty usage string")
	}

	// Verify usage mentions the env var
	if !strings.Contains(flag.Usage, "SPROUT_BIND_ADDR") {
		t.Error("--bind flag usage should mention SPROUT_BIND_ADDR env var")
	}
}

func TestBindFlag_NotHidden(t *testing.T) {
	// Verify that the --bind flag is NOT hidden
	flag := agentCmd.Flags().Lookup("bind")
	if flag == nil {
		t.Fatal("expected --bind flag to be registered on agentCmd")
	}

	if flag.Hidden {
		t.Error("--bind flag should NOT be hidden (it's a user-facing flag)")
	}
}

func TestBindFlag_AllowedAddresses(t *testing.T) {
	// Save original values
	origWebBindAddr := webBindAddr
	defer func() { webBindAddr = origWebBindAddr }()

	allowedAddresses := []string{
		"0.0.0.0",
		"127.0.0.1",
		"localhost",
		"192.168.1.100",
		"10.0.0.5",
	}

	for _, addr := range allowedAddresses {
		err := agentCmd.Flags().Set("bind", addr)
		if err != nil {
			t.Errorf("failed to set --bind to %q: %v", addr, err)
		}
		if webBindAddr != addr {
			t.Errorf("after setting --bind %q, webBindAddr = %s, want %q", addr, webBindAddr, addr)
		}
	}
}
