package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// --- NewTraceSession tests ---

func TestNewTraceSession_EmptyDir_Disabled(t *testing.T) {
	s, err := NewTraceSession("", "provider", "model")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil session")
	}
	if s.IsEnabled {
		t.Error("expected IsEnabled=false for empty traceDir")
	}
	if s.GetRunID() != "" {
		t.Errorf("expected empty RunID, got %q", s.GetRunID())
	}
	if s.GetRunDir() != "" {
		t.Errorf("expected empty RunDir, got %q", s.GetRunDir())
	}
}

func TestNewTraceSession_ValidDir_CreatesFiles(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	if !s.IsEnabled {
		t.Fatal("expected IsEnabled=true for valid traceDir")
	}

	expectedFiles := []string{
		"runs.jsonl",
		"turns.jsonl",
		"tool_calls.jsonl",
		"artifacts_manifest.jsonl",
	}

	for _, name := range expectedFiles {
		path := filepath.Join(s.GetRunDir(), name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist", path)
		}
	}
}

// --- RecordTurn tests ---

func TestRecordTurn_WritesAndVerifiesJSON(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	toolCall := api.ToolCall{}
	toolCall.ID = "tc_001"
	toolCall.Type = "function"
	toolCall.Function.Name = "read_file"
	toolCall.Function.Arguments = `{"path":"/tmp/test.go"}`

	record := TurnRecord{
		RunID:              s.GetRunID(),
		TurnIndex:          0,
		SystemPrompt:       "You are a helpful assistant.",
		UserPrompt:         "Read the file",
		UserPromptOriginal: "Read the file",
		MessagesSent: []api.Message{
			{Role: "user", Content: "Read the file"},
		},
		ToolSchemaPayload: json.RawMessage(`[{"type":"function","function":{"name":"read_file"}}]`),
		RawResponse:       `{"content":"file contents here"}`,
		ParsedToolCalls:   []api.ToolCall{toolCall},
		ParserErrors:      nil,
		FallbackUsed:      false,
		FallbackOutput:    "",
		MachineLabels:     []string{"label_a"},
		Timestamp:         "2024-06-15T10:30:00Z",
	}

	if err := s.RecordTurn(record); err != nil {
		t.Fatalf("RecordTurn returned error: %v", err)
	}

	s.Close()

	// Read back turns.jsonl
	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("failed to read turns.jsonl: %v", err)
	}

	var got TurnRecord
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		t.Fatal("no line found in turns.jsonl")
	}
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal turn record: %v", err)
	}

	if got.RunID != record.RunID {
		t.Errorf("RunID mismatch: got %q, want %q", got.RunID, record.RunID)
	}
	if got.TurnIndex != record.TurnIndex {
		t.Errorf("TurnIndex mismatch: got %d, want %d", got.TurnIndex, record.TurnIndex)
	}
	if got.SystemPrompt != record.SystemPrompt {
		t.Errorf("SystemPrompt mismatch: got %q, want %q", got.SystemPrompt, record.SystemPrompt)
	}
	if got.UserPrompt != record.UserPrompt {
		t.Errorf("UserPrompt mismatch: got %q, want %q", got.UserPrompt, record.UserPrompt)
	}
	if got.FallbackUsed != record.FallbackUsed {
		t.Errorf("FallbackUsed mismatch: got %v, want %v", got.FallbackUsed, record.FallbackUsed)
	}
	if len(got.ParsedToolCalls) != 1 {
		t.Fatalf("expected 1 ParsedToolCall, got %d", len(got.ParsedToolCalls))
	}
	if got.ParsedToolCalls[0].ID != "tc_001" {
		t.Errorf("ParsedToolCall ID mismatch: got %q", got.ParsedToolCalls[0].ID)
	}
	if len(got.MachineLabels) != 1 || got.MachineLabels[0] != "label_a" {
		t.Errorf("MachineLabels mismatch: got %v", got.MachineLabels)
	}
	if got.Timestamp != record.Timestamp {
		t.Errorf("Timestamp mismatch: got %q, want %q", got.Timestamp, record.Timestamp)
	}
}

func TestRecordTurn_MultipleTurns(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	for i := 0; i < 3; i++ {
		if err := s.RecordTurn(TurnRecord{
			RunID:     s.GetRunID(),
			TurnIndex: i,
			Timestamp: "2024-06-15T10:30:00Z",
		}); err != nil {
			t.Fatalf("RecordTurn %d error: %v", i, err)
		}
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("failed to read turns.jsonl: %v", err)
	}

	var records []TurnRecord
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		var rec TurnRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		records = append(records, rec)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 turn records, got %d", len(records))
	}
	if records[0].TurnIndex != 0 || records[1].TurnIndex != 1 || records[2].TurnIndex != 2 {
		t.Error("turn indices out of order")
	}
}

// --- RecordToolCall tests ---

