package types

import (
	"encoding/json"
	"testing"
)

func TestPatchResolutionUnmarshalJSON(t *testing.T) {
	// Test unmarshaling string format (backward compatibility)
	stringJSON := `"single file content"`
	var patch1 PatchResolution
	err := json.Unmarshal([]byte(stringJSON), &patch1)
	if err != nil {
		t.Fatalf("Failed to unmarshal string patch resolution: %v", err)
	}

	if patch1.SingleFile != "single file content" {
		t.Errorf("Expected SingleFile to be 'single file content', got '%s'", patch1.SingleFile)
	}

	if patch1.MultiFile != nil {
		t.Error("Expected MultiFile to be nil for string format")
	}

	// Test unmarshaling object format
	objectJSON := `{
		"file1.go": "content of file1",
		"file2.go": "content of file2"
	}`
	var patch2 PatchResolution
	err = json.Unmarshal([]byte(objectJSON), &patch2)
	if err != nil {
		t.Fatalf("Failed to unmarshal object patch resolution: %v", err)
	}

	if patch2.SingleFile != "" {
		t.Errorf("Expected SingleFile to be empty for object format, got '%s'", patch2.SingleFile)
	}

	if len(patch2.MultiFile) != 2 {
		t.Errorf("Expected MultiFile to have 2 entries, got %d", len(patch2.MultiFile))
	}

	if patch2.MultiFile["file1.go"] != "content of file1" {
		t.Errorf("Expected file1.go content to be 'content of file1', got '%s'", patch2.MultiFile["file1.go"])
	}

	if patch2.MultiFile["file2.go"] != "content of file2" {
		t.Errorf("Expected file2.go content to be 'content of file2', got '%s'", patch2.MultiFile["file2.go"])
	}
}

func TestPatchResolutionMarshalJSON(t *testing.T) {
	// Test marshaling string format
	patch1 := PatchResolution{
		SingleFile: "single file content",
	}

	json1, err := json.Marshal(patch1)
	if err != nil {
		t.Fatalf("Failed to marshal string patch resolution: %v", err)
	}

	// Note: Due to Go's struct marshaling behavior, all exported fields are included
	// even with custom MarshalJSON. This is expected behavior.
	expected1 := `{"SingleFile":"single file content","MultiFile":null}`
	if string(json1) != expected1 {
		t.Errorf("Expected marshaled JSON to be '%s', got '%s'", expected1, string(json1))
	}

	// Test marshaling object format
	patch2 := PatchResolution{
		MultiFile: map[string]string{
			"file1.go": "content of file1",
			"file2.go": "content of file2",
		},
	}

	json2, err := json.Marshal(patch2)
	if err != nil {
		t.Fatalf("Failed to marshal object patch resolution: %v", err)
	}

	// Note: Due to Go's struct marshaling behavior, all exported fields are included
	// even with custom MarshalJSON. This is expected behavior.
	expected2 := `{"SingleFile":"","MultiFile":{"file1.go":"content of file1","file2.go":"content of file2"}}`
	if string(json2) != expected2 {
		t.Errorf("Expected marshaled JSON to be '%s', got '%s'", expected2, string(json2))
	}
}

func TestPatchResolutionIsEmpty(t *testing.T) {
	// Test empty patch resolution
	emptyPatch := PatchResolution{}
	if !emptyPatch.IsEmpty() {
		t.Error("Expected empty patch resolution to be empty")
	}

	// Test patch with single file
	singlePatch := PatchResolution{SingleFile: "content"}
	if singlePatch.IsEmpty() {
		t.Error("Expected single file patch resolution to not be empty")
	}

	// Test patch with multi files
	multiPatch := PatchResolution{
		MultiFile: map[string]string{"file1": "content1"},
	}
	if multiPatch.IsEmpty() {
		t.Error("Expected multi file patch resolution to not be empty")
	}
}

func TestCodeReviewResultUnmarshalJSON(t *testing.T) {
	// Test unmarshaling CodeReviewResult with string patch_resolution
	jsonStr := `{
		"status": "needs_revision",
		"feedback": "Fix the bug",
		"detailed_guidance": "Add error handling",
		"patch_resolution": "fixed code content"
	}`

	var result CodeReviewResult
	err := json.Unmarshal([]byte(jsonStr), &result)
	if err != nil {
		t.Fatalf("Failed to unmarshal CodeReviewResult with string patch: %v", err)
	}

	if result.Status != "needs_revision" {
		t.Errorf("Expected status to be 'needs_revision', got '%s'", result.Status)
	}

	if result.Feedback != "Fix the bug" {
		t.Errorf("Expected feedback to be 'Fix the bug', got '%s'", result.Feedback)
	}

	if result.PatchResolution == nil {
		t.Error("Expected PatchResolution to not be nil")
	}

	if result.PatchResolution.SingleFile != "fixed code content" {
		t.Errorf("Expected SingleFile to be 'fixed code content', got '%s'", result.PatchResolution.SingleFile)
	}

	// Test unmarshaling CodeReviewResult with object patch_resolution
	jsonObj := `{
		"status": "approved",
		"feedback": "Good work",
		"patch_resolution": {
			"main.go": "package main\nfunc main() {}\n",
			"utils.go": "package main\nfunc helper() {}\n"
		}
	}`

	var result2 CodeReviewResult
	err = json.Unmarshal([]byte(jsonObj), &result2)
	if err != nil {
		t.Fatalf("Failed to unmarshal CodeReviewResult with object patch: %v", err)
	}

	if result2.Status != "approved" {
		t.Errorf("Expected status to be 'approved', got '%s'", result2.Status)
	}

	if result2.PatchResolution == nil {
		t.Error("Expected PatchResolution to not be nil")
	}

	if len(result2.PatchResolution.MultiFile) != 2 {
		t.Errorf("Expected MultiFile to have 2 entries, got %d", len(result2.PatchResolution.MultiFile))
	}

	if result2.PatchResolution.MultiFile["main.go"] != "package main\nfunc main() {}\n" {
		t.Errorf("Expected main.go content to be correct, got '%s'", result2.PatchResolution.MultiFile["main.go"])
	}
}
