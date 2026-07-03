//go:build !js

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/testutil"
)

// =============================================================================
// AgentResultMetrics JSON serialization tests
// =============================================================================

func TestAgentResultMetrics_Serialization_AllFields(t *testing.T) {
	m := AgentResultMetrics{
		ElapsedSeconds: 12.5,
		TokensIn:       1500,
		TokensOut:      800,
		LLMCalls:       5,
		Provider:       "openai",
		Model:          "gpt-4o",
		// Security telemetry fields (default zero values)
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal AgentResultMetrics: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal marshaled metrics: %v", err)
	}

	// Verify every expected key is present with the correct value
	assertJSONFloat(t, result, "elapsed_seconds", 12.5)
	assertJSONInt(t, result, "tokens_in", 1500)
	assertJSONInt(t, result, "tokens_out", 800)
	assertJSONInt(t, result, "llm_calls", 5)
	assertJSONString(t, result, "provider", "openai")
	assertJSONString(t, result, "model", "gpt-4o")

	// Ensure exactly these keys and no extras
	expectedKeys := map[string]bool{
		"elapsed_seconds":                true,
		"tokens_in":                      true,
		"tokens_out":                     true,
		"llm_calls":                      true,
		"cost":                           true,
		"provider":                       true,
		"model":                          true,
		"security_cautions_issued":       true,
		"security_retries_after_caution": true,
		"security_loops_detected":        true,
	}
	for k := range result {
		if !expectedKeys[k] {
			t.Errorf("unexpected key in serialized metrics: %q", k)
		}
	}
	if len(result) != len(expectedKeys) {
		t.Errorf("expected %d keys, got %d", len(expectedKeys), len(result))
	}
}

func TestAgentResultMetrics_Serialization_ZeroValues(t *testing.T) {
	m := AgentResultMetrics{}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal zero-value AgentResultMetrics: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// All zero values should still be serialized (no omitempty on metrics fields)
	assertJSONFloat(t, result, "elapsed_seconds", 0.0)
	assertJSONInt(t, result, "tokens_in", 0)
	assertJSONInt(t, result, "tokens_out", 0)
	assertJSONInt(t, result, "llm_calls", 0)
	assertJSONString(t, result, "provider", "")
	assertJSONString(t, result, "model", "")
}

