package agent

import (
	"os"
	"path/filepath"
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
	// A directory beneath a regular file can't be created on any OS,
	// triggering the "unavailable" or error path.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPROUT_FETCH_URL_ARCHIVE_DIR", filepath.Join(blocker, "sprout"))

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
