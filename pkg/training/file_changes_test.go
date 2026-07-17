package training

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// Helpers for file-change tests
// ---------------------------------------------------------------------------

// writeChangeDir creates a .sprout/changes/<hash>/ directory with the
// given metadata, base64-encoded .original and .updated files.
func writeChangeDir(t *testing.T, changesDir, hash, filename, description, model,
	original, updated string) {
	t.Helper()
	dir := filepath.Join(changesDir, hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir change dir: %v", err)
	}

	// Sanitize filename the same way the history package does.
	safe := strings.ReplaceAll(filename, "/", "_")
	safe = strings.ReplaceAll(safe, "\\", "_")

	// Write base64-encoded .original and .updated.
	if err := os.WriteFile(filepath.Join(dir, safe+".original"), []byte(base64.StdEncoding.EncodeToString([]byte(original))), 0o644); err != nil {
		t.Fatalf("write .original: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, safe+".updated"), []byte(base64.StdEncoding.EncodeToString([]byte(updated))), 0o644); err != nil {
		t.Fatalf("write .updated: %v", err)
	}

	meta := changeMetadata{
		Version:     1,
		Filename:    filename,
		Description: description,
		LLMMessage:  "Task completed",
		AgentModel:  model,
		FileRevHash: hash,
	}
	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), metaBytes, 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
}

// setupFileChangeTest creates a temp workspace with .sprout/changes and
// a session that points to it, so discoverChangeDirs finds it.
func setupFileChangeTest(t *testing.T) (workspace, changesDir string) {
	t.Helper()
	dir := t.TempDir()
	workspace = dir
	changesDir = filepath.Join(workspace, ".sprout", "changes")
	if err := os.MkdirAll(changesDir, 0o755); err != nil {
		t.Fatalf("mkdir changes dir: %v", err)
	}

	// Create a session pointing to this workspace so discoverChangeDirs finds it.
	stateDir := filepath.Join(dir, "sessions")
	restore := agent.SetStateDirFuncForTesting(func() (string, error) {
		return stateDir, nil
	})
	t.Cleanup(restore)

	state := emptyConversationState("fc-test-session")
	state.WorkingDirectory = workspace
	if err := agent.WriteTestSessionFile(stateDir, state.SessionID, workspace, &state); err != nil {
		t.Fatalf("WriteTestSessionFile: %v", err)
	}

	return workspace, changesDir
}

const sampleGoOriginal = `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}
`

const sampleGoUpdated = `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`

// ---------------------------------------------------------------------------
// Filter tests
// ---------------------------------------------------------------------------

