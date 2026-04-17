package training

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeToolCall is a test helper that constructs an api.ToolCall.
func makeToolCall(id, name, args string) api.ToolCall {
	tc := api.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

func sampleConversationState(id string) agent.ConversationState {
	return agent.ConversationState{
		SessionID:        id,
		Name:             "test session",
		WorkingDirectory: "/tmp/testdir",
		TotalCost:        0.05,
		TotalTokens:      1000,
		LastUpdated:      time.Now(),
		TaskActions: []agent.TaskAction{
			{Type: "file_created", Description: "Created main.go"},
			{Type: "command_executed", Description: "Ran tests"},
		},
		Messages: []api.Message{
			{Role: "system", Content: "You are a helpful coding assistant."},
			{Role: "user", Content: "Please create a hello world Go program."},
			{
				Role:    "assistant",
				Content: "I'll create a hello world program for you.",
				ToolCalls: []api.ToolCall{
					makeToolCall("call_1", "write_file", `{"path":"main.go","content":"package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"Hello\")}"}`),
				},
			},
			{Role: "tool", Content: "File written successfully.", ToolCallId: "call_1"},
			{
				Role:    "assistant",
				Content: "The file has been created. Let me verify it compiles.",
				ToolCalls: []api.ToolCall{
					makeToolCall("call_2", "shell_command", `{"command":"go build main.go"}`),
				},
			},
			{Role: "tool", Content: "# Output\nBuild successful.", ToolCallId: "call_2"},
			{Role: "assistant", Content: "The program compiles successfully! Here's what was created:\n\n```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```\n\nYou can run it with `go run main.go`."},
		},
	}
}

func emptyConversationState(id string) agent.ConversationState {
	return agent.ConversationState{
		SessionID:   id,
		Name:        "empty",
		LastUpdated: time.Now(),
	}
}

// ---------------------------------------------------------------------------
// validateOptions tests
// ---------------------------------------------------------------------------

func TestValidateOptions_UnsupportedFormat(t *testing.T) {
	err := validateOptions(ExportOptions{Format: "yaml", Output: "/tmp/out.json"})
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Fatalf("error should mention the format: %v", err)
	}
}

func TestValidateOptions_MissingOutput(t *testing.T) {
	err := validateOptions(ExportOptions{Format: "sharegpt", Output: ""})
	if err == nil {
		t.Fatal("expected error for missing output")
	}
}

func TestValidateOptions_NegativeThresholds(t *testing.T) {
	err := validateOptions(ExportOptions{Format: "openai", Output: "/tmp/o.jsonl", MinTurns: -1})
	if err == nil {
		t.Fatal("expected error for negative min-turns")
	}
	err = validateOptions(ExportOptions{Format: "openai", Output: "/tmp/o.jsonl", MinActions: -1})
	if err == nil {
		t.Fatal("expected error for negative min-actions")
	}
}