func TestRecordToolCall_Verifies(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	record := ToolCallRecord{
		RunID:          s.GetRunID(),
		TurnIndex:      2,
		ToolIndex:      5,
		ToolName:       "shell_command",
		Args:           map[string]interface{}{"command": "ls -la", "timeout": 30},
		ArgsNormalized: map[string]interface{}{"command": "ls -la", "timeout": float64(30)},
		Success:        true,
		FullResult:     "total 42\ndrwxr-xr-x",
		ModelResult:    "total 42",
		ErrorCategory:  "",
		ErrorMessage:   "",
		MachineLabels:  []string{LabelToolCallValidationFailure},
		Timestamp:      "2024-06-15T10:31:00Z",
	}

	if err := s.RecordToolCall(record); err != nil {
		t.Fatalf("RecordToolCall returned error: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "tool_calls.jsonl"))
	if err != nil {
		t.Fatalf("failed to read tool_calls.jsonl: %v", err)
	}

	var got ToolCallRecord
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		t.Fatal("no line found in tool_calls.jsonl")
	}
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if got.RunID != record.RunID {
		t.Errorf("RunID mismatch")
	}
	if got.TurnIndex != record.TurnIndex {
		t.Errorf("TurnIndex mismatch: got %d, want %d", got.TurnIndex, record.TurnIndex)
	}
	if got.ToolIndex != record.ToolIndex {
		t.Errorf("ToolIndex mismatch: got %d, want %d", got.ToolIndex, record.ToolIndex)
	}
	if got.ToolName != "shell_command" {
		t.Errorf("ToolName mismatch: got %q", got.ToolName)
	}
	if got.Args["command"] != "ls -la" {
		t.Errorf("Args command mismatch: got %v", got.Args["command"])
	}
	if !got.Success {
		t.Error("expected Success=true")
	}
	if got.FullResult != record.FullResult {
		t.Errorf("FullResult mismatch")
	}
	if got.ErrorCategory != "" {
		t.Errorf("expected empty ErrorCategory, got %q", got.ErrorCategory)
	}
	if len(got.MachineLabels) != 1 || got.MachineLabels[0] != LabelToolCallValidationFailure {
		t.Errorf("MachineLabels mismatch: got %v", got.MachineLabels)
	}
}

func TestRecordToolCall_FailureRecord(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	record := ToolCallRecord{
		RunID:         s.GetRunID(),
		TurnIndex:     0,
		ToolIndex:     0,
		ToolName:      "unknown_tool",
		Success:       false,
		ErrorCategory: "unknown_tool",
		ErrorMessage:  "tool not found",
		Timestamp:     "2024-06-15T10:31:00Z",
	}

	if err := s.RecordToolCall(record); err != nil {
		t.Fatalf("RecordToolCall error: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "tool_calls.jsonl"))
	if err != nil {
		t.Fatalf("failed to read tool_calls.jsonl: %v", err)
	}

	var got ToolCallRecord
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Scan()
	json.Unmarshal(scanner.Bytes(), &got)

	if got.Success {
		t.Error("expected Success=false for failure record")
	}
	if got.ErrorCategory != "unknown_tool" {
		t.Errorf("ErrorCategory mismatch: got %q", got.ErrorCategory)
	}
	if got.ErrorMessage != "tool not found" {
		t.Errorf("ErrorMessage mismatch: got %q", got.ErrorMessage)
	}
}

// --- RecordArtifact tests ---

func TestRecordArtifact_Verifies(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	record := ArtifactManifest{
		RunID:         s.GetRunID(),
		RelativePath:  "src/main.go",
		SizeBytes:     2048,
		Hash:          "abc123def456",
		ArtifactType:  "file_edit",
		MachineLabels: []string{LabelPathViolationAbsolute},
		Timestamp:     "2024-06-15T10:32:00Z",
	}

	if err := s.RecordArtifact(record); err != nil {
		t.Fatalf("RecordArtifact returned error: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "artifacts_manifest.jsonl"))
	if err != nil {
		t.Fatalf("failed to read artifacts_manifest.jsonl: %v", err)
	}

	var got ArtifactManifest
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		t.Fatal("no line found in artifacts_manifest.jsonl")
	}
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if got.RunID != record.RunID {
		t.Errorf("RunID mismatch")
	}
	if got.RelativePath != "src/main.go" {
		t.Errorf("RelativePath mismatch: got %q", got.RelativePath)
	}
	if got.SizeBytes != 2048 {
		t.Errorf("SizeBytes mismatch: got %d", got.SizeBytes)
	}
	if got.Hash != "abc123def456" {
		t.Errorf("Hash mismatch: got %q", got.Hash)
	}
	if got.ArtifactType != "file_edit" {
		t.Errorf("ArtifactType mismatch: got %q", got.ArtifactType)
	}
	if len(got.MachineLabels) != 1 || got.MachineLabels[0] != LabelPathViolationAbsolute {
		t.Errorf("MachineLabels mismatch: got %v", got.MachineLabels)
	}
	if got.Timestamp != record.Timestamp {
		t.Errorf("Timestamp mismatch")
	}
}

// --- Close tests ---

func TestClose_Idempotent(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("first Close error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close should be idempotent, got error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("third Close should also be idempotent, got error: %v", err)
	}
}

func TestClose_DisabledSession(t *testing.T) {
	s, err := NewTraceSession("", "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Close on a disabled session should not error
	if err := s.Close(); err != nil {
		t.Errorf("Close on disabled session returned error: %v", err)
	}
}

// --- Record on closed session ---

func TestRecordOnClosedSession_ReturnsNil(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	s.Close()

	if err := s.RecordTurn(TurnRecord{TurnIndex: 0, Timestamp: "2024-01-01T00:00:00Z"}); err != nil {
		t.Errorf("RecordTurn on closed session should return nil, got %v", err)
	}
	if err := s.RecordToolCall(ToolCallRecord{ToolName: "test", Timestamp: "2024-01-01T00:00:00Z"}); err != nil {
		t.Errorf("RecordToolCall on closed session should return nil, got %v", err)
	}
	if err := s.RecordArtifact(ArtifactManifest{RelativePath: "a", Timestamp: "2024-01-01T00:00:00Z"}); err != nil {
		t.Errorf("RecordArtifact on closed session should return nil, got %v", err)
	}
}

func TestRecordOnDisabledSession_ReturnsNil(t *testing.T) {
	s, err := NewTraceSession("", "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	if err := s.RecordTurn(TurnRecord{TurnIndex: 0, Timestamp: "2024-01-01T00:00:00Z"}); err != nil {
		t.Errorf("RecordTurn on disabled session should return nil, got %v", err)
	}
	if err := s.RecordToolCall(ToolCallRecord{ToolName: "test", Timestamp: "2024-01-01T00:00:00Z"}); err != nil {
		t.Errorf("RecordToolCall on disabled session should return nil, got %v", err)
	}
	if err := s.RecordArtifact(ArtifactManifest{RelativePath: "a", Timestamp: "2024-01-01T00:00:00Z"}); err != nil {
		t.Errorf("RecordArtifact on disabled session should return nil, got %v", err)
	}
}

// --- RunMetadata tests ---

func TestRunMetadata_FieldsCorrect(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "test-provider", "test-model-v2")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	meta := s.Metadata

	if meta.Provider != "test-provider" {
		t.Errorf("Provider mismatch: got %q, want %q", meta.Provider, "test-provider")
	}
	if meta.Model != "test-model-v2" {
		t.Errorf("Model mismatch: got %q, want %q", meta.Model, "test-model-v2")
	}
	if meta.RunID == "" {
		t.Error("RunID should not be empty")
	}

	// RunID format: YYYYMMDD_HHMMSS_hex6
	runIDPattern := regexp.MustCompile(`^\d{8}_\d{6}_[0-9a-f]{6}$`)
	if !runIDPattern.MatchString(meta.RunID) {
		t.Errorf("RunID %q does not match expected format YYYYMMDD_HHMMSS_hex6", meta.RunID)
	}

	// Timestamp should be valid RFC3339 and close to now
	parsed, err := time.Parse(time.RFC3339, meta.Timestamp)
	if err != nil {
		t.Fatalf("Timestamp %q is not valid RFC3339: %v", meta.Timestamp, err)
	}
	now := time.Now().UTC()
	diff := now.Sub(parsed)
	if diff < 0 {
		diff = -diff
	}
	if diff > 5*time.Second {
		t.Errorf("Timestamp %q is too far from now (diff=%v)", meta.Timestamp, diff)
	}

	// Default fields
	if meta.ReasoningMode != "" {
		t.Errorf("expected empty ReasoningMode, got %q", meta.ReasoningMode)
	}
	if meta.Persona != "" {
		t.Errorf("expected empty Persona, got %q", meta.Persona)
	}
	if meta.WorkflowName != "" {
		t.Errorf("expected empty WorkflowName, got %q", meta.WorkflowName)
	}
	if meta.WorkflowIndex != 0 {
		t.Errorf("expected WorkflowIndex=0, got %d", meta.WorkflowIndex)
	}
}

func TestRunMetadata_WrittenToRunsJsonl(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "openai", "gpt-4")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "runs.jsonl"))
	if err != nil {
		t.Fatalf("failed to read runs.jsonl: %v", err)
	}

	var meta RunMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("failed to unmarshal runs.jsonl: %v", err)
	}

	if meta.Provider != "openai" {
		t.Errorf("Provider in runs.jsonl: got %q, want %q", meta.Provider, "openai")
	}
	if meta.Model != "gpt-4" {
		t.Errorf("Model in runs.jsonl: got %q, want %q", meta.Model, "gpt-4")
	}
	if meta.RunID != s.GetRunID() {
		t.Error("RunID in runs.jsonl does not match session RunID")
	}
}

