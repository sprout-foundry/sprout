//go:build !js

package webui

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestNormalizeRemoteWorkspacePathSSH(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"~", "$HOME"},
		{"~/project", "$HOME/project"},
		{"~/deep/nested/path", "$HOME/deep/nested/path"},
		{"${HOME}", "$HOME"},
		{"${HOME}/project", "$HOME/project"},
		{"/absolute/path", "/absolute/path"},
		{"$HOME/project", "$HOME/project"},
		{".", "."},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeRemoteWorkspacePath(tt.input)
			if got != tt.want {
				t.Errorf("normalizeRemoteWorkspacePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsRetryableSSHHealthError(t *testing.T) {
	// Test with actual error messages
	testCases := []struct {
		msg  string
		want bool
	}{
		{"connection reset by peer", true},
		{"Connection Reset By Peer", true},
		{"connection refused", true},
		{"unexpected EOF", true},
		{"EOF", true},
		{"some other error", false},
		{"", false},
	}
	for _, tt := range testCases {
		t.Run(tt.msg, func(t *testing.T) {
			err := createTestSSHError(tt.msg)
			got := isRetryableSSHHealthError(err)
			if got != tt.want {
				t.Errorf("isRetryableSSHHealthError(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

// createTestSSHError creates an error with the given message for testing
func createTestSSHError(msg string) error {
	return &testSSHError{msg: msg}
}

type testSSHError struct {
	msg string
}

func (e *testSSHError) Error() string {
	return e.msg
}

func TestSanitizeRemoteLogName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-host", "my-host"},
		{"user@host", "user_host"},
		{"127.0.0.1", "127.0.0.1"},
		{"host.example.com", "host.example.com"},
		{"", "ssh"},
		{"host with spaces", "host_with_spaces"},
		{"a:b:c", "a_b_c"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeRemoteLogName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeRemoteLogName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSetSSHLaunchStatus(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.setSSHLaunchStatus("test-key", "step", "status", true, "error")
	status := ws.getSSHLaunchStatus("test-key")
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Step != "step" {
		t.Errorf("expected step, got %q", status.Step)
	}
	if status.Status != "status" {
		t.Errorf("expected status, got %q", status.Status)
	}
	if !status.InProgress {
		t.Error("expected InProgress to be true")
	}
	if status.LastError != "error" {
		t.Errorf("expected error, got %q", status.LastError)
	}
}

func TestSetSSHLaunchStatusEmptyKey(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic
	ws.setSSHLaunchStatus("", "step", "status", true, "error")
	ws.setSSHLaunchStatus("  ", "step", "status", true, "error")
}

func TestGetSSHLaunchStatus(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	status := ws.getSSHLaunchStatus("nonexistent")
	if status != nil {
		t.Fatal("expected nil for nonexistent key")
	}

	ws.setSSHLaunchStatus("test-key", "step", "status", true, "")
	status = ws.getSSHLaunchStatus("test-key")
	if status == nil {
		t.Fatal("expected non-nil status after setting")
	}
}

func TestGetSSHLaunchStatusEmptyKey(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	status := ws.getSSHLaunchStatus("")
	if status != nil {
		t.Fatal("expected nil for empty key")
	}
}

func TestStartRemoteSSHBackendSignature(t *testing.T) {
	// Verify the function exists with the correct signature.
	// Actual behavior testing requires a real SSH connection.
	t.Skip("requires actual SSH infrastructure - skipped for CI reliability")
	_ = startRemoteSSHBackend
}

func TestStopRemoteSSHBackend(t *testing.T) {
	t.Run("empty host returns nil", func(t *testing.T) {
		err := stopRemoteSSHBackend("", 1234)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("invalid PID returns nil", func(t *testing.T) {
		err := stopRemoteSSHBackend("host", 0)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("negative PID returns nil", func(t *testing.T) {
		err := stopRemoteSSHBackend("host", -1)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
		}
	})
}

func TestWaitForWebHealthConnectionRefused(t *testing.T) {
	// Use a port that's very unlikely to be in use
	err := waitForWebHealth(59999, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestStopRemoteSSHBackendWhitespaceHost(t *testing.T) {
	err := stopRemoteSSHBackend("  ", 1234)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