func TestAgentResultMetrics_Serialization_RoundTrip(t *testing.T) {
	original := AgentResultMetrics{
		ElapsedSeconds: 99.99,
		TokensIn:       100000,
		TokensOut:      50000,
		LLMCalls:       42,
		Provider:       "anthropic",
		Model:          "claude-3-opus-20240229",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored AgentResultMetrics
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored != original {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", restored, original)
	}
}

func TestAgentResultMetrics_Serialization_LargeValues(t *testing.T) {
	m := AgentResultMetrics{
		ElapsedSeconds: 3600.0,
		TokensIn:       2000000,
		TokensOut:      1000000,
		LLMCalls:       500,
		Provider:       "deepinfra",
		Model:          "meta-llama/Meta-Llama-3.1-405B-Instruct",
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored AgentResultMetrics
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored != m {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", restored, m)
	}
}

// =============================================================================
// AgentResult JSON serialization tests
// =============================================================================

func TestAgentResult_Serialization_SuccessMinimal(t *testing.T) {
	r := AgentResult{
		Status: "success",
		Query:  "fix the bug in main.go",
		Metrics: AgentResultMetrics{
			ElapsedSeconds: 5.0,
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	assertJSONString(t, result, "status", "success")
	assertJSONString(t, result, "query", "fix the bug in main.go")

	// "error" should be omitted when empty (omitempty)
	if _, exists := result["error"]; exists {
		t.Error("expected 'error' key to be omitted for success result, but it was present")
	}

	// "files_modified" should be omitted when nil/empty
	if _, exists := result["files_modified"]; exists {
		t.Error("expected 'files_modified' to be omitted when empty")
	}

	// "git_diff" should be omitted when empty
	if _, exists := result["git_diff"]; exists {
		t.Error("expected 'git_diff' to be omitted when empty")
	}

	// "metrics" must always be present
	if _, exists := result["metrics"]; !exists {
		t.Error("expected 'metrics' key to always be present")
	}
}

func TestAgentResult_Serialization_ErrorWithAllFields(t *testing.T) {
	r := AgentResult{
		Status:        "error",
		Error:         "connection timed out",
		Query:         "refactor the module",
		FilesModified: []string{"main.go", "utils.go"},
		GitDiff:       "diff --git a/main.go\n--- a/main.go\n+++ b/main.go",
		Metrics: AgentResultMetrics{
			ElapsedSeconds: 3.14,
			TokensIn:       500,
			TokensOut:      200,
			LLMCalls:       2,
			Provider:       "anthropic",
			Model:          "claude-3-sonnet",
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	assertJSONString(t, result, "status", "error")
	assertJSONString(t, result, "error", "connection timed out")
	assertJSONString(t, result, "query", "refactor the module")
	assertJSONString(t, result, "git_diff", "diff --git a/main.go\n--- a/main.go\n+++ b/main.go")

	filesRaw, ok := result["files_modified"]
	if !ok {
		t.Fatal("expected 'files_modified' key to be present")
	}
	files, ok := filesRaw.([]interface{})
	if !ok {
		t.Fatalf("expected files_modified to be array, got %T", filesRaw)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].(string) != "main.go" || files[1].(string) != "utils.go" {
		t.Errorf("unexpected files: %v", files)
	}
}

func TestAgentResult_Serialization_RoundTrip(t *testing.T) {
	original := AgentResult{
		Status:        "success",
		Query:         "add unit tests",
		FilesModified: []string{"foo_test.go"},
		GitDiff:       "diff content here",
		Metrics: AgentResultMetrics{
			ElapsedSeconds: 10.5,
			TokensIn:       2000,
			TokensOut:      1500,
			LLMCalls:       3,
			Provider:       "openai",
			Model:          "gpt-4o",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored AgentResult
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.Status != original.Status {
		t.Errorf("Status: got %q, want %q", restored.Status, original.Status)
	}
	if restored.Query != original.Query {
		t.Errorf("Query: got %q, want %q", restored.Query, original.Query)
	}
	if restored.GitDiff != original.GitDiff {
		t.Errorf("GitDiff: got %q, want %q", restored.GitDiff, original.GitDiff)
	}
	if len(restored.FilesModified) != len(original.FilesModified) {
		t.Errorf("FilesModified length: got %d, want %d", len(restored.FilesModified), len(original.FilesModified))
	} else {
		for i, f := range restored.FilesModified {
			if f != original.FilesModified[i] {
				t.Errorf("FilesModified[%d]: got %q, want %q", i, f, original.FilesModified[i])
			}
		}
	}
	if restored.Metrics != original.Metrics {
		t.Errorf("Metrics: got %+v, want %+v", restored.Metrics, original.Metrics)
	}
}

func TestAgentResult_Serialization_FilesModifiedEmptySlice(t *testing.T) {
	r := AgentResult{
		Status:        "success",
		Query:         "test",
		FilesModified: []string{}, // empty but non-nil
		Metrics:       AgentResultMetrics{},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// An empty non-nil slice should still be serialized (as []) because
	// omitempty considers both nil and empty slices as empty.
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// With omitempty, empty slice is omitted
	if _, exists := result["files_modified"]; exists {
		// This is acceptable either way — both behaviors are fine.
		// Just log it for documentation purposes.
		t.Log("files_modified present with empty slice (json.Marshal behavior)")
	}
}

func TestAgentResult_Serialization_BackwardCompatibility(t *testing.T) {
	// Verify that the JSON structure is stable: all known keys exist
	// and new fields can be added without breaking existing consumers.
	r := AgentResult{
		Status: "success",
		Query:  "compatibility test",
		Metrics: AgentResultMetrics{
			ElapsedSeconds: 1.0,
			TokensIn:       100,
			TokensOut:      50,
			LLMCalls:       1,
			Provider:       "openai",
			Model:          "gpt-4o",
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Parse as generic map to verify structure
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	// These top-level keys must always exist
	requiredKeys := []string{"status", "query", "metrics"}
	for _, k := range requiredKeys {
		if _, exists := raw[k]; !exists {
			t.Errorf("missing required key %q in JSON output", k)
		}
	}

	// Parse metrics sub-object
	var metricsRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw["metrics"], &metricsRaw); err != nil {
		t.Fatalf("unmarshal metrics: %v", err)
	}

	// All metric fields must always be present (no omitempty on metrics)
	requiredMetricKeys := []string{
		"elapsed_seconds", "tokens_in", "tokens_out",
		"llm_calls", "provider", "model",
	}
	for _, k := range requiredMetricKeys {
		if _, exists := metricsRaw[k]; !exists {
			t.Errorf("missing required metrics key %q", k)
		}
	}
}

// =============================================================================
// emitJSONResult integration tests
// =============================================================================

func TestEmitJSONResult_SuccessWithAgent(t *testing.T) {
	a := createTestAgent(t)
	defer a.Shutdown()

	// Populate metrics via TrackMetricsFromResponse (the only public setter)
	a.TrackMetricsFromResponse(1000, 500, 1500, 0.05, 0, 0)

	query := "write a hello world program"
	startTime := time.Now().Add(-5 * time.Second)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult(query, startTime, nil, a)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput was:\n%s", err, output)
	}

	if result.Status != "success" {
		t.Errorf("Status = %q, want %q", result.Status, "success")
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
	if result.Query != query {
		t.Errorf("Query = %q, want %q", result.Query, query)
	}
	if result.Metrics.TokensIn != 1000 {
		t.Errorf("TokensIn = %d, want 1000", result.Metrics.TokensIn)
	}
	if result.Metrics.TokensOut != 500 {
		t.Errorf("TokensOut = %d, want 500", result.Metrics.TokensOut)
	}
	if result.Metrics.LLMCalls != 1 {
		t.Errorf("LLMCalls = %d, want 1 (one TrackMetricsFromResponse call)", result.Metrics.LLMCalls)
	}
	if result.Metrics.ElapsedSeconds < 4.9 {
		t.Errorf("ElapsedSeconds = %f, want at least ~5.0", result.Metrics.ElapsedSeconds)
	}
	// Provider and Model should be non-empty for a real agent
	if result.Metrics.Provider == "" {
		t.Error("Provider should not be empty when agent is provided")
	}
	if result.Metrics.Model == "" {
		t.Error("Model should not be empty when agent is provided")
	}
}

func TestEmitJSONResult_SuccessWithAccumulatedMetrics(t *testing.T) {
	a := createTestAgent(t)
	defer a.Shutdown()

	// Simulate multiple API calls accumulating metrics
	a.TrackMetricsFromResponse(500, 200, 700, 0.02, 0, 0)
	a.TrackMetricsFromResponse(600, 300, 900, 0.03, 0, 0)
	a.TrackMetricsFromResponse(400, 100, 500, 0.01, 0, 0)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("test query", time.Now(), nil, a)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Verify accumulated token counts
	if result.Metrics.TokensIn != 1500 { // 500+600+400
		t.Errorf("TokensIn = %d, want 1500", result.Metrics.TokensIn)
	}
	if result.Metrics.TokensOut != 600 { // 200+300+100
		t.Errorf("TokensOut = %d, want 600", result.Metrics.TokensOut)
	}
	if result.Metrics.LLMCalls != 3 { // Three TrackMetricsFromResponse calls
		t.Errorf("LLMCalls = %d, want 3", result.Metrics.LLMCalls)
	}
}

func TestEmitJSONResult_ErrorCase(t *testing.T) {
	a := createTestAgent(t)
	defer a.Shutdown()

	testErr := errors.New("API rate limit exceeded")

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("refactor code", time.Now(), testErr, a)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
	if result.Error != "API rate limit exceeded" {
		t.Errorf("Error = %q, want %q", result.Error, "API rate limit exceeded")
	}
	if result.Query != "refactor code" {
		t.Errorf("Query = %q, want %q", result.Query, "refactor code")
	}
	// Metrics should still be populated even on error
	if result.Metrics.TokensIn != 0 {
		t.Errorf("TokensIn = %d, want 0 (no calls made before error)", result.Metrics.TokensIn)
	}
}

func TestEmitJSONResult_NilAgent(t *testing.T) {
	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("test with nil agent", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.Status != "success" {
		t.Errorf("Status = %q, want %q", result.Status, "success")
	}
	if result.Query != "test with nil agent" {
		t.Errorf("Query = %q, want %q", result.Query, "test with nil agent")
	}
	// All metric values should be zero when agent is nil
	if result.Metrics.TokensIn != 0 {
		t.Errorf("TokensIn = %d, want 0", result.Metrics.TokensIn)
	}
	if result.Metrics.TokensOut != 0 {
		t.Errorf("TokensOut = %d, want 0", result.Metrics.TokensOut)
	}
	if result.Metrics.LLMCalls != 0 {
		t.Errorf("LLMCalls = %d, want 0", result.Metrics.LLMCalls)
	}
	if result.Metrics.Provider != "" {
		t.Errorf("Provider = %q, want empty", result.Metrics.Provider)
	}
	if result.Metrics.Model != "" {
		t.Errorf("Model = %q, want empty", result.Metrics.Model)
	}
}

func TestEmitJSONResult_NilAgentWithError(t *testing.T) {
	testErr := fmt.Errorf("agent initialization failed: no API key")

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("broken query", time.Now(), testErr, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
	if result.Error != "agent initialization failed: no API key" {
		t.Errorf("Error = %q, want %q", result.Error, "agent initialization failed: no API key")
	}
	// Metrics should all be zero
	if result.Metrics.TokensIn != 0 {
		t.Errorf("TokensIn = %d, want 0", result.Metrics.TokensIn)
	}
	if result.Metrics.LLMCalls != 0 {
		t.Errorf("LLMCalls = %d, want 0", result.Metrics.LLMCalls)
	}
}

func TestEmitJSONResult_QueryPreserved(t *testing.T) {
	testCases := []string{
		"simple query",
		"query with \"quotes\" and \\backslashes\\",
		"query with\nnewlines\nand\ttabs",
		"unicode: 日本語 中文 한국어 🚀",
		"",                         // empty query
		strings.Repeat("a", 10000), // long query
	}

	for _, query := range testCases {
		t.Run(fmt.Sprintf("query_len_%d", len(query)), func(t *testing.T) {
			output := testutil.CaptureStdout(t, func() {
				emitJSONResult(query, time.Now(), nil, nil)
			})

			var result AgentResult
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				truncatedQuery := query[:min(len(query), 50)]
				truncatedOutput := output[:min(len(output), 200)]
				t.Fatalf("failed to parse JSON for query %q: %v\noutput: %s", truncatedQuery, err, truncatedOutput)
			}
			if result.Query != query {
				truncatedGot := result.Query[:min(len(result.Query), 100)]
				truncatedWant := query[:min(len(query), 100)]
				t.Errorf("Query mismatch:\n  got:  %q\n  want: %q", truncatedGot, truncatedWant)
			}
		})
	}
}

func TestEmitJSONResult_ElapsedSeconds(t *testing.T) {
	// Verify elapsed time is computed from the provided startTime
	startTime := time.Now().Add(-10 * time.Second)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("timing test", startTime, nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Allow some tolerance for test execution
	if result.Metrics.ElapsedSeconds < 9.5 {
		t.Errorf("ElapsedSeconds = %f, want approximately 10.0", result.Metrics.ElapsedSeconds)
	}
	if result.Metrics.ElapsedSeconds > 15.0 {
		t.Errorf("ElapsedSeconds = %f, unexpectedly large", result.Metrics.ElapsedSeconds)
	}
}

func TestEmitJSONResult_OutputIsValidJSON(t *testing.T) {
	// Ensure the output is always valid JSON regardless of input
	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("validate JSON", time.Now(), nil, nil)
	})

	// Should parse without error
	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, output)
	}
}

func TestEmitJSONResult_Indentation(t *testing.T) {
	// Verify the output uses indented formatting (as set by enc.SetIndent)
	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("indent test", time.Now(), nil, nil)
	})

	// Indented JSON should contain newlines within the object
	if !strings.Contains(output, "  ") {
		t.Error("expected indented JSON output (with leading spaces)")
	}
	// Should have proper line breaks between fields
	if !strings.Contains(output, "\n") {
		t.Error("expected multi-line JSON output")
	}
}

// =============================================================================
// HEAD-fallback integration tests (fresh repos with no commits)
// =============================================================================

func TestEmitJSONResult_FreshRepoNoHEAD_StagedAndUnstaged(t *testing.T) {
	// Create a temp directory with git init (no commits = no HEAD)
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create a file and stage it
	filePath := dir + "/hello.txt"
	if err := os.WriteFile(filePath, []byte("version 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "hello.txt")

	// Now modify it to create unstaged changes on top of the staged version
	if err := os.WriteFile(filePath, []byte("version 2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Change working directory to the temp repo
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// Call emitJSONResult and capture output
	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("fresh repo test", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}

	// GitDiff should be non-empty (staged + unstaged combined)
	if result.GitDiff == "" {
		t.Error("expected non-empty GitDiff for fresh repo with staged and unstaged changes")
	}

	// FilesModified should contain the modified file
	if len(result.FilesModified) == 0 {
		t.Fatal("expected FilesModified to contain hello.txt, got empty/nil")
	}
	found := false
	for _, f := range result.FilesModified {
		if f == "hello.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected FilesModified to contain %q, got %v", "hello.txt", result.FilesModified)
	}
}

func TestEmitJSONResult_FreshRepoNoHEAD_StagedOnly(t *testing.T) {
	// Create a temp directory with git init (no commits)
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create a file and stage it (staged only, no further modifications)
	filePath := dir + "/staged_only.go"
	if err := os.WriteFile(filePath, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "staged_only.go")

	// Change working directory to the temp repo
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("staged only test", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}

	// GitDiff should contain the staged diff
	if result.GitDiff == "" {
		t.Error("expected non-empty GitDiff for fresh repo with staged changes")
	}

	// FilesModified should contain the file
	if len(result.FilesModified) == 0 {
		t.Fatal("expected FilesModified to contain staged_only.go, got empty/nil")
	}
	found := false
	for _, f := range result.FilesModified {
		if f == "staged_only.go" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected FilesModified to contain %q, got %v", "staged_only.go", result.FilesModified)
	}
}

func TestEmitJSONResult_FreshRepoNoHEAD_Deduplication(t *testing.T) {
	// Create a temp directory with git init (no commits)
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create a file, stage it, then modify it again.
	// This means the file appears in both staged and unstaged diffs.
	filePath := dir + "/dedup.txt"
	if err := os.WriteFile(filePath, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "dedup.txt")

	// Modify the file after staging to create unstaged changes
	if err := os.WriteFile(filePath, []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Change working directory to the temp repo
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("dedup test", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}

	// Verify the file appears exactly once in FilesModified
	count := 0
	for _, f := range result.FilesModified {
		if f == "dedup.txt" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected dedup.txt to appear exactly once in FilesModified, got %d occurrences: %v", count, result.FilesModified)
	}
}

func TestEmitJSONResult_FreshRepoNoHEAD_EmptyWorktree(t *testing.T) {
	// Create a temp directory with git init (no commits, no files)
	dir := t.TempDir()
	runGit(t, dir, "init")

	// Change working directory to the empty repo
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("empty worktree test", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}

	// GitDiff should be empty
	if result.GitDiff != "" {
		t.Errorf("expected empty GitDiff for empty worktree, got: %q", result.GitDiff)
	}

	// FilesModified should be empty/nil
	if len(result.FilesModified) != 0 {
		t.Errorf("expected empty FilesModified for empty worktree, got: %v", result.FilesModified)
	}
}

// runGit is a test helper that runs a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed in %s: %v\noutput: %s", strings.Join(args, " "), dir, err, string(out))
	}
}

// =============================================================================
// Untracked files tests
// =============================================================================

func TestEmitJSONResult_UntrackedFilesIncluded(t *testing.T) {
	// Fresh repo (no HEAD) with an untracked file
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create an untracked file (NOT staged)
	if err := os.WriteFile(dir+"/newfile.txt", []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("untracked test", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}

	found := false
	for _, f := range result.FilesModified {
		if f == "newfile.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected FilesModified to contain untracked file %q, got %v", "newfile.txt", result.FilesModified)
	}
}

func TestEmitJSONResult_UntrackedFilesDiff(t *testing.T) {
	// Fresh repo (no HEAD) with an untracked file — diff should show its content
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	content := "line one\nline two\n"
	if err := os.WriteFile(dir+"/untracked.go", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("untracked diff test", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}

	if result.GitDiff == "" {
		t.Fatal("expected non-empty GitDiff for untracked file")
	}
	// The diff should show the file content as additions
	if !strings.Contains(result.GitDiff, "line one") {
		t.Errorf("expected GitDiff to contain untracked file content 'line one', got:\n%s", result.GitDiff)
	}
}

func TestEmitJSONResult_UntrackedFilesNotDuplicate(t *testing.T) {
	// Staged file + untracked file — each should appear exactly once
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create and stage a file
	if err := os.WriteFile(dir+"/staged.txt", []byte("staged content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "staged.txt")

	// Create an untracked file
	if err := os.WriteFile(dir+"/untracked.txt", []byte("untracked content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("dedup untracked test", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}

	// Count occurrences of each file
	counts := make(map[string]int)
	for _, f := range result.FilesModified {
		counts[f]++
	}
	for name, count := range counts {
		if count != 1 {
			t.Errorf("expected %q to appear exactly once in FilesModified, got %d occurrences: %v", name, count, result.FilesModified)
		}
	}

	// Both files should be present
	if counts["staged.txt"] != 1 {
		t.Errorf("expected staged.txt to appear once, got %d", counts["staged.txt"])
	}
	if counts["untracked.txt"] != 1 {
		t.Errorf("expected untracked.txt to appear once, got %d", counts["untracked.txt"])
	}
}

func TestEmitJSONResult_TrackedRepoUntrackedFiles(t *testing.T) {
	// Repo WITH a commit (HEAD exists) + an untracked file
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create initial commit so HEAD exists
	if err := os.WriteFile(dir+"/initial.txt", []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "initial.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Create an untracked file
	if err := os.WriteFile(dir+"/new_untracked.txt", []byte("new stuff\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("tracked repo untracked test", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}

	found := false
	for _, f := range result.FilesModified {
		if f == "new_untracked.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected FilesModified to include untracked file %q, got %v", "new_untracked.txt", result.FilesModified)
	}

	// Untracked file content should also appear in GitDiff
	if result.GitDiff == "" {
		t.Error("expected non-empty GitDiff for repo with untracked files")
	} else if !strings.Contains(result.GitDiff, "new stuff") {
		t.Errorf("expected GitDiff to contain untracked file content 'new stuff', got:\n%s", result.GitDiff)
	}
}

func TestEmitJSONResult_GitignoredFilesExcluded(t *testing.T) {
	// Untracked files that match .gitignore should NOT appear
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create .gitignore
	if err := os.WriteFile(dir+"/.gitignore", []byte("*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create both a gitignored file and a regular file
	if err := os.WriteFile(dir+"/debug.log", []byte("log data\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/regular.txt", []byte("regular content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	output := testutil.CaptureStdout(t, func() {
		emitJSONResult("gitignore test", time.Now(), nil, nil)
	})

	var result AgentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}

	for _, f := range result.FilesModified {
		if f == "debug.log" {
			t.Errorf("gitignored file %q should not appear in FilesModified: %v", "debug.log", result.FilesModified)
		}
	}

	// Regular file should be present
	found := false
	for _, f := range result.FilesModified {
		if f == "regular.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected FilesModified to include %q, got %v", "regular.txt", result.FilesModified)
	}
}

// =============================================================================
// Backward compatibility: verify JSON field names are stable
// =============================================================================

func TestAgentResult_BackwardCompat_FieldNames(t *testing.T) {
	// This test locks in the exact JSON field names so that any renaming
	// is caught as a test failure (breaking change for consumers).
	r := AgentResult{
		Status:        "success",
		Error:         "",
		Query:         "test",
		FilesModified: []string{"a.go"},
		GitDiff:       "diff",
		Metrics: AgentResultMetrics{
			ElapsedSeconds: 1.0,
			TokensIn:       1,
			TokensOut:      1,
			LLMCalls:       1,
			Provider:       "test",
			Model:          "test",
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Map from Go field name to expected JSON key
	expectedKeys := map[string]string{
		"Status":        "status",
		"Error":         "error", // present here because we set it explicitly even though empty
		"Query":         "query",
		"FilesModified": "files_modified",
		"GitDiff":       "git_diff",
		"Metrics":       "metrics",
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for goField, jsonKey := range expectedKeys {
		if goField == "Error" {
			// Error has omitempty; we set empty string so it may be omitted.
			// This is fine — just skip the check for empty Error.
			continue
		}
		if _, exists := raw[jsonKey]; !exists {
			t.Errorf("Go field %q: expected JSON key %q not found", goField, jsonKey)
		}
	}
}

func TestAgentResultMetrics_BackwardCompat_FieldNames(t *testing.T) {
	// Lock in exact JSON field names for metrics sub-struct
	expectedKeys := map[string]string{
		"ElapsedSeconds": "elapsed_seconds",
		"TokensIn":       "tokens_in",
		"TokensOut":      "tokens_out",
		"LLMCalls":       "llm_calls",
		"Provider":       "provider",
		"Model":          "model",
	}

	m := AgentResultMetrics{
		ElapsedSeconds: 1.0,
		TokensIn:       1,
		TokensOut:      1,
		LLMCalls:       1,
		Provider:       "test",
		Model:          "test",
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for goField, jsonKey := range expectedKeys {
		if _, exists := raw[jsonKey]; !exists {
			t.Errorf("Go field %q: expected JSON key %q not found", goField, jsonKey)
		}
	}
}

// =============================================================================
// Test helpers
// =============================================================================

// createTestAgent creates a minimal agent suitable for testing in the cmd package.
// It uses agent.NewAgentWithModel which auto-selects the test provider under go test.
func createTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	a, err := agent.NewAgentWithModel("test:test")
	if err != nil {
		t.Fatalf("failed to create test agent: %v", err)
	}
	return a
}

// assertJSONString checks that the map contains the given key with the expected string value.
func assertJSONString(t *testing.T, m map[string]interface{}, key, expected string) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("key %q not found in JSON", key)
		return
	}
	s, ok := v.(string)
	if !ok {
		t.Errorf("key %q: expected string, got %T", key, v)
		return
	}
	if s != expected {
		t.Errorf("key %q: got %q, want %q", key, s, expected)
	}
}

// assertJSONFloat checks that the map contains the given key with the expected float64 value.
func assertJSONFloat(t *testing.T, m map[string]interface{}, key string, expected float64) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("key %q not found in JSON", key)
		return
	}
	f, ok := v.(float64)
	if !ok {
		t.Errorf("key %q: expected float64, got %T", key, v)
		return
	}
	if f != expected {
		t.Errorf("key %q: got %f, want %f", key, f, expected)
	}
}

// assertJSONInt checks that the map contains the given key with the expected integer value.
// JSON numbers are decoded as float64, so we check for float64 and compare.
func assertJSONInt(t *testing.T, m map[string]interface{}, key string, expected int) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("key %q not found in JSON", key)
		return
	}
	f, ok := v.(float64)
	if !ok {
		t.Errorf("key %q: expected numeric value, got %T", key, v)
		return
	}
	if int(f) != expected {
		t.Errorf("key %q: got %d, want %d", key, int(f), expected)
	}
}