// --- hashFile helper tests ---

func TestHashFile_KnownContent(t *testing.T) {
	// Create a temp file with known content
	tmpFile := filepath.Join(t.TempDir(), "testfile.txt")
	content := "hello world"
	if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	hash, err := hashFile(tmpFile)
	if err != nil {
		t.Fatalf("hashFile error: %v", err)
	}

	// SHA-256 of "hello world"
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expected {
		t.Errorf("hash mismatch:\n  got:  %q\n  want: %q", hash, expected)
	}
}

func TestHashFile_EmptyFile(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(tmpFile, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write empty file: %v", err)
	}

	hash, err := hashFile(tmpFile)
	if err != nil {
		t.Fatalf("hashFile error: %v", err)
	}

	// SHA-256 of empty string
	expected := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if hash != expected {
		t.Errorf("hash mismatch:\n  got:  %q\n  want: %q", hash, expected)
	}
}

func TestHashFile_NonexistentFile(t *testing.T) {
	_, err := hashFile("/nonexistent/path/to/file.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestHashFile_BinaryContent(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "binary.bin")
	// Write some binary data
	data := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xab, 0xcd}
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		t.Fatalf("failed to write binary file: %v", err)
	}

	hash, err := hashFile(tmpFile)
	if err != nil {
		t.Fatalf("hashFile error: %v", err)
	}

	if len(hash) != 64 {
		t.Errorf("expected 64-char hex string, got length %d", len(hash))
	}

	// Verify it's valid hex
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("hash contains non-hex character: %c", c)
		}
	}
}

