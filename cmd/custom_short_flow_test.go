package cmd

import (
	"bufio"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// runCustomModelAddKnown needs to be exported so tests in the same package
// can drive it directly with synthetic readers.
func TestRunCustomModelAddKnown_NoAuthRequired(t *testing.T) {
	// A "no-auth" provider like ollama shouldn't prompt for credentials.
	known := configuration.KnownProviderInfo{
		Source:         "custom",
		Name:           "my-local",
		DisplayName:    "my-local",
		EnvVar:         "",
		RequiresAPIKey: false,
		Endpoint:       "http://localhost:11434/v1",
		DefaultModel:   "llama3",
	}

	// Pass an empty reader; the function should print a message and return
	// without reading any input.
	reader := bufio.NewReader(strings.NewReader(""))
	if err := runCustomModelAddKnown(reader, known); err != nil {
		t.Errorf("runCustomModelAddKnown returned error: %v", err)
	}
}

func TestRunCustomModelAddKnown_UserDeclines(t *testing.T) {
	// User says "n" to "Set the API key now?"
	known := configuration.KnownProviderInfo{
		Source:         "custom",
		Name:           "ai-worker",
		DisplayName:    "ai-worker",
		EnvVar:         "AI_WORKER_API_KEY",
		RequiresAPIKey: true,
		Endpoint:       "http://192.168.1.134:8033/v1/chat/completions",
		DefaultModel:   "qwen3.6-27b",
		ContextSize:    200000,
	}

	// Make sure env var is unset (we explicitly want the "no credentials
	// configured" path to fire)
	t.Setenv("AI_WORKER_API_KEY", "")

	// Configure test-only credential backend so this test doesn't touch
	// the real keyring/file store.
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	reader := bufio.NewReader(strings.NewReader("n\n"))
	if err := runCustomModelAddKnown(reader, known); err != nil {
		t.Errorf("runCustomModelAddKnown returned error: %v", err)
	}
}

func TestRunCustomModelAddKnown_EmptyKey(t *testing.T) {
	// User says "y" to set, then submits an empty key. Should be a no-op.
	known := configuration.KnownProviderInfo{
		Source:         "custom",
		Name:           "ai-worker",
		DisplayName:    "ai-worker",
		EnvVar:         "AI_WORKER_API_KEY",
		RequiresAPIKey: true,
		Endpoint:       "http://192.168.1.134:8033/v1/chat/completions",
		DefaultModel:   "qwen3.6-27b",
	}

	t.Setenv("AI_WORKER_API_KEY", "")
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// "y" + empty key — should not store anything.
	reader := bufio.NewReader(strings.NewReader("y\n\n"))
	if err := runCustomModelAddKnown(reader, known); err != nil {
		t.Errorf("runCustomModelAddKnown returned error: %v", err)
	}

	// Verify no credential was stored.
	resolved, err := credentials.ResolveProvider("ai-worker")
	if err == nil && strings.TrimSpace(resolved.Value) != "" {
		t.Errorf("Expected no credential for ai-worker, got %q", resolved.Value)
	}
}
func TestRunCustomModelAddKnown_UserAcceptsAndStoresKey(t *testing.T) {
	// User says "y" to "Set the API key now?" and provides a non-empty key.
	// The credential should land in the active backend so `/provider
	// <name>` will resolve it on the next try.
	known := configuration.KnownProviderInfo{
		Source:         "custom",
		Name:           "ai-worker",
		DisplayName:    "ai-worker",
		EnvVar:         "AI_WORKER_API_KEY",
		RequiresAPIKey: true,
		Endpoint:       "http://192.168.1.134:8033/v1/chat/completions",
		DefaultModel:   "qwen3.6-27b",
		ContextSize:    200000,
	}

	t.Setenv("AI_WORKER_API_KEY", "")
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// "y" + a real-looking key. The wizard should:
	//   1. Print "Set the API key for ai-worker now via the credential backend? [Y/n]"
	//   2. Read "y\n"
	//   3. Print "API key (will be stored; or set AI_WORKER_API_KEY):"
	//   4. Read "sk-test-12345\n"
	//   5. Store via credentials.SetToActiveBackend
	//   6. Print "Stored credential for ai-worker"
	reader := bufio.NewReader(strings.NewReader("y\nsk-test-12345\n"))
	if err := runCustomModelAddKnown(reader, known); err != nil {
		t.Fatalf("runCustomModelAddKnown returned error: %v", err)
	}

	// Verify the credential was stored.
	resolved, err := credentials.ResolveProvider("ai-worker")
	if err != nil {
		t.Fatalf("ResolveProvider failed: %v", err)
	}
	if strings.TrimSpace(resolved.Value) != "sk-test-12345" {
		t.Errorf("Expected stored credential sk-test-12345, got %q", resolved.Value)
	}
}