func TestValidateOptions_Valid(t *testing.T) {
	err := validateOptions(ExportOptions{Format: "alpaca", Output: "/tmp/out.json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// meetsThresholds tests
// ---------------------------------------------------------------------------

func TestMeetsThresholds(t *testing.T) {
	state := sampleConversationState("sess1")

	// sampleConversationState has 1 user-assistant turn (user→assistant with
	// tool calls, then two more assistant-only messages via tool intermediaries)
	// and 2 actions.
	if !meetsThresholds(state, 1, 1) {
		t.Fatal("expected session to pass thresholds")
	}
	if !meetsThresholds(state, 1, 2) {
		t.Fatal("expected session to pass exact action threshold")
	}
	if meetsThresholds(state, 2, 1) {
		t.Fatal("expected session to fail with too many min-turns (only 1 turn)")
	}
	if meetsThresholds(state, 1, 3) {
		t.Fatal("expected session to fail with too many min-actions")
	}

	empty := emptyConversationState("empty")
	if meetsThresholds(empty, 1, 1) {
		t.Fatal("empty session should not pass thresholds")
	}
}

// ---------------------------------------------------------------------------
// countTurns tests
// ---------------------------------------------------------------------------

func TestCountTurns(t *testing.T) {
	tests := []struct {
		name     string
		messages []api.Message
		want     int
	}{
		{
			name:     "empty",
			messages: nil,
			want:     0,
		},
		{
			name: "one turn",
			messages: []api.Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "hello"},
			},
			want: 1,
		},
		{
			name: "two turns",
			messages: []api.Message{
				{Role: "user", Content: "q1"},
				{Role: "assistant", Content: "a1"},
				{Role: "user", Content: "q2"},
				{Role: "assistant", Content: "a2"},
			},
			want: 2,
		},
		{
			name: "assistant without preceding user",
			messages: []api.Message{
				{Role: "assistant", Content: "hello"},
			},
			want: 0,
		},
		{
			name: "user without assistant",
			messages: []api.Message{
				{Role: "user", Content: "hello"},
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countTurns(tt.messages)
			if got != tt.want {
				t.Errorf("countTurns() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// toolCallsToText tests
// ---------------------------------------------------------------------------

func TestToolCallsToText(t *testing.T) {
	tcs := []api.ToolCall{
		makeToolCall("c1", "read_file", `{"path":"/tmp/f.go"}`),
		makeToolCall("c2", "shell_command", `{"command":"go test"}`),
	}

	text := toolCallsToText(tcs, "")
	if !strings.Contains(text, "read_file") {
		t.Error("expected tool name in output")
	}
	if !strings.Contains(text, "shell_command") {
		t.Error("expected second tool name in output")
	}

	// With existing content.
	text2 := toolCallsToText(tcs, "Here is what I'll do:")
	if !strings.HasPrefix(text2, "Here is what I'll do:") {
		t.Error("existing content should come first")
	}
}

func TestToolCallsToText_Single(t *testing.T) {
	tcs := []api.ToolCall{
		makeToolCall("c1", "edit_file", `{"path":"f.go","old_str":"x","new_str":"y"}`),
	}
	text := toolCallsToText(tcs, "")
	if !strings.Contains(text, "edit_file") {
		t.Error("expected tool name")
	}
	// Single tool call should not have trailing newline.
	if strings.HasSuffix(text, "\n") {
		t.Error("single tool call should not end with newline")
	}
}

// ---------------------------------------------------------------------------
// flattenStandardMessages tests
// ---------------------------------------------------------------------------

func TestFlattenStandardMessages_NoToolResults(t *testing.T) {
	opts := ExportOptions{NoToolResults: true, IncludeSystem: false}
	messages := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi", ToolCalls: []api.ToolCall{
			makeToolCall("c1", "read_file", `{"path":"f.go"}`),
		}},
		{Role: "tool", Content: "very long file content here", ToolCallId: "c1"},
		{Role: "assistant", Content: "Here is what I found."},
	}

	result := flattenStandardMessages(messages, opts)

	// System message should be excluded.
	for _, m := range result {
		if m.Role == "system" {
			t.Error("system message should be excluded when IncludeSystem=false")
		}
	}

	// Tool message should be replaced with a placeholder.
	foundPlaceholder := false
	for _, m := range result {
		if m.Role == "tool" && strings.Contains(m.Content, "[tool result:") {
			foundPlaceholder = true
		}
	}
	if !foundPlaceholder {
		t.Error("expected tool result placeholder")
	}

	// Assistant tool-call message should be converted to text.
	foundToolCall := false
	for _, m := range result {
		if m.Role == "assistant" && strings.Contains(m.Content, "Tool call:") {
			foundToolCall = true
		}
	}
	if !foundToolCall {
		t.Error("expected assistant message with 'Tool call:' text")
	}
}

func TestFlattenStandardMessages_KeepToolResults(t *testing.T) {
	opts := ExportOptions{NoToolResults: false, IncludeSystem: false}
	messages := []api.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "tool", Content: "raw tool output data", ToolCallId: "c1"},
	}

	result := flattenStandardMessages(messages, opts)

	// Tool message should retain original content.
	found := false
	for _, m := range result {
		if m.Role == "tool" && m.Content == "raw tool output data" {
			found = true
		}
	}
	if !found {
		t.Error("expected raw tool content to be preserved when NoToolResults=false")
	}
}

func TestFlattenStandardMessages_IncludeSystem(t *testing.T) {
	opts := ExportOptions{NoToolResults: true, IncludeSystem: true}
	messages := []api.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}

	result := flattenStandardMessages(messages, opts)
	foundSystem := false
	for _, m := range result {
		if m.Role == "system" && m.Content == "You are helpful." {
			foundSystem = true
		}
	}
	if !foundSystem {
		t.Error("system message should be included when IncludeSystem=true")
	}
}

func TestDeduplicateConsecutive(t *testing.T) {
	messages := []api.Message{
		{Role: "assistant", Content: "Part 1"},
		{Role: "assistant", Content: "Part 2"},
		{Role: "user", Content: "Question"},
	}

	result := deduplicateConsecutive(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != "assistant" {
		t.Error("first message should be assistant")
	}
	if !strings.Contains(result[0].Content, "Part 1") || !strings.Contains(result[0].Content, "Part 2") {
		t.Error("consecutive assistant messages should be merged")
	}
	if result[1].Role != "user" || result[1].Content != "Question" {
		t.Error("user message should be unchanged")
	}
}

func TestDeduplicateConsecutive_Empty(t *testing.T) {
	if deduplicateConsecutive(nil) != nil {
		t.Error("nil input should return nil")
	}
	if deduplicateConsecutive([]api.Message{}) != nil {
		t.Error("empty input should return nil")
	}
}

// ---------------------------------------------------------------------------
// normalizeRole tests
// ---------------------------------------------------------------------------

func TestNormalizeRole(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"user", "user"},
		{"assistant", "assistant"},
		{"system", "system"},
		{"tool", "user"},
		{"", "user"},
	}
	for _, tt := range tests {
		got := normalizeRole(tt.input)
		if got != tt.want {
			t.Errorf("normalizeRole(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// inferToolName tests
// ---------------------------------------------------------------------------

func TestInferToolName(t *testing.T) {
	if inferToolName(api.Message{Content: "some output"}) != "result" {
		t.Error("non-empty tool message should return 'result'")
	}
	if inferToolName(api.Message{Content: ""}) != "empty" {
		t.Error("empty tool message should return 'empty'")
	}
	if inferToolName(api.Message{Content: "  "}) != "empty" {
		t.Error("whitespace-only tool message should return 'empty'")
	}
}

// ---------------------------------------------------------------------------
// alpacaFromConversation tests
// ---------------------------------------------------------------------------

func TestAlpacaFromConversation(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "Write a Go HTTP server"},
		{Role: "assistant", Content: "Here is a simple server."},
		{Role: "user", Content: "Add logging"},
		{Role: "assistant", Content: "I added logging to the server."},
	}

	example := alpacaFromConversation(messages)
	if example == nil {
		t.Fatal("expected non-nil example")
	}
	if example.Instruction != "Write a Go HTTP server" {
		t.Errorf("instruction = %q, want %q", example.Instruction, "Write a Go HTTP server")
	}
	if example.Output != "I added logging to the server." {
		t.Errorf("output = %q, want %q", example.Output, "I added logging to the server.")
	}
	// Input should contain the intermediate conversation context.
	if !strings.Contains(example.Input, "Here is a simple server.") {
		t.Error("input should contain intermediate assistant response")
	}
	if !strings.Contains(example.Input, "Add logging") {
		t.Error("input should contain second user message")
	}
}

func TestAlpacaFromConversation_SingleTurn(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "2+2 = 4"},
	}
	example := alpacaFromConversation(messages)
	if example == nil {
		t.Fatal("expected non-nil example")
	}
	if example.Instruction != "What is 2+2?" {
		t.Errorf("instruction = %q", example.Instruction)
	}
	if example.Input != "" {
		t.Errorf("input should be empty for single turn, got %q", example.Input)
	}
	if example.Output != "2+2 = 4" {
		t.Errorf("output = %q", example.Output)
	}
}