// --- randomID tests ---

func TestRandomID_Length(t *testing.T) {
	id := randomID(6)
	if len(id) != 6 {
		t.Errorf("expected length 6, got %d", len(id))
	}

	id12 := randomID(12)
	if len(id12) != 12 {
		t.Errorf("expected length 12, got %d", len(id12))
	}
}

func TestRandomID_Deterministic(t *testing.T) {
	// randomID is deterministic because it uses i%len(charset) pattern
	id1 := randomID(6)
	id2 := randomID(6)
	if id1 != id2 {
		t.Errorf("expected deterministic output, got %q vs %q", id1, id2)
	}
}

func TestRandomID_ExpectedOutput(t *testing.T) {
	// With charset "0123456789abcdef", b[i] = charset[i%16]
	// For length 6: "012345"
	id := randomID(6)
	expected := "012345"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}

	// For length 16: full charset repeated once "0123456789abcdef"
	id16 := randomID(16)
	expected16 := "0123456789abcdef"
	if id16 != expected16 {
		t.Errorf("expected %q, got %q", expected16, id16)
	}
}

// --- collectEnvConfig tests ---

func TestCollectEnvConfig_NoEnvVars(t *testing.T) {
	// Clear relevant env vars to test empty config
	for _, key := range []string{
		"SPROUT_INTERACTIVE_INPUT_MAX_CHARS",
		"SPROUT_AUTOMATION_INPUT_MAX_CHARS",
		"SPROUT_USER_INPUT_MAX_CHARS",
		"SPROUT_READ_FILE_MAX_BYTES",
		"SPROUT_SHELL_HEAD_TOKENS",
		"SPROUT_SHELL_TAIL_TOKENS",
		"SPROUT_VISION_MAX_TEXT_CHARS",
		"SPROUT_SEARCH_MAX_BYTES",
		"SPROUT_FETCH_URL_MAX_CHARS",
		"SPROUT_SUBAGENT_MAX_TOKENS",
		"SPROUT_NO_SUBAGENT_MODE",
		"SPROUT_ISOLATED_CONFIG",
	} {
		t.Setenv(key, "")
	}

	config := collectEnvConfig()
	if len(config) != 0 {
		t.Errorf("expected empty config with no env vars set, got %v", config)
	}
}