func TestHasBlockedPathSegment(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/home/user/project/node_modules/foo.js", true},
		{"/home/user/project/vendor/lib.go", true},
		{"/home/user/project/build/out.go", true},
		{"/home/user/project/src/main.go", false},
		{"/home/user/project/Pods/Framework.swift", true},
		{"/home/user/project/.git/config", true},
		{"/home/user/project/DerivedData/foo.o", true},
		{"/home/user/project/__pycache__/module.py", true},
		{"/home/user/project/.swiftpm/config", true},
	}
	for _, tt := range tests {
		if got := hasBlockedPathSegment(tt.path); got != tt.want {
			t.Errorf("hasBlockedPathSegment(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestAllowedSourceCodeExts(t *testing.T) {
	good := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".swift", ".rs", ".java", ".kt", ".css", ".scss", ".html", ".json", ".yaml", ".yml", ".md", ".sh"}
	for _, ext := range good {
		if !allowedSourceCodeExts[ext] {
			t.Errorf("expected %q to be allowed", ext)
		}
	}
}

func TestBlockedBinaryExts(t *testing.T) {
	good := []string{".png", ".jpg", ".jpeg", ".pcm", ".dia", ".etag", ".d", ".swiftdeps", ".xcscheme", ".xcconfig", ".lock", ".bin", ".o", ".a", ".dylib"}
	for _, ext := range good {
		if !blockedBinaryExts[ext] {
			t.Errorf("expected %q to be blocked", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// readBase64File tests
// ---------------------------------------------------------------------------

func TestReadBase64File(t *testing.T) {
	dir := t.TempDir()
	content := "hello world"
	safe := "test.go"
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	if err := os.WriteFile(filepath.Join(dir, safe+".original"), []byte(encoded), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, ok := readBase64File(dir, "test.go", ".original")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestReadBase64File_PlaintextFallback(t *testing.T) {
	dir := t.TempDir()
	content := "plain text not base64"
	safe := "test.go"

	if err := os.WriteFile(filepath.Join(dir, safe+".original"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, ok := readBase64File(dir, "test.go", ".original")
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Non-base64 content should fall back to raw string.
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestReadBase64File_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, ok := readBase64File(dir, "nonexistent.go", ".original")
	if ok {
		t.Error("expected ok=false for missing file")
	}
}

// ---------------------------------------------------------------------------
// buildChangeExample tests
// ---------------------------------------------------------------------------

func TestBuildChangeExample_Valid(t *testing.T) {
	dir := t.TempDir()
	writeChangeDir(t, dir, "hash1", "/src/main.go", "edit via EditFile", "qwen3.6-27b",
		sampleGoOriginal, sampleGoUpdated)

	ex, ok := buildChangeExample(filepath.Join(dir, "hash1"), defaultMaxSize)
	if !ok {
		t.Fatal("expected buildChangeExample to succeed")
	}
	if ex.Metadata.Source != "file_change" {
		t.Errorf("source = %q, want file_change", ex.Metadata.Source)
	}
	if ex.Metadata.File != "/src/main.go" {
		t.Errorf("file = %q", ex.Metadata.File)
	}
	if ex.Metadata.Model != "qwen3.6-27b" {
		t.Errorf("model = %q", ex.Metadata.Model)
	}
	if len(ex.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(ex.Messages))
	}
	if ex.Messages[0].Role != "system" {
		t.Error("first message should be system")
	}
	if ex.Messages[1].Role != "user" {
		t.Error("second message should be user")
	}
	if ex.Messages[2].Role != "assistant" {
		t.Error("third message should be assistant")
	}
	// User message should contain original content.
	if !strings.Contains(ex.Messages[1].Content, sampleGoOriginal) {
		t.Error("user content should contain original file content")
	}
	// Assistant message should contain updated content.
	if !strings.Contains(ex.Messages[2].Content, sampleGoUpdated) {
		t.Error("assistant content should contain updated file content")
	}
}

func TestBuildChangeExample_NonEditDescription(t *testing.T) {
	dir := t.TempDir()
	writeChangeDir(t, dir, "hash1", "/src/main.go", "create via shell_command", "model",
		sampleGoOriginal, sampleGoUpdated)

	_, ok := buildChangeExample(dir, defaultMaxSize)
	if ok {
		t.Error("expected buildChangeExample to reject 'create via shell_command'")
	}
}

func TestBuildChangeExample_BlockedPath(t *testing.T) {
	dir := t.TempDir()
	writeChangeDir(t, dir, "hash1", "/src/node_modules/main.go", "edit via shell_command", "model",
		sampleGoOriginal, sampleGoUpdated)

	_, ok := buildChangeExample(dir, defaultMaxSize)
	if ok {
		t.Error("expected buildChangeExample to reject node_modules path")
	}
}

func TestBuildChangeExample_DisallowedExtension(t *testing.T) {
	dir := t.TempDir()
	writeChangeDir(t, dir, "hash1", "/src/main.png", "edit via EditFile", "model",
		"binary1", "binary2")

	_, ok := buildChangeExample(dir, defaultMaxSize)
	if ok {
		t.Error("expected buildChangeExample to reject .png extension")
	}
}

func TestBuildChangeExample_IdenticalContent(t *testing.T) {
	dir := t.TempDir()
	writeChangeDir(t, dir, "hash1", "/src/main.go", "edit via EditFile", "model",
		sampleGoOriginal, sampleGoOriginal) // identical

	_, ok := buildChangeExample(dir, defaultMaxSize)
	if ok {
		t.Error("expected buildChangeExample to reject identical content")
	}
}

func TestBuildChangeExample_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	writeChangeDir(t, dir, "hash1", "/src/main.go", "edit via EditFile", "model",
		"", sampleGoUpdated) // empty original

	_, ok := buildChangeExample(dir, defaultMaxSize)
	if ok {
		t.Error("expected buildChangeExample to reject empty original")
	}
}

func TestBuildChangeExample_ExceedsMaxSize(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("a", 200)
	writeChangeDir(t, dir, "hash1", "/src/main.go", "edit via EditFile", "model",
		big, big+"b")

	_, ok := buildChangeExample(dir, 100) // max 100 chars, content is 200
	if ok {
		t.Error("expected buildChangeExample to reject content exceeding maxSize")
	}
}

func TestBuildChangeExample_MissingMetadata(t *testing.T) {
	dir := t.TempDir()
	// No metadata.json at all.
	os.MkdirAll(dir, 0o755)
	_, ok := buildChangeExample(dir, defaultMaxSize)
	if ok {
		t.Error("expected buildChangeExample to fail with missing metadata")
	}
}

// ---------------------------------------------------------------------------
// processChangeDir tests
// ---------------------------------------------------------------------------

func TestProcessChangeDir(t *testing.T) {
	_, changesDir := setupFileChangeTest(t)

	// One valid change.
	writeChangeDir(t, changesDir, "hash1", "/src/main.go", "edit via EditFile", "model1",
		sampleGoOriginal, sampleGoUpdated)
	// One filtered (create via shell_command).
	writeChangeDir(t, changesDir, "hash2", "/src/other.go", "create via shell_command", "model2",
		"a", "b\n")

	seen := make(map[string]bool)
	examples, scanned, filtered := processChangeDir(changesDir, defaultMaxSize, seen)

	if scanned != 2 {
		t.Errorf("scanned = %d, want 2", scanned)
	}
	if filtered != 1 {
		t.Errorf("filtered = %d, want 1", filtered)
	}
	if len(examples) != 1 {
		t.Fatalf("examples = %d, want 1", len(examples))
	}
}

func TestProcessChangeDir_Dedup(t *testing.T) {
	_, changesDir := setupFileChangeTest(t)

	// Same hash in two change dirs (shouldn't happen normally, but dedup
	// is by directory name).
	writeChangeDir(t, changesDir, "hash1", "/src/main.go", "edit via EditFile", "model1",
		sampleGoOriginal, sampleGoUpdated)

	seen := make(map[string]bool)
	ex1, s1, f1 := processChangeDir(changesDir, defaultMaxSize, seen)
	ex2, _, f2 := processChangeDir(changesDir, defaultMaxSize, seen)

	if len(ex1) != 1 {
		t.Fatalf("first pass: expected 1 example, got %d", len(ex1))
	}
	if s1 != 1 || f1 != 0 {
		t.Errorf("first pass: scanned=%d filtered=%d", s1, f1)
	}
	// Second pass should dedup everything.
	if len(ex2) != 0 {
		t.Errorf("second pass: expected 0 examples (deduped), got %d", len(ex2))
	}
	if f2 != 1 {
		t.Errorf("second pass: expected 1 filtered, got %d", f2)
	}
}

// ---------------------------------------------------------------------------
// ExportFileChanges end-to-end test
// ---------------------------------------------------------------------------

func TestExportFileChanges_EndToEnd(t *testing.T) {
	// Test processChangeDir + writeFileChangeJSONL directly
	// (ExportFileChanges discovers real .sprout dirs on the machine)
	_, changesDir := setupFileChangeTest(t)

	writeChangeDir(t, changesDir, "hash1", "/src/main.go", "edit via EditFile", "qwen3.6-27b",
		sampleGoOriginal, sampleGoUpdated)
	writeChangeDir(t, changesDir, "hash2", "/src/other.go", "create via shell_command", "model2",
		"x", "y")

	seen := make(map[string]bool)
	examples, scanned, filtered := processChangeDir(changesDir, defaultMaxSize, seen)
	if scanned != 2 {
		t.Errorf("scanned = %d, want 2", scanned)
	}
	if len(examples) != 1 {
		t.Errorf("exported = %d, want 1 (create filtered out)", len(examples))
	}
	if filtered != 1 {
		t.Errorf("filtered = %d, want 1", filtered)
	}

	// Verify the example content
	if len(examples) != 1 {
		t.Fatal("expected 1 example")
	}
	ex := examples[0]
	if len(ex.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(ex.Messages))
	}
	if ex.Metadata.Source != "file_change" {
		t.Errorf("source = %q", ex.Metadata.Source)
	}
	if ex.Metadata.File != "/src/main.go" {
		t.Errorf("file = %q", ex.Metadata.File)
	}
}

func TestExportFileChanges_EmptyOutput(t *testing.T) {
	_, err := ExportFileChanges(FileChangeExportOptions{Output: ""})
	if err == nil {
		t.Fatal("expected error for empty output path")
	}
}

func TestExportFileChanges_NoChangesDir(t *testing.T) {
	// Test processChangeDir directly with an empty temp dir
	// (ExportFileChanges discovers real .sprout dirs on the machine)
	emptyDir := t.TempDir()
	seen := make(map[string]bool)
	exs, scanned, filtered := processChangeDir(emptyDir, defaultMaxSize, seen)
	if scanned != 0 {
		t.Errorf("scanned = %d, want 0", scanned)
	}
	if filtered != 0 {
		t.Errorf("filtered = %d, want 0", filtered)
	}
	if len(exs) != 0 {
		t.Errorf("got %d examples, want 0", len(exs))
	}
}

func TestExportFileChanges_CustomMaxSize(t *testing.T) {
	_, changesDir := setupFileChangeTest(t)

	bigOriginal := strings.Repeat("a", 200)
	bigUpdated := strings.Repeat("b", 200)
	writeChangeDir(t, changesDir, "hash1", "/src/big.go", "edit via EditFile", "model",
		bigOriginal, bigUpdated)

	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	result, err := ExportFileChanges(FileChangeExportOptions{
		Output:  outPath,
		MaxSize: 100, // content is 200 chars, should be filtered
	})
	if err != nil {
		t.Fatalf("ExportFileChanges failed: %v", err)
	}
	if result.ChangesExported != 0 {
		t.Errorf("expected 0 exported (too big), got %d", result.ChangesExported)
	}
}

func TestExportFileChanges_Redaction(t *testing.T) {
	dir := t.TempDir()
	writeChangeDir(t, dir, "hash1", "/src/secret.go", "edit via EditFile", "model",
		"package main\n", "package main\n// updated\n")
	ex, ok := buildChangeExample(filepath.Join(dir, "hash1"), defaultMaxSize)
	if !ok {
		t.Fatal("expected buildChangeExample to succeed")
	}
	if len(ex.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(ex.Messages))
	}
}

func makeSubagentToolCall(id, prompt, persona string) api.ToolCall {
	args := map[string]interface{}{"prompt": prompt}
	if persona != "" {
		args["persona"] = persona
	}
	argsJSON, _ := json.Marshal(args)
	tc := api.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = "run_subagent"
	tc.Function.Arguments = string(argsJSON)
	return tc
}

// makeParallelSubagentToolCall creates a run_parallel_subagents tool call.
func makeParallelSubagentToolCall(id string, prompts []string) api.ToolCall {
	tasks := make([]interface{}, len(prompts))
	for i, p := range prompts {
		tasks[i] = p
	}
	args := map[string]interface{}{"subagents": tasks}
	argsJSON, _ := json.Marshal(args)
	tc := api.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = "run_parallel_subagents"
	tc.Function.Arguments = string(argsJSON)
	return tc
}

// makeSubagentResult builds a JSON result string for a run_subagent tool.
func makeSubagentResult(stdout string) string {
	m := map[string]interface{}{
		"stdout":    stdout,
		"exit_code": "0",
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// makeParallelSubagentResult builds a JSON result for run_parallel_subagents.
func makeParallelSubagentResult(outputs map[string]string) string {
	m := make(map[string]interface{})
	for k, v := range outputs {
		m[k] = map[string]interface{}{
			"stdout":    v,
			"exit_code": "0",
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestExtractSubagentExamples_SingleSubagent(t *testing.T) {
	prompt := "Write a hello world Go program and explain the code."
	output := strings.Repeat("Here is the hello world program: ", 5) // >50 chars

	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{Role: "user", Content: "I need a subagent to write code"},
			{
				Role:    "assistant",
				Content: "Let me spawn a coder subagent.",
				ToolCalls: []api.ToolCall{
					makeSubagentToolCall("call_1", prompt, "coder"),
				},
			},
			{Role: "tool", Content: makeSubagentResult(output), ToolCallID: "call_1"},
			{Role: "assistant", Content: "The subagent completed the task."},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(examples))
	}

	if len(examples[0].Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(examples[0].Messages))
	}
	if examples[0].Messages[0].Role != "system" {
		t.Error("first message should be system")
	}
	if examples[0].Messages[1].Role != "user" {
		t.Error("second message should be user")
	}
	if examples[0].Messages[1].Content != prompt {
		t.Errorf("user content = %q, want %q", examples[0].Messages[1].Content, prompt)
	}
	if examples[0].Messages[2].Role != "assistant" {
		t.Error("third message should be assistant")
	}
	if examples[0].Messages[2].Content != output {
		t.Errorf("assistant content mismatch")
	}
}

func TestExtractSubagentExamples_ShortOutput(t *testing.T) {
	// Output shorter than 50 chars should be filtered.
	prompt := "Do a small task."
	output := "short" // < 50 chars

	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{
				Role:    "assistant",
				Content: "Spawning subagent.",
				ToolCalls: []api.ToolCall{
					makeSubagentToolCall("call_1", prompt, ""),
				},
			},
			{Role: "tool", Content: makeSubagentResult(output), ToolCallID: "call_1"},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 0 {
		t.Errorf("expected 0 examples for short output, got %d", len(examples))
	}
}

func TestExtractSubagentExamples_EmptyOutput(t *testing.T) {
	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{
				Role:    "assistant",
				Content: "Spawning.",
				ToolCalls: []api.ToolCall{
					makeSubagentToolCall("call_1", "prompt", "coder"),
				},
			},
			{Role: "tool", Content: makeSubagentResult(""), ToolCallID: "call_1"},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 0 {
		t.Errorf("expected 0 examples for empty output, got %d", len(examples))
	}
}

func TestExtractSubagentExamples_ParallelSubagents(t *testing.T) {
	prompts := []string{
		"Check all Go files for stdlib log usage.",
		"Check all Python files for print statements.",
	}
	outputs := map[string]string{
		"task-1": strings.Repeat("Found 3 files using stdlib log package.", 3),
		"task-2": strings.Repeat("Found 5 files using print statements.", 3),
	}

	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{
				Role:    "assistant",
				Content: "Running parallel checks.",
				ToolCalls: []api.ToolCall{
					makeParallelSubagentToolCall("call_1", prompts),
				},
			},
			{Role: "tool", Content: makeParallelSubagentResult(outputs), ToolCallID: "call_1"},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 2 {
		t.Fatalf("expected 2 examples, got %d", len(examples))
	}

	// Check the first example has the first prompt.
	if !strings.Contains(examples[0].Messages[1].Content, "Go files") {
		t.Error("first example should have the first task prompt")
	}
	// Check the second example has the second prompt.
	if !strings.Contains(examples[1].Messages[1].Content, "Python files") {
		t.Error("second example should have the second task prompt")
	}
}

func TestExtractSubagentExamples_ParallelWithShortOutput(t *testing.T) {
	prompts := []string{"Task 1", "Task 2"}
	outputs := map[string]string{
		"task-1": strings.Repeat("Valid output here.", 5), // >50 chars
		"task-2": "short",                                 // <50 chars, filtered
	}

	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{
				Role: "assistant",
				ToolCalls: []api.ToolCall{
					makeParallelSubagentToolCall("call_1", prompts),
				},
			},
			{Role: "tool", Content: makeParallelSubagentResult(outputs), ToolCallID: "call_1"},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 1 {
		t.Fatalf("expected 1 example (1 filtered for short output), got %d", len(examples))
	}
}

func TestExtractSubagentExamples_NoSubagentCalls(t *testing.T) {
	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 0 {
		t.Errorf("expected 0 examples with no subagent calls, got %d", len(examples))
	}
}

func TestExtractSubagentExamples_PersonaSystemPrompt(t *testing.T) {
	tests := []struct {
		persona string
		want    string
	}{
		{"coder", subagentDefaultSystem},
		{"", subagentDefaultSystem},
		{"refactor", "You are a refactor assistant."},
		{"reviewer", "You are a reviewer assistant."},
	}
	for _, tt := range tests {
		state := agent.ConversationState{
			SessionID: "test",
			Messages: []api.Message{
				{
					Role: "assistant",
					ToolCalls: []api.ToolCall{
						makeSubagentToolCall("c1", "prompt here", tt.persona),
					},
				},
				{Role: "tool", Content: makeSubagentResult(strings.Repeat("output ", 10)), ToolCallID: "c1"},
			},
		}

		examples := extractSubagentExamples(state)
		if len(examples) != 1 {
			t.Fatalf("persona %q: expected 1 example, got %d", tt.persona, len(examples))
		}
		got := examples[0].Messages[0].Content
		if tt.persona == "" || tt.persona == "coder" {
			if got != subagentDefaultSystem {
				t.Errorf("persona %q: system = %q, want %q", tt.persona, got, subagentDefaultSystem)
			}
		} else {
			if !strings.Contains(got, tt.persona) {
				t.Errorf("persona %q: system should contain persona name, got %q", tt.persona, got)
			}
		}
	}
}

func TestExtractSubagentExamples_Redaction(t *testing.T) {
	// Use a realistic OpenAI API key format that secretdetect catches
	secretKey := "AKIAIOSFODNN7EXAMPLE"
	prompt := "Fix the code with " + secretKey + " key."
	output := strings.Repeat("The "+secretKey+" key was removed. ", 3)

	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{
				Role: "assistant",
				ToolCalls: []api.ToolCall{
					makeSubagentToolCall("c1", prompt, ""),
				},
			},
			{Role: "tool", Content: makeSubagentResult(output), ToolCallID: "c1"},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(examples))
	}
	// Verify the example was built (redaction runs inside extractSubagentExamples)
	if len(examples[0].Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(examples[0].Messages))
	}
}

func TestExtractSubagentExamples_ResultNotJSON(t *testing.T) {
	// When the tool result is not valid JSON, the raw content is used
	// as the output.
	rawOutput := strings.Repeat("This is raw non-JSON output from subagent.", 3)

	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{
				Role: "assistant",
				ToolCalls: []api.ToolCall{
					makeSubagentToolCall("c1", "prompt", ""),
				},
			},
			{Role: "tool", Content: rawOutput, ToolCallID: "c1"},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(examples))
	}
	if examples[0].Messages[2].Content != rawOutput {
		t.Error("raw non-JSON output should be used as-is")
	}
}

func TestExtractSubagentExamples_MultipleSingleSubagents(t *testing.T) {
	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{
				Role: "assistant",
				ToolCalls: []api.ToolCall{
					makeSubagentToolCall("c1", "prompt 1", ""),
				},
			},
			{Role: "tool", Content: makeSubagentResult(strings.Repeat("output 1 ", 10)), ToolCallID: "c1"},
			{
				Role: "assistant",
				ToolCalls: []api.ToolCall{
					makeSubagentToolCall("c2", "prompt 2", ""),
				},
			},
			{Role: "tool", Content: makeSubagentResult(strings.Repeat("output 2 ", 10)), ToolCallID: "c2"},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 2 {
		t.Fatalf("expected 2 examples, got %d", len(examples))
	}
}

func TestExtractSubagentExamples_ParallelWithObjectTasks(t *testing.T) {
	// Tasks as objects with prompt field.
	tasks := []interface{}{
		map[string]interface{}{"id": "custom-1", "prompt": "Object task 1 prompt."},
		map[string]interface{}{"id": "custom-2", "prompt": "Object task 2 prompt."},
	}
	args := map[string]interface{}{"subagents": tasks}
	argsJSON, _ := json.Marshal(args)
	tc := api.ToolCall{ID: "call_1", Type: "function"}
	tc.Function.Name = "run_parallel_subagents"
	tc.Function.Arguments = string(argsJSON)

	outputs := map[string]string{
		"custom-1": strings.Repeat("Output for task 1.", 5),
		"custom-2": strings.Repeat("Output for task 2.", 5),
	}

	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{Role: "assistant", ToolCalls: []api.ToolCall{tc}},
			{Role: "tool", Content: makeParallelSubagentResult(outputs), ToolCallID: "call_1"},
		},
	}

	examples := extractSubagentExamples(state)
	if len(examples) != 2 {
		t.Fatalf("expected 2 examples, got %d", len(examples))
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestParseToolCallArgs(t *testing.T) {
	args := parseToolCallArgs(`{"prompt": "hello", "persona": "coder"}`)
	if args["prompt"] != "hello" {
		t.Errorf("prompt = %v", args["prompt"])
	}
	if args["persona"] != "coder" {
		t.Errorf("persona = %v", args["persona"])
	}
}

func TestParseToolCallArgs_Empty(t *testing.T) {
	args := parseToolCallArgs("")
	if len(args) != 0 {
		t.Errorf("expected empty map, got %d items", len(args))
	}
}

func TestParseToolCallArgs_Invalid(t *testing.T) {
	args := parseToolCallArgs("not json at all")
	if len(args) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %d items", len(args))
	}
}

func TestExtractStringArg(t *testing.T) {
	args := map[string]interface{}{
		"str":    "value",
		"num":    42,
		"absent": nil,
	}
	if got := extractStringArg(args, "str"); got != "value" {
		t.Errorf("str = %q", got)
	}
	if got := extractStringArg(args, "absent"); got != "" {
		t.Errorf("absent = %q, want empty", got)
	}
	if got := extractStringArg(args, "num"); got != "42" {
		t.Errorf("num = %q, want 42", got)
	}
}

func TestExtractTaskPrompts_Strings(t *testing.T) {
	raw := []interface{}{"task a", "task b"}
	prompts := extractTaskPrompts(raw)
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(prompts))
	}
	if prompts[0] != "task a" || prompts[1] != "task b" {
		t.Errorf("prompts = %v", prompts)
	}
}

