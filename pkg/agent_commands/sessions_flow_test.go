package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// sessionExportTestHelper manages the lifecycle of a test setup for session export tests.
// It creates a temp directory, writes a scoped session file, and changes the working directory
// so that ListSessionsWithTimestamps (used by ExecuteSessionExport) can find the session.
type sessionExportTestHelper struct {
	stateDir   string
	workingDir string
	exportPath string
}

// newSessionExportTestHelper sets up a temp directory with a scoped session file
// and chdirs into the session's working directory so ListSessionsWithTimestamps
// can find it. Cleanup is registered via t.Cleanup automatically.
func newSessionExportTestHelper(t *testing.T, sessionID string, state *agent.ConversationState) *sessionExportTestHelper {
	t.Helper()

	stateDir := t.TempDir()

	// Override the state directory for the persistence layer.
	restoreDir := agent.SetStateDirFuncForTesting(func() (string, error) { return stateDir, nil })

	// Create a fake working directory so scoped session lookup works.
	workingDir := filepath.Join(stateDir, "project")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("failed to create working dir: %v", err)
	}

	// Ensure the state records the correct working directory.
	state.WorkingDirectory = workingDir

	// Write the scoped session file.
	if err := agent.WriteTestSessionFile(stateDir, sessionID, workingDir, state); err != nil {
		t.Fatalf("failed to write test session file: %v", err)
	}

	// Change working directory to the session's working directory.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	t.Cleanup(func() {
		os.Chdir(origDir)
		restoreDir()
	})

	return &sessionExportTestHelper{
		stateDir:   stateDir,
		workingDir: workingDir,
		exportPath: filepath.Join(stateDir, "export_output.json"),
	}
}

// readExportedFile reads the content and permissions of the exported file.
func (h *sessionExportTestHelper) readExportedFile(t *testing.T) ([]byte, os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(h.exportPath)
	if err != nil {
		t.Fatalf("failed to read exported file: %v", err)
	}
	info, err := os.Stat(h.exportPath)
	if err != nil {
		t.Fatalf("failed to stat exported file: %v", err)
	}
	return data, info.Mode().Perm()
}

// --- Test Cases ---

func TestExecuteSessionExport_RedactsAPISecrets(t *testing.T) {
	sensitiveContent := `Here is my OpenAI API key: sk-abc123def456ghi789jklmnopqrs
And a bearer token: Authorization: Bearer ghp_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789AbCd
Also a JSON credential: {"api_key": "sk-live0000000000000000000000000000000000000000000"}
Normal text: The quick brown fox jumps over the lazy dog. File at /tmp/test.txt was created.`

	state := &agent.ConversationState{
		SessionID:   "export-redact-test",
		Name:        "test session with secrets",
		LastUpdated: time.Now(),
		Messages: []api.Message{
			{Role: "user", Content: sensitiveContent},
		},
		TotalCost:    0.05,
		TotalTokens:  1000,
		PromptTokens: 600,
	}

	h := newSessionExportTestHelper(t, "export-redact-test", state)

	flow := &SessionsFlow{}
	result, err := flow.ExecuteSessionExport([]string{"1", h.exportPath})
	if err != nil {
		t.Fatalf("ExecuteSessionExport failed: %v", err)
	}
	if !strings.Contains(result, "[OK]") {
		t.Fatalf("expected OK result, got: %s", result)
	}

	exported, perms := h.readExportedFile(t)
	exportedStr := string(exported)

	// Verify sensitive patterns are redacted.
	assertNotContains(t, exportedStr, "sk-abc123def456ghi789jklmnopqrs",
		"OpenAI API key should be redacted")
	assertNotContains(t, exportedStr, "ghp_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789AbCd",
		"GitHub PAT should be redacted")
	assertNotContains(t, exportedStr, "sk-live0000000000000000000000000000000000000000000",
		"sk-live API key should be redacted")
	assertContains(t, exportedStr, "[REDACTED]",
		"exported JSON should contain at least one [REDACTED] marker")

	// Verify normal content is preserved.
	assertContains(t, exportedStr, "quick brown fox",
		"normal text should be preserved in export")
	assertContains(t, exportedStr, "/tmp/test.txt",
		"file paths in normal text should be preserved")
	assertContains(t, exportedStr, "export-redact-test",
		"session ID should be preserved")

	// Verify file permissions are 0600.
	if perms != 0o600 {
		t.Errorf("expected file permissions 0600, got %04o", perms)
	}
}