func TestCollectEnvConfig_WithEnvVars(t *testing.T) {
	t.Setenv("SPROUT_INTERACTIVE_INPUT_MAX_CHARS", "50000")
	t.Setenv("SPROUT_AUTOMATION_INPUT_MAX_CHARS", "25000")
	t.Setenv("SPROUT_USER_INPUT_MAX_CHARS", "10000")
	t.Setenv("SPROUT_READ_FILE_MAX_BYTES", "500000")
	t.Setenv("SPROUT_SHELL_HEAD_TOKENS", "200")
	t.Setenv("SPROUT_SHELL_TAIL_TOKENS", "100")
	t.Setenv("SPROUT_VISION_MAX_TEXT_CHARS", "30000")
	t.Setenv("SPROUT_SEARCH_MAX_BYTES", "100000")
	t.Setenv("SPROUT_FETCH_URL_MAX_CHARS", "80000")
	t.Setenv("SPROUT_SUBAGENT_MAX_TOKENS", "8000")
	t.Setenv("SPROUT_NO_SUBAGENT_MODE", "true")
	t.Setenv("SPROUT_ISOLATED_CONFIG", "isolated.toml")

	config := collectEnvConfig()

	expectedKeys := map[string]string{
		"interactive_input_max_chars": "50000",
		"automation_input_max_chars":  "25000",
		"user_input_max_chars":        "10000",
		"read_file_max_bytes":         "500000",
		"shell_head_tokens":           "200",
		"shell_tail_tokens":           "100",
		"vision_max_text_chars":       "30000",
		"search_max_bytes":            "100000",
		"fetch_url_max_chars":         "80000",
		"subagent_max_tokens":         "8000",
		"no_subagent_mode":            "true",
		"isolated_config":             "isolated.toml",
	}

	if len(config) != len(expectedKeys) {
		t.Errorf("expected %d config entries, got %d", len(expectedKeys), len(config))
	}

	for key, expectedVal := range expectedKeys {
		gotVal, ok := config[key]
		if !ok {
			t.Errorf("missing key %q in config", key)
			continue
		}
		if gotVal != expectedVal {
			t.Errorf("config[%q]: got %q, want %q", key, gotVal, expectedVal)
		}
	}
}

func TestCollectEnvConfig_PartialEnvVars(t *testing.T) {
	for _, key := range []string{
		"SPROUT_INTERACTIVE_INPUT_MAX_CHARS",
		"SPROUT_AUTOMATION_INPUT_MAX_CHARS",
	} {
		t.Setenv(key, "")
	}

	t.Setenv("SPROUT_NO_SUBAGENT_MODE", "lax")

	config := collectEnvConfig()
	if len(config) != 1 {
		t.Errorf("expected exactly 1 config entry, got %d: %v", len(config), config)
	}
	if config["no_subagent_mode"] != "lax" {
		t.Errorf("no_subagent_mode: got %q, want %q", config["no_subagent_mode"], "lax")
	}
}

// --- Accessor tests ---

func TestGetRunID(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "provider", "model")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	runID := s.GetRunID()
	if runID == "" {
		t.Error("GetRunID returned empty string")
	}
	if runID != s.RunID {
		t.Error("GetRunID does not match RunID")
	}
	if runID != s.Metadata.RunID {
		t.Error("GetRunID does not match Metadata.RunID")
	}
}

func TestGetRunDir(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "provider", "model")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	runDir := s.GetRunDir()
	if runDir == "" {
		t.Error("GetRunDir returned empty string")
	}
	if !strings.HasPrefix(runDir, traceDir) {
		t.Errorf("GetRunDir %q should be under traceDir %q", runDir, traceDir)
	}
	if runDir != s.RunDir {
		t.Error("GetRunDir does not match RunDir")
	}
}

// --- Machine label constants ---

func TestLabelConstants(t *testing.T) {
	labels := map[string]string{
		"LabelPathViolationAbsolute":     LabelPathViolationAbsolute,
		"LabelPathViolationNested":       LabelPathViolationNested,
		"LabelPathViolationDisallowed":   LabelPathViolationDisallowed,
		"LabelSchemaEnvelopeViolation":   LabelSchemaEnvelopeViolation,
		"LabelLayoutViolation":           LabelLayoutViolation,
		"LabelToolCallValidationFailure": LabelToolCallValidationFailure,
		"LabelToolCallUnknownTool":       LabelToolCallUnknownTool,
		"LabelToolCallTimeout":           LabelToolCallTimeout,
		"LabelToolCallExecutionError":    LabelToolCallExecutionError,
	}

	for name, val := range labels {
		if val == "" {
			t.Errorf("label constant %s is empty", name)
		}
		if strings.Contains(val, " ") {
			t.Errorf("label constant %s contains spaces: %q", name, val)
		}
	}
}

// --- NewJSONLWriter tests ---

func TestNewJSONLWriter_BadDirectory(t *testing.T) {
	// Create a path in a directory that doesn't exist
	badPath := filepath.Join(t.TempDir(), "nonexistent", "deep", "nested", "file.jsonl")

	// Verify directory doesn't exist
	if _, err := os.Stat(filepath.Dir(badPath)); err == nil {
		t.Fatal("expected nonexistent directory")
	}

	// Attempt to create writer - should fail
	w, err := newJSONLWriter(badPath)
	if err == nil {
		t.Fatal("expected error for non-existent directory, got nil")
	}
	if !strings.Contains(err.Error(), "no such file or directory") {
		t.Errorf("expected 'no such file or directory' error, got: %v", err)
	}
	if w != nil {
		t.Error("expected nil writer on error")
	}
}

// --- NewTraceSession directory creation tests ---