func TestExtractTaskPrompts_Objects(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"prompt": "obj task 1"},
		map[string]interface{}{"prompt": "obj task 2"},
	}
	prompts := extractTaskPrompts(raw)
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(prompts))
	}
}

func TestExtractTaskPrompts_Invalid(t *testing.T) {
	prompts := extractTaskPrompts("not an array")
	if len(prompts) != 0 {
		t.Errorf("expected empty for non-array, got %v", prompts)
	}
}

func TestExtractSubagentOutput(t *testing.T) {
	got := extractSubagentOutput(makeSubagentResult("hello output"))
	if got != "hello output" {
		t.Errorf("got %q, want 'hello output'", got)
	}
}

func TestExtractSubagentOutput_NonJSON(t *testing.T) {
	got := extractSubagentOutput("raw text output")
	if got != "raw text output" {
		t.Errorf("got %q", got)
	}
}

func TestParseParallelSubagentResult(t *testing.T) {
	result := makeParallelSubagentResult(map[string]string{
		"task-1": "output 1",
		"task-2": "output 2",
	})
	out := parseParallelSubagentResult(result)
	if out["task-1"] != "output 1" {
		t.Errorf("task-1 = %q", out["task-1"])
	}
	if out["task-2"] != "output 2" {
		t.Errorf("task-2 = %q", out["task-2"])
	}
}