func TestExecuteSessionExport_RedactsBearerTokens(t *testing.T) {
	state := &agent.ConversationState{
		SessionID:   "bearer-test",
		Name:        "bearer token session",
		LastUpdated: time.Now(),
		Messages: []api.Message{
			{
				Role:    "assistant",
				Content: `curl -H "Authorization: Bearer sk-abc123def456789012345678901234567890ab" https://api.example.com/v1/complete`,
			},
		},
	}

	h := newSessionExportTestHelper(t, "bearer-test", state)

	flow := &SessionsFlow{}
	_, err := flow.ExecuteSessionExport([]string{"1", h.exportPath})
	if err != nil {
		t.Fatalf("ExecuteSessionExport failed: %v", err)
	}

	exported, _ := h.readExportedFile(t)
	exportedStr := string(exported)

	assertNotContains(t, exportedStr, "sk-abc123def456789012345678901234567890ab",
		"Bearer token value should be redacted")
	assertContains(t, exportedStr, "[REDACTED]",
		"Bearer token should be replaced with [REDACTED]")
	assertContains(t, exportedStr, "api.example.com",
		"API URL should be preserved")
}

func TestExecuteSessionExport_FilePermissionsAre0600(t *testing.T) {
	state := &agent.ConversationState{
		SessionID:   "perms-test",
		Name:        "permissions test",
		LastUpdated: time.Now(),
		Messages: []api.Message{
			{Role: "user", Content: "Hello world"},
		},
	}

	h := newSessionExportTestHelper(t, "perms-test", state)

	flow := &SessionsFlow{}
	_, err := flow.ExecuteSessionExport([]string{"1", h.exportPath})
	if err != nil {
		t.Fatalf("ExecuteSessionExport failed: %v", err)
	}

	_, perms := h.readExportedFile(t)
	if perms != 0o600 {
		t.Errorf("expected file permissions 0600, got %04o", perms)
	}
}

func TestExecuteSessionExport_PreservesNonSensitiveContent(t *testing.T) {
	safeContent := "Please help me write a Go function that sorts a list of integers in ascending order. " +
		"The function signature should be: func SortInts(nums []int) []int. " +
		"Use the standard library sort package. Return the sorted slice."

	state := &agent.ConversationState{
		SessionID:   "safe-content-test",
		Name:        "safe content session",
		LastUpdated: time.Now(),
		Messages: []api.Message{
			{Role: "user", Content: safeContent},
			{
				Role: "assistant",
				Content: "Here is the function:\n```go\nfunc SortInts(nums []int) []int {\n" +
					"    sort.Ints(nums)\n    return nums\n}\n```\n\nThis uses the sort package.",
			},
		},
		TotalTokens: 500,
	}

	h := newSessionExportTestHelper(t, "safe-content-test", state)

	flow := &SessionsFlow{}
	_, err := flow.ExecuteSessionExport([]string{"1", h.exportPath})
	if err != nil {
		t.Fatalf("ExecuteSessionExport failed: %v", err)
	}

	exported, _ := h.readExportedFile(t)
	exportedStr := string(exported)

	// All content should be preserved since nothing is sensitive.
	assertContains(t, exportedStr, "SortInts",
		"function name should be preserved")
	assertContains(t, exportedStr, "sort package",
		"normal text should be preserved")
	assertContains(t, exportedStr, "Go function that sorts",
		"user message should be preserved")
	assertContains(t, exportedStr, "safe-content-test",
		"session ID should be preserved")

	// Should NOT contain any [REDACTED] markers.
	if strings.Contains(exportedStr, "[REDACTED]") {
		t.Errorf("non-sensitive export should not contain [REDACTED] markers, but got:\n%s", exportedStr)
	}
}