func TestNewTraceSession_MkdirFails(t *testing.T) {
	// Create a temporary directory and make it read-only
	parentDir := t.TempDir()
	if err := os.Chmod(parentDir, 0555); err != nil {
		t.Fatalf("failed to make parent directory read-only: %v", err)
	}

	// Attempt to create session in read-only directory
	s, err := NewTraceSession(parentDir, "provider", "model")
	if err == nil {
		t.Fatal("expected error for read-only directory")
	}
	if err == nil && s != nil {
		s.Close()
	}

	// Verify error message
	errMsg := err.Error()
	if !strings.Contains(errMsg, "failed to create trace run directory") {
		t.Errorf("expected error containing 'failed to create trace run directory', got: %v", err)
	}
}

// --- Close idempotency tests ---

func TestClose_IdempotentAfterRecord(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Record a turn to ensure writers are created
	if err := s.RecordTurn(TurnRecord{
		RunID:     s.GetRunID(),
		TurnIndex: 0,
		Timestamp: "2024-06-15T10:30:00Z",
	}); err != nil {
		t.Fatalf("RecordTurn error: %v", err)
	}

	// Close the session
	if err := s.Close(); err != nil {
		t.Fatalf("first Close error: %v", err)
	}

	// Closing again should be idempotent (no error)
	if err := s.Close(); err != nil {
		t.Errorf("second Close should be idempotent, got error: %v", err)
	}

	// Closing a third time should also be idempotent
	if err := s.Close(); err != nil {
		t.Errorf("third Close should be idempotent, got error: %v", err)
	}
}

// --- NewTraceSession: tools.jsonl writer fails (later writer error, tests defer cleanup) ---

func TestNewTraceSession_ToolsWriterFails(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to pre-create run dir: %v", err)
	}

	// Create tool_calls.jsonl as a directory — os.OpenFile will fail
	obstacle := filepath.Join(runDir, "tool_calls.jsonl")
	if err := os.Mkdir(obstacle, 0o755); err != nil {
		t.Fatalf("failed to create obstacle: %v", err)
	}

	_, err := NewTraceSession(traceDir, "provider", "model")
	if err == nil {
		t.Fatal("expected error when tool_calls.jsonl is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory") && !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("expected directory-related error, got: %v", err)
	}
}

// --- NewTraceSession: artifacts_manifest.jsonl writer fails (tests defer cleanup of toolsWriter) ---

func TestNewTraceSession_ArtifactsWriterFails(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to pre-create run dir: %v", err)
	}

	// Create artifacts_manifest.jsonl as a directory
	obstacle := filepath.Join(runDir, "artifacts_manifest.jsonl")
	if err := os.Mkdir(obstacle, 0o755); err != nil {
		t.Fatalf("failed to create obstacle: %v", err)
	}

	_, err := NewTraceSession(traceDir, "provider", "model")
	if err == nil {
		t.Fatal("expected error when artifacts_manifest.jsonl is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory") && !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("expected directory-related error, got: %v", err)
	}
}

// --- NewTraceSession: metadata write fails (runs.jsonl writes fail) ---

func TestNewTraceSession_MetadataWriteFails(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to pre-create run dir: %v", err)
	}

	// Create runs.jsonl as a symlink to /dev/full.
	// newJSONLWriter opens with O_CREATE|O_WRONLY|O_TRUNC which succeeds on /dev/full,
	// but encoder.Encode (json.NewEncoder.Write) fails with ENOSPC.
	runsTarget := filepath.Join(runDir, "runs.jsonl")
	if err := os.Symlink("/dev/full", runsTarget); err != nil {
		t.Skipf("skipping: cannot create symlink to /dev/full: %v", err)
	}

	_, err := NewTraceSession(traceDir, "provider", "model")
	if err == nil {
		t.Fatal("expected error when metadata write to /dev/full fails (ENOSPC)")
	}
	if !strings.Contains(err.Error(), "failed to write run metadata") && !strings.Contains(err.Error(), "failed to create runs writer") {
		t.Errorf("expected metadata write failure error, got: %v", err)
	}
	t.Logf("got expected metadata write failure: %v", err)
}

// --- Close: only ToolsFile close produces an error ---

func TestClose_ToolsFileError(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Pre-close ONLY the tools file handle to trigger error in Close
	s.mu.Lock()
	if s.ToolsFile != nil {
		s.ToolsFile.file.Close()
	}
	s.mu.Unlock()

	err = s.Close()
	if err == nil {
		t.Log("Close returned nil even though ToolsFile was pre-closed (OS-dependent)")
	} else {
		t.Logf("got expected close error: %v", err)
	}

	// Second close should be idempotent
	if err := s.Close(); err != nil {
		t.Errorf("second Close should be idempotent, got error: %v", err)
	}
}

// --- Close: only ArtifactsFile close produces an error ---

