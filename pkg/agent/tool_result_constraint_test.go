package agent

import (
	"os"
	"strings"
	"testing"
)

func TestConstrainToolResultForModel_NonSpecialTool(t *testing.T) {
	result := "hello world"
	args := map[string]interface{}{"command": "echo"}
	out := constrainToolResultForModel("shell_command", args, result)
	if out != result {
		t.Errorf("non-special tool: got %q; want %q", out, result)
	}
}

func TestConstrainToolResultForModel_FetchURLWithinLimit(t *testing.T) {
	result := strings.Repeat("a", 100)
	args := map[string]interface{}{"url": "https://example.com"}
	out := constrainToolResultForModel("fetch_url", args, result)
	if out != result {
		t.Errorf("fetch_url within limit: got len %d; want %d", len(out), len(result))
	}
}

func TestConstrainToolResultForModel_FetchURLExactLimit(t *testing.T) {
	result := strings.Repeat("b", defaultFetchURLResultMaxChars)
	args := map[string]interface{}{"url": "https://example.com"}
	out := constrainToolResultForModel("fetch_url", args, result)
	if out != result {
		t.Errorf("fetch_url at exact limit: got len %d; want %d", len(out), len(result))
	}
}

func TestConstrainToolResultForModel_FetchURLOverLimit(t *testing.T) {
	// Create a string slightly over the 80000 char limit
	result := strings.Repeat("c", defaultFetchURLResultMaxChars+1)
	args := map[string]interface{}{"url": "https://example.com"}

	t.Setenv("SPROUT_FETCH_URL_ARCHIVE_DIR", "") // use default

	out := constrainToolResultForModel("fetch_url", args, result)

	// Should contain "omitted"
	if !strings.Contains(out, "omitted") {
		t.Error("output should contain 'omitted' but doesn't")
	}

	// Should contain head and tail of original
	if !strings.Contains(out, "ccc") {
		t.Error("output should contain head/tail of original content")
	}
}

func TestConstrainToolResultForModel_FetchURLOverLimit_CustomDir(t *testing.T) {
	// Use a temp directory so the file save succeeds
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_FETCH_URL_ARCHIVE_DIR", tmpDir)

	result := strings.Repeat("d", defaultFetchURLResultMaxChars+1)
	args := map[string]interface{}{"url": "https://example.com"}

	out := constrainToolResultForModel("fetch_url", args, result)

	if !strings.Contains(out, "omitted") {
		t.Error("output should contain 'omitted'")
	}
	// Should contain the saved path
	if !strings.Contains(out, "Full output saved to") {
		t.Error("output should contain 'Full output saved to' when file is saved successfully")
	}
}

func TestConstrainToolResultForModel_FetchURLOverLimit_BadDir(t *testing.T) {
	// Point to a path that can't be written to, triggering the "unavailable" or error path
	t.Setenv("SPROUT_FETCH_URL_ARCHIVE_DIR", "/proc/nonexistent/sprout")

	result := strings.Repeat("e", defaultFetchURLResultMaxChars+1)
	args := map[string]interface{}{"url": "https://example.com"}

	out := constrainToolResultForModel("fetch_url", args, result)

	if !strings.Contains(out, "omitted") {
		t.Error("output should contain 'omitted'")
	}
	// Either "unavailable" or an error message
	if !strings.Contains(out, "unavailable") && !strings.Contains(out, "Failed to save full output") {
		t.Errorf("output should contain 'unavailable' or error when save fails; got: %s", out)
	}
}

func TestConstrainToolResultForModel_AnalyzeImageContent_InvalidJSON(t *testing.T) {
	result := "this is not json"
	args := map[string]interface{}{"image_path": "test.png"}
	out := constrainToolResultForModel("analyze_image_content", args, result)
	if out != result {
		t.Errorf("invalid JSON: got %q; want %q", out, result)
	}
}