func TestAlpacaFromConversation_NoUser(t *testing.T) {
	messages := []api.Message{
		{Role: "assistant", Content: "Hello!"},
	}
	if alpacaFromConversation(messages) != nil {
		t.Error("expected nil when no user message")
	}
}

func TestAlpacaFromConversation_NoAssistant(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "user", Content: "Are you there?"},
	}
	result := alpacaFromConversation(msgs)
	if result != nil {
		t.Errorf("expected nil when no assistant messages, got %+v", result)
	}
}

func TestAlpacaFromConversation_WhitespaceOnlyAssistant(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "   \t\n  "},
	}
	if alpacaFromConversation(msgs) != nil {
		t.Error("expected nil for whitespace-only assistant")
	}
}

// ---------------------------------------------------------------------------
// writeJSONArray / writeJSONL tests
// ---------------------------------------------------------------------------

func TestAlpacaFromConversation_TrailingWhitespaceInAssistant(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "  The answer is 4.  \n"},
	}
	example := alpacaFromConversation(msgs)
	if example == nil {
		t.Fatal("expected non-nil example")
	}
	if example.Output != "The answer is 4." {
		t.Errorf("expected output to be trimmed, got %q", example.Output)
	}
	if example.Instruction != "What is 2+2?" {
		t.Errorf("expected instruction to be first user message, got %q", example.Instruction)
	}
	if example.Input != "" {
		t.Errorf("expected empty input (no intermediate messages), got %q", example.Input)
	}
}

