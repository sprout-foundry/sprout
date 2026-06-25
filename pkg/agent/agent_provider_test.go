package agent

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestLooksLikeProviderModelSpecifier(t *testing.T) {
	t.Parallel()
	mgr, err := configuration.NewManagerSilent()
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{name: "openai provider model", model: "openai:gpt-4o", expected: true},
		{name: "ollama provider model", model: "ollama:llama3", expected: true},
		{name: "no colon", model: "claude-sonnet-4", expected: false},
		{name: "empty string", model: "", expected: false},
		{name: "colon only", model: ":", expected: false},
		{name: "empty provider", model: ":claude", expected: false},
		{name: "empty model", model: "openai:", expected: false},
		{name: "unknown provider", model: "bogus:model", expected: false},
		{name: "just provider name", model: "openai", expected: false},
		{name: "multiple colons", model: "openai:sub:model", expected: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeProviderModelSpecifier(mgr, tc.model); got != tc.expected {
				t.Errorf("looksLikeProviderModelSpecifier(%q) = %v, expected %v", tc.model, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ErrProviderNotConfigured sentinel
// ---------------------------------------------------------------------------

func TestErrProviderNotConfigured_IsSentinel(t *testing.T) {
	t.Parallel()

	// Verify the sentinel error is accessible and has the expected string.
	if ErrProviderNotConfigured == nil {
		t.Fatal("ErrProviderNotConfigured should not be nil")
	}

	errMsg := ErrProviderNotConfigured.Error()
	if errMsg == "" {
		t.Fatal("ErrProviderNotConfigured should have a non-empty error message")
	}

	// Verify the error message contains key phrases.
	if !strings.Contains(errMsg, "not configured") {
		t.Errorf("expected error to contain 'not configured', got: %s", errMsg)
	}

	// Verify errors.Is works correctly with the sentinel.
	wrapped := fmt.Errorf("wrapped: %w", ErrProviderNotConfigured)
	if !errors.Is(wrapped, ErrProviderNotConfigured) {
		t.Error("errors.Is should match ErrProviderNotConfigured when wrapped")
	}

	unrelated := errors.New("some other error")
	if errors.Is(unrelated, ErrProviderNotConfigured) {
		t.Error("errors.Is should not match unrelated errors")
	}
}

// ---------------------------------------------------------------------------
// TestRecoverProviderStartup_DaemonMode
// ---------------------------------------------------------------------------

func TestRecoverProviderStartup_DaemonMode_ReturnsErrProviderNotConfigured(t *testing.T) {
	// In daemon mode (SPROUT_DAEMON=1), recoverProviderStartup should return
	// ErrProviderNotConfigured for ANY provider error, not just model-not-found.
	// This allows the web UI to start and present a provider configuration UI.

	t.Setenv("SPROUT_DAEMON", "1")

	// Pipe stdin to guarantee non-interactive detection.
	w, restore := redirectStdinToPipe(t)
	defer restore()
	_ = w.Close()

	// Sanity check: environment must be non-interactive.
	if !isNonInteractive() {
		t.Fatal("precondition failed: stdin must be non-interactive")
	}
	if !isSSHDaemon() {
		t.Fatal("precondition failed: isSSHDaemon() must be true when SPROUT_DAEMON=1")
	}

	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	fakeStartupErr := errors.New("generic provider initialization failure")
	provider, model, err := recoverProviderStartup(
		configManager,
		api.OpenAIClientType,
		"gpt-4o",
		fakeStartupErr,
	)

	if err != ErrProviderNotConfigured {
		t.Fatalf("expected ErrProviderNotConfigured, got: %v", err)
	}

	// Verify errors.Is works on the returned error.
	if !errors.Is(err, ErrProviderNotConfigured) {
		t.Error("errors.Is should match ErrProviderNotConfigured")
	}

	// Provider and model should be empty (nothing resolved).
	if provider != "" {
		t.Errorf("expected empty provider, got %q", provider)
	}
	if model != "" {
		t.Errorf("expected empty model, got %q", model)
	}
}

func TestRecoverProviderStartup_DaemonMode_ModelError_ReturnsErrProviderNotConfigured(t *testing.T) {
	// Even with a model-not-found error, daemon mode should return
	// ErrProviderNotConfigured (not ErrModelNotAvailable) so the web UI
	// can present a full provider configuration UI.

	t.Setenv("SPROUT_DAEMON", "1")

	w, restore := redirectStdinToPipe(t)
	defer restore()
	_ = w.Close()

	if !isNonInteractive() {
		t.Fatal("precondition failed: stdin must be non-interactive")
	}

	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	// Simulate a model-not-found error.
	modelErr := errors.New("model not found: gpt-999")
	provider, model, err := recoverProviderStartup(
		configManager,
		api.OpenAIClientType,
		"gpt-999",
		modelErr,
	)

	// In daemon mode, model errors also return ErrProviderNotConfigured
	// (the web UI handles both model selection and provider setup).
	if err != ErrProviderNotConfigured {
		t.Fatalf("expected ErrProviderNotConfigured for model error in daemon mode, got: %v", err)
	}

	if provider != "" {
		t.Errorf("expected empty provider, got %q", provider)
	}
	if model != "" {
		t.Errorf("expected empty model, got %q", model)
	}
}

func TestRecoverProviderStartup_DaemonMode_SSHDaemonEnv(t *testing.T) {
	// Verify the alternative SSH daemon detection path (BROWSER=none) also
	// triggers ErrProviderNotConfigured.

	t.Setenv("BROWSER", "none")

	w, restore := redirectStdinToPipe(t)
	defer restore()
	_ = w.Close()

	if !isNonInteractive() {
		t.Fatal("precondition failed: stdin must be non-interactive")
	}
	if !isSSHDaemon() {
		t.Fatal("precondition failed: isSSHDaemon() must be true when BROWSER=none")
	}

	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	fakeErr := errors.New("API key invalid")
	provider, model, err := recoverProviderStartup(
		configManager,
		api.OpenRouterClientType,
		"anthropic/claude-3",
		fakeErr,
	)

	if err != ErrProviderNotConfigured {
		t.Fatalf("expected ErrProviderNotConfigured in SSH daemon mode, got: %v", err)
	}
	if provider != "" || model != "" {
		t.Errorf("expected empty provider and model, got provider=%q model=%q", provider, model)
	}
}

// ---------------------------------------------------------------------------
// TestSSHDaemon_UnsetFlipsDetection
//
// Documents the env-var lifecycle contract enforced by the daemon-mode
// launch path in cmd/agent_modes.go and cmd/agent_command.go:
//
//   - When --daemon is passed, the process sets SPROUT_DAEMON=1 so that
//     isSSHDaemon() returns true during provider resolution.
//   - When the daemon process exits, a `defer os.Unsetenv("SPROUT_DAEMON")`
//     removes the flag from the process environment so it does NOT leak
//     to subprocesses the user explicitly spawns afterward (or to tests
//     sharing the same process).
//
// This test validates the consumer end of that contract: isSSHDaemon()
// must flip from true to false the instant SPROUT_DAEMON is unset.
// If a future regression leaves the env var set (e.g., remove the defer,
// forget to call os.Unsetenv), downstream code would silently take the
// daemon code path in non-daemon processes.
// ---------------------------------------------------------------------------

func TestSSHDaemon_UnsetFlipsDetection(t *testing.T) {
	// Set SPROUT_DAEMON and verify isSSHDaemon() picks it up.
	t.Setenv("SPROUT_DAEMON", "1")
	if !isSSHDaemon() {
		t.Fatal("precondition: isSSHDaemon() must be true when SPROUT_DAEMON=1")
	}

	// Simulate the defer os.Unsetenv("SPROUT_DAEMON") that cmd/agent_modes.go
	// and cmd/agent_command.go register when the daemon exits. After this,
	// isSSHDaemon() must return false — otherwise the flag has leaked to
	// code paths that should treat this as a non-daemon process.
	os.Unsetenv("SPROUT_DAEMON")
	if isSSHDaemon() {
		t.Fatal("isSSHDaemon() must return false after SPROUT_DAEMON is unset; the env var leaked")
	}

	// t.Setenv auto-restores SPROUT_DAEMON=1 on test cleanup, but that
	// restoration is harmless because the test has already finished.
}
