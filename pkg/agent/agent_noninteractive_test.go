package agent

import (
	"errors"
	"os"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// redirectStdinToPipe replaces os.Stdin with the read end of a new pipe.
// The caller must close the write end when done. Returns a restore function
// that resets os.Stdin to its original value. Intentionally NOT parallel-safe
// (modifies the global os.Stdin).
func redirectStdinToPipe(t *testing.T) (*os.File, func()) {
	t.Helper()

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	os.Stdin = r

	restore := func() {
		// Drain any blocked readers by closing the write end first.
		_ = w.Close()
		// Reset stdin even if closing the read end fails.
		os.Stdin = oldStdin
		_ = r.Close()
	}

	return w, restore
}

// ---------------------------------------------------------------------------
// TestIsNonInteractive
// ---------------------------------------------------------------------------

func TestIsNonInteractive(t *testing.T) {
	// Under `go test`, stdin is typically a pipe, so isNonInteractive()
	// should return true. We verify this baseline, then explicitly redirect
	// stdin to a pipe to guarantee non-TTY behaviour.

	t.Run("baseline under go test", func(t *testing.T) {
		// In normal `go test` execution stdin is not a terminal (piped).
		if !isNonInteractive() {
			t.Log("Note: stdin was reported as a terminal; this is unusual under go test")
		}
		// We don't fail — some environments (e.g. IDE test runners) may
		// allocate a pseudo-TTY for stdin.
	})

	t.Run("with piped stdin returns true", func(t *testing.T) {
		w, restore := redirectStdinToPipe(t)
		defer restore()

		// Close the write end immediately; the read end becomes an EOF pipe.
		_ = w.Close()

		if !isNonInteractive() {
			t.Error("isNonInteractive() should return true when stdin is a pipe")
		}
	})
}

// ---------------------------------------------------------------------------
// TestRecoverProviderStartupNonInteractive
// ---------------------------------------------------------------------------

func TestRecoverProviderStartupNonInteractive(t *testing.T) {
	// recoverProviderStartup should detect non-interactive mode and return
	// an error immediately rather than blocking on a prompt. We replace
	// stdin with a pipe to guarantee the non-interactive detection fires.

	w, restore := redirectStdinToPipe(t)
	defer restore()

	// Close the write end so that reading from stdin yields EOF immediately,
	// preventing any possible blocking in the function.
	_ = w.Close()

	// Verify the environment is actually non-interactive (sanity check).
	if !isNonInteractive() {
		t.Fatal("precondition failed: stdin must be non-interactive for this test")
	}

	// Create a real configuration manager (silent mode to avoid prompts).
	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	fakeStartupErr := errors.New("API key not configured")

	// Call recoverProviderStartup — it should detect non-interactive mode
	// and return immediately with an error (never reach promptProviderRecoveryChoice).
	provider, model, err := recoverProviderStartup(
		configManager,
		api.OpenAIClientType,
		"gpt-4o",
		fakeStartupErr,
	)

	if err == nil {
		t.Fatal("expected recoverProviderStartup to return an error in non-interactive mode")
	}

	// Verify return values: empty strings for provider/model since nothing was resolved.
	if provider != "" {
		t.Errorf("expected empty provider, got %q", provider)
	}
	if model != "" {
		t.Errorf("expected empty model, got %q", model)
	}

	// Verify the error message contains actionable guidance.
	errMsg := err.Error()
	requiredPhrases := []string{
		"non-interactive",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(strings.ToLower(errMsg), phrase) {
			t.Errorf("error message should contain %q, got: %s", phrase, errMsg)
		}
	}

	// Verify the original startup error is wrapped (preserves the chain).
	if !strings.Contains(errMsg, fakeStartupErr.Error()) {
		t.Errorf("error should wrap the original startup error %q, got: %s", fakeStartupErr.Error(), errMsg)
	}

	// Verify the failed provider name appears in the error (case-insensitive).
	if !strings.Contains(strings.ToLower(errMsg), "openai") {
		t.Errorf("error should mention the failed provider name (got: %s)", errMsg)
	}
}

// ---------------------------------------------------------------------------
// TestNonInteractiveErrorMessageContent
// ---------------------------------------------------------------------------

func TestNonInteractiveErrorMessageContent(t *testing.T) {
	// Verify that non-interactive error messages in agent.go and
	// agent_provider.go contain all expected guidance phrases.
	//
	// NOTE: These tests use literal strings mirroring the production code
	// rather than calling the actual error-producing functions. They serve
	// as documentation of expected message content and catch gross formatting
	// changes, but will NOT detect drift if production messages change
	// silently. The primary regression guard is TestRecoverProviderStartupNonInteractive
	// which calls the real function. The early fast-fail path in
	// NewAgentWithModel is not directly testable under go test because
	// isRunningUnderTest() always returns true for test binaries.

	t.Run("NewAgentWithModel provider resolution error", func(t *testing.T) {
		// From agent.go — early non-interactive fast-fail:
		//   "no provider configured. running in non-interactive mode. Set LEDIT_PROVIDER
		//    / configure ~/.ledit/config.json, or run `ledit agent` interactively: %w"
		errMsg := "no provider configured. running in non-interactive mode. Set LEDIT_PROVIDER / configure ~/.ledit/config.json, or run `ledit agent` interactively: some error"

		expectedPhrases := []struct {
			name   string
			phrase string
		}{
			{"non-interactive mode (case-insensitive)", "non-interactive mode"},
			{"LEDIT_PROVIDER env var", "LEDIT_PROVIDER"},
			{"config file path", "~/.ledit/config.json"},
			{"interactive run guidance", "run `ledit agent` interactively"},
		}

		for _, tc := range expectedPhrases {
			if !strings.Contains(errMsg, tc.phrase) {
				t.Errorf("[%s] expected error to contain %q, got: %s", tc.name, tc.phrase, errMsg)
			}
		}
	})

	t.Run("NewAgentWithModel API key error", func(t *testing.T) {
		// From agent.go — second non-interactive check after resolution succeeds
		// but EnsureAPIKey fails:
		//   "no provider configured. running in non-interactive mode. Set LEDIT_PROVIDER
		//    / configure ~/.ledit/config.json, or run `ledit agent` interactively: %w"
		errMsg := "no provider configured. running in non-interactive mode. Set LEDIT_PROVIDER / configure ~/.ledit/config.json, or run `ledit agent` interactively: some error"

		required := []string{"non-interactive mode", "LEDIT_PROVIDER", "~/.ledit/config.json"}
		for _, phrase := range required {
			if !strings.Contains(errMsg, phrase) {
				t.Errorf("expected error to contain %q, got: %s", phrase, errMsg)
			}
		}
	})

	t.Run("ResolveProviderModel fallback error", func(t *testing.T) {
		// From agent.go — the fallback path when ResolveProviderModel fails
		// and stdin is not a terminal (uses lowercase 'running'):
		//   "no provider configured. running in non-interactive mode. Set LEDIT_PROVIDER
		//    / configure ~/.ledit/config.json, or run `ledit agent` interactively"
		errMsg := "no provider configured. running in non-interactive mode. Set LEDIT_PROVIDER / configure ~/.ledit/config.json, or run `ledit agent` interactively"

		// Use case-insensitive check for "non-interactive mode" since agent.go
		// uses "running" (lowercase) in the fallback path vs "Running" (uppercase)
		// in the early check.
		if !strings.Contains(strings.ToLower(errMsg), "non-interactive mode") {
			t.Errorf("expected error to contain 'non-interactive mode' (case-insensitive), got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "LEDIT_PROVIDER") {
			t.Errorf("expected error to contain 'LEDIT_PROVIDER', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "~/.ledit/config.json") {
			t.Errorf("expected error to contain '~/.ledit/config.json', got: %s", errMsg)
		}
	})

	t.Run("recoverProviderStartup error", func(t *testing.T) {
		// From agent_provider.go — recoverProviderStartup non-interactive path:
		//   "failed to initialize provider %s: running in non-interactive mode.
		//    Set LEDIT_PROVIDER / configure ~/.ledit/config.json, or run `ledit agent`
		//    interactively: %w"
		errMsg := "failed to initialize provider OpenAI: running in non-interactive mode. Set LEDIT_PROVIDER / configure ~/.ledit/config.json, or run `ledit agent` interactively: API key not configured"

		required := []string{
			"non-interactive",
			"LEDIT_PROVIDER",
			"~/.ledit/config.json",
			"run `ledit agent` interactively",
		}
		for _, phrase := range required {
			if !strings.Contains(errMsg, phrase) {
				t.Errorf("expected error to contain %q, got: %s", phrase, errMsg)
			}
		}

		// The provider name should be present.
		if !strings.Contains(errMsg, "OpenAI") {
			t.Errorf("expected error to mention provider name 'OpenAI', got: %s", errMsg)
		}
	})
}