func TestAlpacaFromConversation_WhitespaceOnUser(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "  What is 2+2?  \n"},
		{Role: "assistant", Content: "2+2 = 4"},
	}
	example := alpacaFromConversation(msgs)
	if example == nil {
		t.Fatal("expected non-nil example")
	}
	if example.Instruction != "What is 2+2?" {
		t.Errorf("expected trimmed instruction, got %q", example.Instruction)
	}
	if example.Output != "2+2 = 4" {
		t.Errorf("output = %q, want %q", example.Output, "2+2 = 4")
	}
	if example.Input != "" {
		t.Errorf("expected empty input for single-turn, got %q", example.Input)
	}
}

func TestWriteJSONArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	data := []ShareGPTConversation{
		{ID: "s1", Messages: []ShareGPTMessage{{Role: "user", Content: "hi"}}, Metadata: ShareGPTMetadata{Source: "ledit"}},
	}

	if err := writeJSONArray(data, path); err != nil {
		t.Fatalf("writeJSONArray failed: %v", err)
	}

	read, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if !strings.Contains(string(read), `"id": "s1"`) {
		t.Error("output should contain session ID")
	}
	if !strings.Contains(string(read), `"source": "ledit"`) {
		t.Error("output should contain source metadata")
	}
}

func TestWriteJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.jsonl")
	data := []OpenAITrainingExample{
		{Messages: []OpenAIMessage{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}},
		{Messages: []OpenAIMessage{{Role: "user", Content: "bye"}, {Role: "assistant", Content: "goodbye"}}},
	}

	if err := writeJSONL(data, path); err != nil {
		t.Fatalf("writeJSONL failed: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open output: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
		var ex OpenAITrainingExample
		if err := json.Unmarshal(scanner.Bytes(), &ex); err != nil {
			t.Fatalf("invalid JSONL line: %v", err)
		}
		if len(ex.Messages) != 2 {
			t.Errorf("expected 2 messages per example, got %d", len(ex.Messages))
		}
	}
	if count != 2 {
		t.Errorf("expected 2 JSONL lines, got %d", count)
	}
}

func TestWriteJSONL_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := writeJSONL(nil, path); err != nil {
		t.Fatalf("writeJSONL with nil should succeed: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Size() != 0 {
		t.Error("empty JSONL file should be 0 bytes")
	}
}

// ---------------------------------------------------------------------------
// buildShareGPT / buildOpenAI / buildAlpaca tests
// ---------------------------------------------------------------------------

func TestBuildShareGPT(t *testing.T) {
	states := []agent.ConversationState{sampleConversationState("s1")}
	opts := ExportOptions{NoToolResults: true}

	convos, err := buildShareGPT(states, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(convos) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convos))
	}
	if convos[0].ID != "s1" {
		t.Errorf("ID = %q, want %q", convos[0].ID, "s1")
	}
	if convos[0].Metadata.Source != "ledit" {
		t.Error("metadata source should be 'ledit'")
	}
	if convos[0].Metadata.SessionName != "test session" {
		t.Errorf("session name = %q, want %q", convos[0].Metadata.SessionName, "test session")
	}

	// Should contain user and assistant messages but no tool messages.
	userCount, assistantCount := 0, 0
	for _, msg := range convos[0].Messages {
		if msg.Role == "tool" {
			t.Error("tool messages should be filtered from ShareGPT")
		}
		if msg.Role == "user" {
			userCount++
		}
		if msg.Role == "assistant" {
			assistantCount++
		}
	}
	if userCount == 0 {
		t.Error("expected at least one user message")
	}
	if assistantCount == 0 {
		t.Error("expected at least one assistant message")
	}
}