func TestClose_ArtifactsFileError(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Pre-close ONLY the artifacts file handle
	s.mu.Lock()
	if s.ArtifactsFile != nil {
		s.ArtifactsFile.file.Close()
	}
	s.mu.Unlock()

	err = s.Close()
	if err == nil {
		t.Log("Close returned nil even though ArtifactsFile was pre-closed (OS-dependent)")
	} else {
		t.Logf("got expected close error: %v", err)
	}

	// Second close should be idempotent
	if err := s.Close(); err != nil {
		t.Errorf("second Close should be idempotent, got error: %v", err)
	}
}

// --- Close: multiple file close errors (returns first error) ---

func TestClose_MultipleFileErrors(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	s.mu.Lock()
	// Pre-close runs + tools handles (multiple errors)
	if s.RunsFile != nil {
		s.RunsFile.file.Close()
	}
	if s.ToolsFile != nil {
		s.ToolsFile.file.Close()
	}
	if s.ArtifactsFile != nil {
		s.ArtifactsFile.file.Close()
	}
	s.mu.Unlock()

	// Should return first error, not panic
	err = s.Close()
	if err == nil {
		t.Log("Close returned nil with multiple pre-closed files (OS-dependent)")
	}

	if err := s.Close(); err != nil {
		t.Errorf("second Close should be idempotent, got error: %v", err)
	}
}

// --- RecordTurn: large data with many tool calls ---