func TestConstrainToolResultForModel_AnalyzeImageContent_ValidJSON(t *testing.T) {
	// Build a valid ImageAnalysisResponse JSON
	result := `{
		"success": true,
		"input_path": "test.png",
		"input_type": "local_file",
		"ocr_attempted": true,
		"extracted_text": "Hello World",
		"analysis": {
			"description": "A simple test image",
			"elements": [{"tag": "h1", "text": "Hello"}],
			"issues": ["small text"],
			"suggestions": ["increase font"]
		}
	}`
	args := map[string]interface{}{"image_path": "test.png"}
	out := constrainToolResultForModel("analyze_image_content", args, result)

	// Should be compacted
	if out == result {
		t.Error("should have compacted the result, but got identical output")
	}
	if !strings.Contains(out, "analyze_image_content result:") {
		t.Error("output should contain 'analyze_image_content result:'")
	}
	if !strings.Contains(out, "- success: true") {
		t.Error("output should contain success field")
	}
}

func TestConstrainToolResultForModel_AnalyzeImageContent_EmptyExtractedText(t *testing.T) {
	result := `{
		"success": true,
		"input_path": "test.png",
		"input_type": "local_file",
		"ocr_attempted": false,
		"extracted_text": ""
	}`
	args := map[string]interface{}{"image_path": "test.png"}
	out := constrainToolResultForModel("analyze_image_content", args, result)

	// Should still produce a summary without crashing
	if !strings.Contains(out, "analyze_image_content result:") {
		t.Error("output should contain 'analyze_image_content result:'")
	}
}

func TestConstrainToolResultForModel_FetchURLOverLimit_CustomMaxChars(t *testing.T) {
	t.Setenv("SPROUT_FETCH_URL_MAX_CHARS", "100")
	t.Setenv("SPROUT_FETCH_URL_ARCHIVE_DIR", "")

	result := strings.Repeat("f", 150)
	args := map[string]interface{}{"url": "https://example.com"}

	out := constrainToolResultForModel("fetch_url", args, result)

	if !strings.Contains(out, "omitted") {
		t.Error("output should contain 'omitted' when custom max is exceeded")
	}
}

func TestBuildFetchURLTruncationNotice_WithPath(t *testing.T) {
	notice := buildFetchURLTruncationNotice(5000, "/tmp/sprout/file.txt", nil)
	if !strings.Contains(notice, "/tmp/sprout/file.txt") {
		t.Error("notice should contain path")
	}
	if !strings.Contains(notice, "omitted") {
		t.Error("notice should contain 'omitted'")
	}
	if !strings.Contains(notice, "5000") {
		t.Error("notice should contain character count")
	}
}

func TestBuildFetchURLTruncationNotice_WithError(t *testing.T) {
	notice := buildFetchURLTruncationNotice(1000, "", os.ErrPermission)
	if !strings.Contains(notice, "omitted") {
		t.Error("notice should contain 'omitted'")
	}
	if !strings.Contains(notice, "1000") {
		t.Error("notice should contain character count")
	}
	// Error message should be present
	if !strings.Contains(notice, "permission denied") {
		t.Error("notice should contain error message")
	}
}

func TestBuildFetchURLTruncationNotice_NeitherPathNorError(t *testing.T) {
	notice := buildFetchURLTruncationNotice(2000, "", nil)
	if !strings.Contains(notice, "omitted") {
		t.Error("notice should contain 'omitted'")
	}
	if !strings.Contains(notice, "unavailable") {
		t.Error("notice should contain 'unavailable' when no path and no error")
	}
	if !strings.Contains(notice, "2000") {
		t.Error("notice should contain character count")
	}
}

func TestLimitAnalyzeImageExcerpt_ShorterThanMax(t *testing.T) {
	text := "short text"
	out := limitAnalyzeImageExcerpt(text, 100)
	if out != "short text" {
		t.Errorf("got %q; want %q", out, "short text")
	}
}