func TestExecuteSessionExport_RedactsMixedSensitiveAndNormalMessages(t *testing.T) {
	sensitiveMsg := `Checking my env vars:
OPENAI_API_KEY=sk-test1234567890abcdefghijklmnop
GITHUB_TOKEN=ghpat_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij
PATH=/usr/local/bin:/usr/bin
LEDIT_DEBUG=true
Normal stuff: refactored the auth module, all tests passing.`

	state := &agent.ConversationState{
		SessionID:   "mixed-content-test",
		Name:        "mixed sensitive content",
		LastUpdated: time.Now(),
		Messages: []api.Message{
			{Role: "user", Content: sensitiveMsg},
			{Role: "assistant", Content: "Great! The refactoring looks clean. Here's a summary of changes."},
		},
		TotalCost:        0.03,
		TotalTokens:      2500,
		PromptTokens:     1500,
		CompletionTokens: 1000,
	}

	h := newSessionExportTestHelper(t, "mixed-content-test", state)

	flow := &SessionsFlow{}
	_, err := flow.ExecuteSessionExport([]string{"1", h.exportPath})
	if err != nil {
		t.Fatalf("ExecuteSessionExport failed: %v", err)
	}

	exported, _ := h.readExportedFile(t)
	exportedStr := string(exported)

	// Sensitive values must be redacted.
	assertNotContains(t, exportedStr, "sk-test1234567890abcdefghijklmnop",
		"OpenAI key should be redacted")
	assertNotContains(t, exportedStr, "ghpat_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
		"GitHub PAT should be redacted")

	// Non-sensitive env vars should be preserved.
	assertContains(t, exportedStr, "/usr/local/bin",
		"PATH values should be preserved")
	assertContains(t, exportedStr, "LEDIT_DEBUG=true",
		"LEDIT_ prefixed values should be preserved")

	// Normal content preserved.
	assertContains(t, exportedStr, "refactored the auth module",
		"non-sensitive text should be preserved")
	assertContains(t, exportedStr, "summary of changes",
		"assistant response should be preserved")

	// Metadata preserved.
	assertContains(t, exportedStr, "mixed-content-test",
		"session ID should be preserved")
}

func TestExecuteSessionExport_ValidJSONOutput(t *testing.T) {
	state := &agent.ConversationState{
		SessionID:   "json-valid-test",
		Name:        "json validation test",
		LastUpdated: time.Now(),
		Messages: []api.Message{
			{Role: "user", Content: "test message"},
		},
		TotalTokens: 100,
	}

	h := newSessionExportTestHelper(t, "json-valid-test", state)

	flow := &SessionsFlow{}
	_, err := flow.ExecuteSessionExport([]string{"1", h.exportPath})
	if err != nil {
		t.Fatalf("ExecuteSessionExport failed: %v", err)
	}

	exported, _ := h.readExportedFile(t)

	// Verify the export is valid JSON.
	var parsed map[string]interface{}
	if err := json.Unmarshal(exported, &parsed); err != nil {
		t.Fatalf("exported file is not valid JSON: %v\nContent:\n%s", err, string(exported))
	}

	// Verify key fields exist.
	if _, ok := parsed["session_id"]; !ok {
		t.Error("exported JSON should contain 'session_id' field")
	}
	if _, ok := parsed["name"]; !ok {
		t.Error("exported JSON should contain 'name' field")
	}
	if _, ok := parsed["messages"]; !ok {
		t.Error("exported JSON should contain 'messages' field")
	}
}

func TestExecuteSessionExport_RedactsAcrossMultipleMessages(t *testing.T) {
	state := &agent.ConversationState{
		SessionID:   "multi-msg-test",
		Name:        "multi message secrets",
		LastUpdated: time.Now(),
		Messages: []api.Message{
			{Role: "user", Content: "Here's my key: sk-aaaabbbbccccddddaaaabbbbccccdddd12345678"},
			{Role: "assistant", Content: "Let me store that. Also, the auth token is sk-zzzzyyyyxxxxwwwwZZZZYYYYXXXXWWWW"},
			{Role: "user", Content: "Great. Also my GitHub token: ghp_1234567890abcdefghijklmnopqrstuvwxyz"},
		},
	}

	h := newSessionExportTestHelper(t, "multi-msg-test", state)

	flow := &SessionsFlow{}
	_, err := flow.ExecuteSessionExport([]string{"1", h.exportPath})
	if err != nil {
		t.Fatalf("ExecuteSessionExport failed: %v", err)
	}

	exported, _ := h.readExportedFile(t)
	exportedStr := string(exported)

	// All secret patterns across all messages should be redacted.
	secrets := []string{
		"sk-aaaabbbbccccddddaaaabbbbccccdddd12345678",
		"sk-zzzzyyyyxxxxwwwwZZZZYYYYXXXXWWWW",
		"ghp_1234567890abcdefghijklmnopqrstuvwxyz",
	}
	for _, secret := range secrets {
		assertNotContains(t, exportedStr, secret,
			"secret across messages should be redacted: "+secret[:15]+"...")
	}
}