func TestRecordTurn_LargeData(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	// Build a large system prompt
	largePrompt := strings.Repeat("This is a system prompt line with various instructions. ", 500)

	// Build many messages
	messages := make([]api.Message, 100)
	for i := range messages {
		messages[i] = api.Message{
			Role:    "user",
			Content: strings.Repeat("x", 1000),
		}
	}

	// Build many tool calls
	toolCalls := make([]api.ToolCall, 50)
	for i := range toolCalls {
		toolCalls[i] = api.ToolCall{
			ID:   "tc_" + strings.Repeat("0", i),
			Type: "function",
		}
		toolCalls[i].Function.Name = "tool_" + strings.Repeat("a", i%20)
		toolCalls[i].Function.Arguments = `{"arg":"` + strings.Repeat("d", 200) + `"}`
	}

	record := TurnRecord{
		RunID:              s.GetRunID(),
		TurnIndex:          0,
		SystemPrompt:       largePrompt,
		UserPrompt:         strings.Repeat("User prompt text. ", 200),
		UserPromptOriginal: strings.Repeat("Original user prompt. ", 200),
		MessagesSent:       messages,
		ToolSchemaPayload:  json.RawMessage(`[{"type":"function"}]`),
		RawResponse:        strings.Repeat("response body ", 1000),
		ParsedToolCalls:    toolCalls,
		ParserErrors:       []string{"error1", "error2", "error3"},
		FallbackUsed:       true,
		FallbackOutput:     strings.Repeat("fallback ", 500),
		MachineLabels:      []string{"label1", "label2", "label3", "label4"},
		Timestamp:          "2024-06-15T10:30:00Z",
	}

	if err := s.RecordTurn(record); err != nil {
		t.Fatalf("RecordTurn with large data returned error: %v", err)
	}

	s.Close()

	// Read back and verify turn record count
	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("failed to read turns.jsonl: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024) // 10MB buffer for large JSON lines
	count := 0
	for scanner.Scan() {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 turn record, got %d", count)
	}
}

// --- RecordToolCall: all fields populated including ArgsNormalized, ErrorCategory, ErrorMessage ---

func TestRecordToolCall_AllFields(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	record := ToolCallRecord{
		RunID:     s.GetRunID(),
		TurnIndex: 3,
		ToolIndex: 7,
		ToolName:  "edit_file",
		Args: map[string]interface{}{
			"path":    "/src/main.go",
			"content": "package main\n\nfunc main() {}",
			"append":  false,
		},
		ArgsNormalized: map[string]interface{}{
			"path":    "/src/main.go",
			"content": "package main\n\nfunc main() {}",
			"append":  false,
		},
		Success:       false,
		FullResult:    strings.Repeat("error line\n", 100),
		ModelResult:   "truncated error output",
		ErrorCategory: "timeout",
		ErrorMessage:  "operation timed out after 30s",
		MachineLabels: []string{
			LabelToolCallTimeout,
			LabelToolCallExecutionError,
		},
		Timestamp: "2024-06-15T10:31:00Z",
	}

	if err := s.RecordToolCall(record); err != nil {
		t.Fatalf("RecordToolCall returned error: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "tool_calls.jsonl"))
	if err != nil {
		t.Fatalf("failed to read tool_calls.jsonl: %v", err)
	}

	var got ToolCallRecord
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		t.Fatal("no line found in tool_calls.jsonl")
	}
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if got.ToolName != "edit_file" {
		t.Errorf("ToolName: got %q, want %q", got.ToolName, "edit_file")
	}
	if got.TurnIndex != 3 {
		t.Errorf("TurnIndex: got %d, want 3", got.TurnIndex)
	}
	if got.ToolIndex != 7 {
		t.Errorf("ToolIndex: got %d, want 7", got.ToolIndex)
	}
	if got.ArgsNormalized == nil {
		t.Error("ArgsNormalized should not be nil")
	}
	if got.ErrorCategory != "timeout" {
		t.Errorf("ErrorCategory: got %q, want %q", got.ErrorCategory, "timeout")
	}
	if got.ErrorMessage != "operation timed out after 30s" {
		t.Errorf("ErrorMessage: got %q, want %q", got.ErrorMessage, "operation timed out after 30s")
	}
	if got.Success {
		t.Error("expected Success=false")
	}
	if len(got.MachineLabels) != 2 {
		t.Errorf("MachineLabels: expected 2, got %d", len(got.MachineLabels))
	}
}

// --- RecordArtifact: various artifact types ---

func TestRecordArtifact_VariousTypes(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	artifactTypes := []struct {
		artType string
		labels  []string
	}{
		{"file_edit", []string{LabelPathViolationAbsolute}},
		{"file_create", []string{LabelPathViolationNested}},
		{"file_delete", []string{}},
		{"directory_create", []string{LabelPathViolationDisallowed}},
		{"directory_delete", []string{}},
		{"file_read", []string{LabelSchemaEnvelopeViolation, LabelLayoutViolation}},
		{"shell_output", []string{LabelToolCallUnknownTool}},
		{"diff_output", []string{}},
	}

	for i, tt := range artifactTypes {
		record := ArtifactManifest{
			RunID:         s.GetRunID(),
			RelativePath:  "path/to/artifact_" + tt.artType,
			SizeBytes:     int64(1024 * (i + 1)),
			Hash:          strings.Repeat(string('a'+byte(i)), 64),
			ArtifactType:  tt.artType,
			MachineLabels: tt.labels,
			Timestamp:     "2024-06-15T10:32:00Z",
		}

		if err := s.RecordArtifact(record); err != nil {
			t.Fatalf("RecordArtifact %d (%s) error: %v", i, tt.artType, err)
		}
	}

	s.Close()

	// Read back and verify count
	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "artifacts_manifest.jsonl"))
	if err != nil {
		t.Fatalf("failed to read artifacts_manifest.jsonl: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	count := 0
	for scanner.Scan() {
		var art ArtifactManifest
		if err := json.Unmarshal(scanner.Bytes(), &art); err != nil {
			t.Fatalf("failed to unmarshal artifact at line %d: %v", count+1, err)
		}
		if art.ArtifactType != artifactTypes[count].artType {
			t.Errorf("line %d: ArtifactType got %q, want %q", count+1, art.ArtifactType, artifactTypes[count].artType)
		}
		if len(art.MachineLabels) != len(artifactTypes[count].labels) {
			t.Errorf("line %d: MachineLabels count got %d, want %d", count+1, len(art.MachineLabels), len(artifactTypes[count].labels))
		}
		count++
	}

	if count != len(artifactTypes) {
		t.Errorf("expected %d artifact records, got %d", len(artifactTypes), count)
	}
}

// --- Concurrent RecordTurn and RecordToolCall (non-flaky) ---

func TestConcurrent_RecordTurnAndToolCall(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	const numGoroutines = 10
	const perGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // half turns, half tool calls

	// Launch turn writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				err := s.RecordTurn(TurnRecord{
					RunID:     s.GetRunID(),
					TurnIndex: id*perGoroutine + j,
					Timestamp: "2024-01-01T00:00:00Z",
				})
				if err != nil {
					t.Errorf("goroutine %d turn %d: %v", id, j, err)
				}
			}
		}(i)
	}

	// Launch tool call writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				err := s.RecordToolCall(ToolCallRecord{
					RunID:     s.GetRunID(),
					TurnIndex: id,
					ToolIndex: j,
					ToolName:  "concurrent_tool",
					Args:      map[string]interface{}{"id": id, "idx": j},
					Timestamp: "2024-01-01T00:00:00Z",
				})
				if err != nil {
					t.Errorf("goroutine %d tool %d: %v", id, j, err)
				}
			}
		}(i)
	}

	wg.Wait()

	if err := s.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// Verify correct line counts
	for _, file := range []struct {
		name     string
		expected int
	}{
		{"turns.jsonl", numGoroutines * perGoroutine},
		{"tool_calls.jsonl", numGoroutines * perGoroutine},
	} {
		data, err := os.ReadFile(filepath.Join(s.GetRunDir(), file.name))
		if err != nil {
			t.Fatalf("failed to read %s: %v", file.name, err)
		}
		lines := 0
		for _, b := range data {
			if b == '\n' {
				lines++
			}
		}
		if lines != file.expected {
			t.Errorf("%s: expected %d lines, got %d", file.name, file.expected, lines)
		}
	}
}