func TestLimitAnalyzeImageExcerpt_LongerThanMax(t *testing.T) {
	text := strings.Repeat("x", 100)
	out := limitAnalyzeImageExcerpt(text, 50)
	if !strings.Contains(out, "EXCERPT TRUNCATED") {
		t.Error("should contain 'EXCERPT TRUNCATED' when text exceeds maxChars")
	}
	if len(out) > 50 {
		t.Errorf("output len %d should be <= maxChars %d", len(out), 50)
	}
}

func TestLimitAnalyzeImageExcerpt_MaxCharsZero(t *testing.T) {
	text := "some text"
	out := limitAnalyzeImageExcerpt(text, 0)
	if out != "some text" {
		t.Errorf("got %q; want %q", out, "some text")
	}
}

func TestLimitAnalyzeImageExcerpt_MaxCharsNegative(t *testing.T) {
	text := "some text"
	out := limitAnalyzeImageExcerpt(text, -1)
	if out != "some text" {
		t.Errorf("got %q; want %q", out, "some text")
	}
}

func TestLimitAnalyzeImageExcerpt_ExactLength(t *testing.T) {
	text := "abcdefghij" // 10 chars
	out := limitAnalyzeImageExcerpt(text, 10)
	if out != text {
		t.Errorf("got %q; want %q", out, text)
	}
}

func TestCompactAnalyzeImageResultForModel_InvalidJSON(t *testing.T) {
	result := "not json"
	out := compactAnalyzeImageResultForModel(result)
	if out != result {
		t.Errorf("invalid JSON should be returned as-is: got %q; want %q", out, result)
	}
}

func TestCompactAnalyzeImageResultForModel_ValidJSONWithAllFields(t *testing.T) {
	result := `{
		"success": true,
		"input_path": "/images/test.png",
		"input_type": "local_file",
		"ocr_attempted": true,
		"error_code": "",
		"error_message": "",
		"extracted_text": "This is some extracted text from the image",
		"output_truncated": false,
		"original_chars": 1000,
		"returned_chars": 500,
		"full_output_path": "/tmp/full_output.txt",
		"analysis": {
			"description": "A test image",
			"elements": [{"tag": "h1", "text": "Title"}],
			"issues": ["blurry"],
			"suggestions": ["use higher res"]
		}
	}`

	out := compactAnalyzeImageResultForModel(result)

	if out == result {
		t.Error("should have compacted, got identical output")
	}
	if !strings.Contains(out, "analyze_image_content result:") {
		t.Error("should contain header")
	}
	if !strings.Contains(out, "- success: true") {
		t.Error("should contain success field")
	}
	if !strings.Contains(out, "- input_path: /images/test.png") {
		t.Error("should contain input_path")
	}
	if !strings.Contains(out, "- input_type: local_file") {
		t.Error("should contain input_type")
	}
	if !strings.Contains(out, "- ocr_attempted: true") {
		t.Error("should contain ocr_attempted")
	}
	if !strings.Contains(out, "- detected_elements: 1") {
		t.Error("should contain detected_elements count")
	}
	if !strings.Contains(out, "- issues: blurry") {
		t.Error("should contain issues")
	}
	if !strings.Contains(out, "- suggestions: use higher res") {
		t.Error("should contain suggestions")
	}
	if !strings.Contains(out, "- extracted_text_excerpt:") {
		t.Error("should contain extracted_text_excerpt")
	}
	if !strings.Contains(out, "- full_output_path: /tmp/full_output.txt") {
		t.Error("should contain full_output_path")
	}
}

func TestCompactAnalyzeImageResultForModel_EmptyExtractedText(t *testing.T) {
	result := `{
		"success": true,
		"input_path": "test.png",
		"input_type": "local_file",
		"ocr_attempted": false,
		"extracted_text": ""
	}`

	out := compactAnalyzeImageResultForModel(result)

	// Should not crash, should still produce a summary
	if !strings.Contains(out, "analyze_image_content result:") {
		t.Error("should contain header even with empty extracted_text")
	}
	if !strings.Contains(out, "- success: true") {
		t.Error("should contain success field")
	}
}