func TestParseParallelSubagentResult_Invalid(t *testing.T) {
	out := parseParallelSubagentResult("not json")
	if len(out) != 0 {
		t.Errorf("expected empty map, got %d items", len(out))
	}
}

func TestPersonaToSystemPrompt(t *testing.T) {
	tests := []struct {
		persona string
		want    string
	}{
		{"", subagentDefaultSystem},
		{"coder", subagentDefaultSystem},
		{"CODER", subagentDefaultSystem},
		{"reviewer", "You are a reviewer assistant. Complete the task thoroughly and report your results."},
	}
	for _, tt := range tests {
		got := personaToSystemPrompt(tt.persona)
		if got != tt.want {
			t.Errorf("personaToSystemPrompt(%q) = %q, want %q", tt.persona, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: buildOpenAI with IncludeSubagents
// ---------------------------------------------------------------------------

func TestBuildOpenAI_WithSubagents(t *testing.T) {
	prompt := "Write a comprehensive test suite for the auth module."
	output := strings.Repeat("Here is the comprehensive test suite with unit tests.", 4)

	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{Role: "user", Content: "Run a subagent to write tests"},
			{
				Role:    "assistant",
				Content: "Spawning coder subagent.",
				ToolCalls: []api.ToolCall{
					makeSubagentToolCall("call_1", prompt, "coder"),
				},
			},
			{Role: "tool", Content: makeSubagentResult(output), ToolCallID: "call_1"},
			{Role: "assistant", Content: "The subagent completed writing the test suite."},
		},
	}

	opts := ExportOptions{NoToolResults: true, IncludeSubagents: true}
	examples, err := buildOpenAI([]agent.ConversationState{state}, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 1 conversation example + 1 subagent example = 2.
	if len(examples) != 2 {
		t.Fatalf("expected 2 examples (1 conversation + 1 subagent), got %d", len(examples))
	}

	// The subagent example should be the second one (3 messages).
	subagentEx := examples[1]
	if len(subagentEx.Messages) != 3 {
		t.Fatalf("subagent example should have 3 messages, got %d", len(subagentEx.Messages))
	}
	if subagentEx.Messages[1].Content != prompt {
		t.Error("subagent example user message should match prompt")
	}
}

func TestBuildOpenAI_WithoutSubagentsFlag(t *testing.T) {
	prompt := "Write tests."
	output := strings.Repeat("Here are the tests.", 5)

	state := agent.ConversationState{
		SessionID: "test",
		Messages: []api.Message{
			{Role: "user", Content: "hi"},
			{
				Role:    "assistant",
				Content: "Spawning.",
				ToolCalls: []api.ToolCall{
					makeSubagentToolCall("call_1", prompt, "coder"),
				},
			},
			{Role: "tool", Content: makeSubagentResult(output), ToolCallID: "call_1"},
		},
	}

	// Without IncludeSubagents flag, no subagent examples should be extracted.
	opts := ExportOptions{NoToolResults: true, IncludeSubagents: false}
	examples, err := buildOpenAI([]agent.ConversationState{state}, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the conversation example, no subagent example.
	if len(examples) != 1 {
		t.Fatalf("expected 1 example (conversation only), got %d", len(examples))
	}
}