func TestBuildShareGPT_WithSystem(t *testing.T) {
	states := []agent.ConversationState{sampleConversationState("s1")}
	opts := ExportOptions{NoToolResults: true, IncludeSystem: true}

	convos, err := buildShareGPT(states, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(convos) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convos))
	}

	foundSystem := false
	for _, msg := range convos[0].Messages {
		if msg.Role == "system" {
			foundSystem = true
		}
	}
	if !foundSystem {
		t.Error("system message should be included when IncludeSystem=true")
	}
}

func TestBuildOpenAI(t *testing.T) {
	states := []agent.ConversationState{sampleConversationState("s1")}
	opts := ExportOptions{NoToolResults: true}

	examples, err := buildOpenAI(states, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(examples))
	}
	// Messages should not contain tool messages.
	for _, msg := range examples[0].Messages {
		if msg.Role == "tool" {
			t.Error("tool messages should be filtered from OpenAI examples")
		}
	}
}

func TestBuildAlpaca(t *testing.T) {
	states := []agent.ConversationState{sampleConversationState("s1")}
	opts := ExportOptions{NoToolResults: true}

	examples, err := buildAlpaca(states, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(examples))
	}
	if examples[0].Instruction == "" {
		t.Error("alpaca instruction should not be empty")
	}
	if examples[0].Output == "" {
		t.Error("alpaca output should not be empty")
	}
}