// TestExecuteSessionExport_UsageErrors verifies argument validation.
func TestExecuteSessionExport_UsageErrors(t *testing.T) {
	flow := &SessionsFlow{}

	t.Run("no args returns usage", func(t *testing.T) {
		result, err := flow.ExecuteSessionExport([]string{})
		if err != nil {
			t.Fatalf("expected nil error for missing args, got: %v", err)
		}
		if !strings.Contains(result, "Usage:") {
			t.Errorf("expected usage message, got: %s", result)
		}
	})

	t.Run("one arg returns usage", func(t *testing.T) {
		result, err := flow.ExecuteSessionExport([]string{"1"})
		if err != nil {
			t.Fatalf("expected nil error for one arg, got: %v", err)
		}
		if !strings.Contains(result, "Usage:") {
			t.Errorf("expected usage message, got: %s", result)
		}
	})
}

// TestExecuteSessionExport_SessionNotFound verifies error when session doesn't exist.
func TestExecuteSessionExport_SessionNotFound(t *testing.T) {
	stateDir := t.TempDir()
	restoreDir := agent.SetStateDirFuncForTesting(func() (string, error) { return stateDir, nil })

	workingDir := filepath.Join(stateDir, "empty-project")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	origDir, _ := os.Getwd()
	os.Chdir(workingDir)

	t.Cleanup(func() {
		os.Chdir(origDir)
		restoreDir()
	})

	flow := &SessionsFlow{}
	_, err := flow.ExecuteSessionExport([]string{"1", "/tmp/should-not-exist.json"})
	if err == nil {
		t.Fatal("expected error for non-existent session, got nil")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "failed to list") {
		t.Errorf("expected session-not-found error, got: %v", err)
	}
}

// TestExecuteSessionExport_RedactsSlackTokens verifies xoxb-pattern tokens are redacted.
func TestExecuteSessionExport_RedactsSlackTokens(t *testing.T) {
	state := &agent.ConversationState{
		SessionID:   "slack-test",
		Name:        "slack token session",
		LastUpdated: time.Now(),
		Messages: []api.Message{
			{
				Role:    "user",
				Content: "My Slack bot token is xoxb-123456789-ABCDEFGHIJKLMNOP-QRSTUVWXYZ and app token is xoxa-123456789012-AbCdEfGhIjKlMnOpQrS",
			},
		},
	}

	h := newSessionExportTestHelper(t, "slack-test", state)

	flow := &SessionsFlow{}
	_, err := flow.ExecuteSessionExport([]string{"1", h.exportPath})
	if err != nil {
		t.Fatalf("ExecuteSessionExport failed: %v", err)
	}

	exported, _ := h.readExportedFile(t)
	exportedStr := string(exported)

	assertNotContains(t, exportedStr, "xoxb-123456789-ABCDEFGHIJKLMNOP-QRSTUVWXYZ",
		"Slack bot token should be redacted")
	assertNotContains(t, exportedStr, "xoxa-123456789012-AbCdEfGhIjKlMnOpQrS",
		"Slack app token should be redacted")
	assertContains(t, exportedStr, "[REDACTED]", "Slack tokens should be replaced with [REDACTED]")
}

// --- Assertion Helpers ---

func assertContains(t *testing.T, content, substr, msg string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("%s: expected content to contain %q", msg, substr)
	}
}

func assertNotContains(t *testing.T, content, substr, msg string) {
	t.Helper()
	if strings.Contains(content, substr) {
		t.Errorf("%s: expected content NOT to contain %q", msg, substr)
	}
}