func TestBuildAlpaca_EmptyConversation(t *testing.T) {
	states := []agent.ConversationState{emptyConversationState("empty")}
	opts := ExportOptions{NoToolResults: true}

	examples, err := buildAlpaca(states, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(examples) != 0 {
		t.Errorf("expected 0 examples for empty conversation, got %d", len(examples))
	}
}

// ---------------------------------------------------------------------------
// sortSessionsNewestFirst tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------// End-to-end ExportSessions with specific session
// ---------------------------------------------------------------------------

// TestExportSessions_SpecificSession exercises the ExportSessions function by
// relying on the option to target a specific session via --session. If the
// session doesn't exist on the host, SessionsExported will be 0 which is also
// a valid outcome. The test verifies that ExportSessions completes without
// error and returns a well-formed ExportResult.
func TestExportSessions_SpecificSession(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.jsonl")

	// Using a non-existent session ID should result in 0 exported.
	result, err := ExportSessions(ExportOptions{
		Format:     "openai",
		Output:     outPath,
		Session:    "nonexistent_session_xyz",
		MinTurns:   0,
		MinActions: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SessionsScanned != 0 {
		t.Errorf("expected 0 scanned for missing session, got %d", result.SessionsScanned)
	}
	if result.SessionsExported != 0 {
		t.Errorf("expected 0 exported, got %d", result.SessionsExported)
	}
	if result.OutputPath != outPath {
		t.Errorf("output path = %q, want %q", result.OutputPath, outPath)
	}
}

// TestExportSessions_InvalidOptions verifies error returns for bad configs.
func TestExportSessions_InvalidOptions(t *testing.T) {
	dir := t.TempDir()

	_, err := ExportSessions(ExportOptions{
		Format: "badformat",
		Output: filepath.Join(dir, "out.json"),
	})
	if err == nil {
		t.Fatal("expected error for bad format")
	}

	_, err = ExportSessions(ExportOptions{
		Format: "sharegpt",
		Output: "",
	})
	if err == nil {
		t.Fatal("expected error for empty output")
	}
}

// TestExportSessions_WithAllAndMinFilters tests the all-sessions path with
// quality filters. This may or may not find sessions on the host, but it
// should always complete without error.
func TestExportSessions_WithAllAndMinFilters(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "all_out.jsonl")
	stateDir := filepath.Join(dir, "sessions")
	restore := agent.SetStateDirFuncForTesting(func() (string, error) {
		return stateDir, nil
	})
	defer restore()

	workingDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	qualifying := sampleConversationState("qualifying")
	qualifying.WorkingDirectory = workingDir
	for i := 0; i < 8; i++ {
		qualifying.TaskActions = append(qualifying.TaskActions, agent.TaskAction{Type: "file_modified", Description: "extra action"})
	}
	for i := 0; i < 4; i++ {
		qualifying.Messages = append(qualifying.Messages,
			api.Message{Role: "user", Content: "follow-up request"},
			api.Message{Role: "assistant", Content: "follow-up response"},
		)
	}

	filteredByActions := sampleConversationState("filtered-actions")
	filteredByActions.WorkingDirectory = filepath.Join(dir, "workspace-actions")

	filteredByTurns := sampleConversationState("filtered-turns")
	filteredByTurns.WorkingDirectory = filepath.Join(dir, "workspace-turns")
	for i := 0; i < 8; i++ {
		filteredByTurns.TaskActions = append(filteredByTurns.TaskActions, agent.TaskAction{Type: "file_modified", Description: "extra action"})
	}
	filteredByTurns.Messages = filteredByTurns.Messages[:4]

	for _, state := range []agent.ConversationState{qualifying, filteredByActions, filteredByTurns} {
		if err := os.MkdirAll(state.WorkingDirectory, 0o755); err != nil {
			t.Fatalf("mkdir state workspace: %v", err)
		}
		if err := agent.WriteTestSessionFile(stateDir, state.SessionID, state.WorkingDirectory, &state); err != nil {
			t.Fatalf("WriteTestSessionFile(%s): %v", state.SessionID, err)
		}
	}

	result, err := ExportSessions(ExportOptions{
		Format:        "openai",
		Output:        outPath,
		All:           true,
		MinTurns:      5,
		MinActions:    10,
		NoToolResults: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SessionsScanned != 3 {
		t.Fatalf("expected 3 scanned sessions, got %d", result.SessionsScanned)
	}
	if result.SessionsExported != 1 {
		t.Fatalf("expected 1 exported session, got %d", result.SessionsExported)
	}
	if result.SessionsFiltered != 2 {
		t.Fatalf("expected 2 filtered sessions, got %d", result.SessionsFiltered)
	}
	if result.OutputPath != outPath {
		t.Errorf("output path mismatch")
	}
}

// ---------------------------------------------------------------------------
// Full pipeline test: build + write for all formats
// ---------------------------------------------------------------------------

func TestFullPipeline_AllFormats(t *testing.T) {
	states := []agent.ConversationState{
		sampleConversationState("s1"),
		sampleConversationState("s2"),
	}
	opts := ExportOptions{NoToolResults: true}

	dir := t.TempDir()

	// ShareGPT
	sharegptPath := filepath.Join(dir, "sharegpt.json")
	convos, err := buildShareGPT(states, opts)
	if err != nil {
		t.Fatalf("buildShareGPT: %v", err)
	}
	if len(convos) != 2 {
		t.Fatalf("expected 2 ShareGPT conversations, got %d", len(convos))
	}
	if err := writeJSONArray(convos, sharegptPath); err != nil {
		t.Fatalf("writeJSONArray: %v", err)
	}
	data, err := os.ReadFile(sharegptPath)
	if err != nil {
		t.Fatalf("read sharegpt: %v", err)
	}
	// Verify it's valid JSON containing 2 conversations.
	var parsed []ShareGPTConversation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid ShareGPT JSON: %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("parsed %d conversations, want 2", len(parsed))
	}

	// OpenAI
	openaiPath := filepath.Join(dir, "openai.jsonl")
	examples, err := buildOpenAI(states, opts)
	if err != nil {
		t.Fatalf("buildOpenAI: %v", err)
	}
	if len(examples) != 2 {
		t.Fatalf("expected 2 OpenAI examples, got %d", len(examples))
	}
	if err := writeJSONL(examples, openaiPath); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}
	f, err := os.Open(openaiPath)
	if err != nil {
		t.Fatalf("open openai: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}
	if lineCount != 2 {
		t.Errorf("expected 2 JSONL lines, got %d", lineCount)
	}

	// Alpaca
	alpacaPath := filepath.Join(dir, "alpaca.json")
	alpacaExamples, err := buildAlpaca(states, opts)
	if err != nil {
		t.Fatalf("buildAlpaca: %v", err)
	}
	if len(alpacaExamples) != 2 {
		t.Fatalf("expected 2 Alpaca examples, got %d", len(alpacaExamples))
	}
	if err := writeJSONArray(alpacaExamples, alpacaPath); err != nil {
		t.Fatalf("writeJSONArray alpaca: %v", err)
	}
	alpacaData, err := os.ReadFile(alpacaPath)
	if err != nil {
		t.Fatalf("read alpaca: %v", err)
	}
	var alpacaParsed []AlpacaExample
	if err := json.Unmarshal(alpacaData, &alpacaParsed); err != nil {
		t.Fatalf("invalid Alpaca JSON: %v", err)
	}
	if len(alpacaParsed) != 2 {
		t.Errorf("parsed %d alpaca examples, want 2", len(alpacaParsed))
	}
}
