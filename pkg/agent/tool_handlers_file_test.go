package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsImageExtension(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// True cases
		{"png", "file.png", true},
		{"jpg", "file.jpg", true},
		{"jpeg", "file.jpeg", true},
		{"gif", "file.gif", true},
		{"webp", "file.webp", true},
		{"bmp", "file.bmp", true},
		{"avif", "file.avif", true},
		{"png uppercase", "file.PNG", true},
		{"JPG uppercase", "file.JPG", true},
		{"nested path png", "/home/user/images/photo.png", true},
		{"just extension", ".png", true},

		// False cases
		{"txt", "file.txt", false},
		{"go", "main.go", false},
		{"json", "config.json", false},
		{"no extension", "file", false},
		{"empty string", "", false},
		{"pdf", "doc.pdf", false},
		{"svg", "image.svg", false},
		{"ico", "icon.ico", false},
		{"tiff", "image.tiff", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isImageExtension(tc.input); got != tc.want {
				t.Errorf("isImageExtension(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsPDFExtension(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"pdf", "doc.pdf", true},
		{"PDF uppercase", "doc.PDF", true},
		{"nested pdf", "/home/docs/report.pdf", true},
		{"txt", "file.txt", false},
		{"png", "file.png", false},
		{"no extension", "file", false},
		{"empty", "", false},
		{"json", "data.json", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPDFExtension(tc.input); got != tc.want {
				t.Errorf("isPDFExtension(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestGetFilePath(t *testing.T) {
	t.Run("path key with string value", func(t *testing.T) {
		got, err := getFilePath(map[string]interface{}{"path": "file.txt"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "file.txt" {
			t.Errorf("got %q, want %q", got, "file.txt")
		}
	})

	t.Run("file_path key with string value", func(t *testing.T) {
		got, err := getFilePath(map[string]interface{}{"file_path": "file.txt"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "file.txt" {
			t.Errorf("got %q, want %q", got, "file.txt")
		}
	})

	t.Run("path takes priority over file_path", func(t *testing.T) {
		got, err := getFilePath(map[string]interface{}{
			"path":      "primary.txt",
			"file_path": "secondary.txt",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "primary.txt" {
			t.Errorf("got %q, want %q", got, "primary.txt")
		}
	})

	t.Run("neither key present returns error", func(t *testing.T) {
		_, err := getFilePath(map[string]interface{}{"other": "value"})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "path") {
			t.Errorf("error should mention 'path', got: %v", err)
		}
	})

	t.Run("path with byte slice value", func(t *testing.T) {
		got, err := getFilePath(map[string]interface{}{"path": []byte("file.txt")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "file.txt" {
			t.Errorf("got %q, want %q", got, "file.txt")
		}
	})

	t.Run("path with nil value returns error", func(t *testing.T) {
		_, err := getFilePath(map[string]interface{}{"path": nil})
		if err == nil {
			t.Fatal("expected error for nil path")
		}
	})

	t.Run("empty args map returns error", func(t *testing.T) {
		_, err := getFilePath(map[string]interface{}{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestGetRequiredString(t *testing.T) {
	t.Run("existing key with string value", func(t *testing.T) {
		got, err := getRequiredString(map[string]interface{}{"content": "hello"}, "content")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})

	t.Run("missing key returns error with key name", func(t *testing.T) {
		_, err := getRequiredString(map[string]interface{}{}, "content")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "content") {
			t.Errorf("error should contain 'content', got: %v", err)
		}
		if !strings.Contains(err.Error(), "required") {
			t.Errorf("error should contain 'required', got: %v", err)
		}
	})

	t.Run("nil value returns error", func(t *testing.T) {
		_, err := getRequiredString(map[string]interface{}{"content": nil}, "content")
		if err == nil {
			t.Fatal("expected error for nil value")
		}
	})

	t.Run("int value returns formatted string", func(t *testing.T) {
		got, err := getRequiredString(map[string]interface{}{"count": 42}, "count")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "42" {
			t.Errorf("got %q, want %q", got, "42")
		}
	})

	t.Run("byte slice value returns string", func(t *testing.T) {
		got, err := getRequiredString(map[string]interface{}{"data": []byte("hello")}, "data")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})
}

func TestToInt(t *testing.T) {
	t.Run("int value", func(t *testing.T) {
		v, ok := toInt(int(42))
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 42 {
			t.Errorf("got %d, want 42", v)
		}
	})

	t.Run("float64 value", func(t *testing.T) {
		v, ok := toInt(float64(42.7))
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 42 {
			t.Errorf("got %d, want 42 (truncated)", v)
		}
	})

	t.Run("float64 zero", func(t *testing.T) {
		v, ok := toInt(float64(0))
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 0 {
			t.Errorf("got %d, want 0", v)
		}
	})

	t.Run("negative int", func(t *testing.T) {
		v, ok := toInt(int(-5))
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != -5 {
			t.Errorf("got %d, want -5", v)
		}
	})

	t.Run("negative float64 truncates toward zero", func(t *testing.T) {
		v, ok := toInt(float64(-3.7))
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != -3 {
			t.Errorf("got %d, want -3 (truncated toward zero)", v)
		}
	})

	t.Run("large float64", func(t *testing.T) {
		v, ok := toInt(float64(1e9))
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 1000000000 {
			t.Errorf("got %d, want 1000000000", v)
		}
	})

	t.Run("string value returns false", func(t *testing.T) {
		_, ok := toInt("42")
		if ok {
			t.Error("expected ok=false for string")
		}
	})

	t.Run("nil value returns false", func(t *testing.T) {
		_, ok := toInt(nil)
		if ok {
			t.Error("expected ok=false for nil")
		}
	})

	t.Run("bool value returns false", func(t *testing.T) {
		_, ok := toInt(true)
		if ok {
			t.Error("expected ok=false for bool")
		}
	})

	t.Run("slice value returns false", func(t *testing.T) {
		_, ok := toInt([]int{1, 2})
		if ok {
			t.Error("expected ok=false for slice")
		}
	})
}

func TestValidateJSONContent(t *testing.T) {
	t.Run("valid JSON with .json path returns empty", func(t *testing.T) {
		warn := validateJSONContent(`{"key": "value"}`, "file.json")
		if warn != "" {
			t.Errorf("got warning %q, want empty", warn)
		}
	})

	t.Run("valid JSON array with .json path returns empty", func(t *testing.T) {
		warn := validateJSONContent(`[1, 2, 3]`, "file.json")
		if warn != "" {
			t.Errorf("got warning %q, want empty", warn)
		}
	})

	t.Run("invalid JSON with .json path returns warning", func(t *testing.T) {
		warn := validateJSONContent(`{key: value}`, "config.json")
		if warn == "" {
			t.Fatal("expected warning for invalid JSON")
		}
		if !strings.Contains(warn, "invalid JSON") {
			t.Errorf("warning should contain 'invalid JSON', got: %s", warn)
		}
		if !strings.Contains(warn, "config.json") {
			t.Errorf("warning should contain filename, got: %s", warn)
		}
	})

	t.Run("any content with non-json path returns empty", func(t *testing.T) {
		warn := validateJSONContent(`{key: value}`, "file.go")
		if warn != "" {
			t.Errorf("got warning %q, want empty for non-json file", warn)
		}
	})

	t.Run("empty content with .json path returns empty", func(t *testing.T) {
		warn := validateJSONContent("", "file.json")
		if warn != "" {
			t.Errorf("got warning %q, want empty for empty content", warn)
		}
	})
}

func TestDisallowRawStructuredWrite(t *testing.T) {
	t.Run("json extension returns error", func(t *testing.T) {
		err := disallowRawStructuredWrite("file.json", "write_file")
		if err == nil {
			t.Fatal("expected error for .json")
		}
		if !strings.Contains(err.Error(), "write_file") {
			t.Errorf("error should contain tool name, got: %v", err)
		}
	})

	t.Run("yaml extension returns error", func(t *testing.T) {
		err := disallowRawStructuredWrite("file.yaml", "write_file")
		if err == nil {
			t.Fatal("expected error for .yaml")
		}
	})

	t.Run("yml extension returns error", func(t *testing.T) {
		err := disallowRawStructuredWrite("file.yml", "write_file")
		if err == nil {
			t.Fatal("expected error for .yml")
		}
	})

	t.Run("go extension returns nil", func(t *testing.T) {
		err := disallowRawStructuredWrite("file.go", "write_file")
		if err != nil {
			t.Errorf("expected nil for .go, got: %v", err)
		}
	})

	t.Run("txt extension returns nil", func(t *testing.T) {
		err := disallowRawStructuredWrite("file.txt", "write_file")
		if err != nil {
			t.Errorf("expected nil for .txt, got: %v", err)
		}
	})

	t.Run("JSON uppercase extension returns error", func(t *testing.T) {
		err := disallowRawStructuredWrite("file.JSON", "write_file")
		if err == nil {
			t.Fatal("expected error for .JSON (uppercase)")
		}
	})

	t.Run("YAML uppercase extension returns error", func(t *testing.T) {
		err := disallowRawStructuredWrite("file.YAML", "write_file")
		if err == nil {
			t.Fatal("expected error for .YAML (uppercase)")
		}
	})

	t.Run("no extension returns nil", func(t *testing.T) {
		err := disallowRawStructuredWrite("Makefile", "write_file")
		if err != nil {
			t.Errorf("expected nil for no extension, got: %v", err)
		}
	})

	t.Run("error message includes tool name and extension", func(t *testing.T) {
		err := disallowRawStructuredWrite("data.json", "write_file")
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "write_file") {
			t.Errorf("error should contain tool name 'write_file', got: %s", msg)
		}
		if !strings.Contains(msg, ".json") {
			t.Errorf("error should contain '.json', got: %s", msg)
		}
	})
}

func TestLineColFromOffset(t *testing.T) {
	t.Run("offset 0 returns line 1 col 1", func(t *testing.T) {
		l, c := lineColFromOffset("abc", 0)
		if l != 1 || c != 1 {
			t.Errorf("got line=%d col=%d, want 1,1", l, c)
		}
	})

	t.Run("offset 1 returns line 1 col 1", func(t *testing.T) {
		l, c := lineColFromOffset("abc", 1)
		if l != 1 || c != 1 {
			t.Errorf("got line=%d col=%d, want 1,1", l, c)
		}
	})

	t.Run("single line offset 2 is col 2", func(t *testing.T) {
		l, c := lineColFromOffset("abc", 2)
		if l != 1 || c != 2 {
			t.Errorf("got line=%d col=%d, want 1,2", l, c)
		}
	})

	t.Run("newline advances to next line", func(t *testing.T) {
		// "abc\ndef" - chars: a(1) b(2) c(3) \n(4) d(5) e(6) f(7)
		l, c := lineColFromOffset("abc\ndef", 5)
		if l != 2 || c != 1 {
			t.Errorf("got line=%d col=%d, want 2,1", l, c)
		}
	})

	t.Run("position after newline is col 1 of next line", func(t *testing.T) {
		l, c := lineColFromOffset("abc\ndef", 6)
		if l != 2 || c != 2 {
			t.Errorf("got line=%d col=%d, want 2,2", l, c)
		}
	})

	t.Run("position at end of line before newline", func(t *testing.T) {
		// "abc\n" - offset 4 is the newline char, line 1 col 4
		l, c := lineColFromOffset("abc\n", 4)
		if l != 1 || c != 4 {
			t.Errorf("got line=%d col=%d, want 1,4", l, c)
		}
	})

	t.Run("offset beyond content length is clamped", func(t *testing.T) {
		l, c := lineColFromOffset("abc", 100)
		if l != 1 || c != 4 {
			t.Errorf("got line=%d col=%d, want 1,4 (clamped to end+1)", l, c)
		}
	})

	t.Run("empty string returns line 1 col 1", func(t *testing.T) {
		l, c := lineColFromOffset("", 1)
		if l != 1 || c != 1 {
			t.Errorf("got line=%d col=%d, want 1,1", l, c)
		}
	})

	t.Run("empty string with negative offset", func(t *testing.T) {
		l, c := lineColFromOffset("", -5)
		if l != 1 || c != 1 {
			t.Errorf("got line=%d col=%d, want 1,1", l, c)
		}
	})

	t.Run("multi-line content", func(t *testing.T) {
		// "a\nb\nc" - line 1: a\n, line 2: b\n, line 3: c
		// offset 1=a, 2=\n, 3=b, 4=\n, 5=c
		l, c := lineColFromOffset("a\nb\nc", 5)
		if l != 3 || c != 1 {
			t.Errorf("got line=%d col=%d, want 3,1", l, c)
		}
	})
}

func TestSnippetAtLine(t *testing.T) {
	t.Run("line 1 of multi-line content", func(t *testing.T) {
		snippet := snippetAtLine("line1\nline2\nline3", 1)
		if snippet != "line1" {
			t.Errorf("got %q, want %q", snippet, "line1")
		}
	})

	t.Run("line 2 of multi-line content", func(t *testing.T) {
		snippet := snippetAtLine("line1\nline2\nline3", 2)
		if snippet != "line2" {
			t.Errorf("got %q, want %q", snippet, "line2")
		}
	})

	t.Run("line 0 returns empty", func(t *testing.T) {
		snippet := snippetAtLine("line1\nline2", 0)
		if snippet != "" {
			t.Errorf("got %q, want empty", snippet)
		}
	})

	t.Run("negative line returns empty", func(t *testing.T) {
		snippet := snippetAtLine("line1\nline2", -1)
		if snippet != "" {
			t.Errorf("got %q, want empty", snippet)
		}
	})

	t.Run("line beyond content returns empty", func(t *testing.T) {
		snippet := snippetAtLine("line1\nline2", 100)
		if snippet != "" {
			t.Errorf("got %q, want empty", snippet)
		}
	})

	t.Run("long line truncated with ellipsis", func(t *testing.T) {
		longLine := strings.Repeat("x", 200)
		snippet := snippetAtLine(longLine, 1)
		if len(snippet) > 123 {
			t.Errorf("snippet should be <= 123 chars (120 + '...'), got %d", len(snippet))
		}
		if !strings.HasSuffix(snippet, "...") {
			t.Errorf("long snippet should end with '...', got: %q", snippet)
		}
	})

	t.Run("short line returned as-is", func(t *testing.T) {
		snippet := snippetAtLine("short", 1)
		if snippet != "short" {
			t.Errorf("got %q, want %q", snippet, "short")
		}
	})

	t.Run("empty content returns empty", func(t *testing.T) {
		snippet := snippetAtLine("", 1)
		if snippet != "" {
			t.Errorf("got %q, want empty", snippet)
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		snippet := snippetAtLine("  leading and trailing  \n", 1)
		if snippet != "leading and trailing" {
			t.Errorf("got %q, want 'leading and trailing'", snippet)
		}
	})
}

func TestSameToolJSONFixHint(t *testing.T) {
	t.Run("edit_file returns edit_file hint", func(t *testing.T) {
		hint := sameToolJSONFixHint("edit_file")
		if !strings.Contains(hint, "edit_file") {
			t.Errorf("hint should contain 'edit_file', got: %s", hint)
		}
	})

	t.Run("write_file returns write_file hint", func(t *testing.T) {
		hint := sameToolJSONFixHint("write_file")
		if !strings.Contains(hint, "write_file") {
			t.Errorf("hint should contain 'write_file', got: %s", hint)
		}
	})

	t.Run("empty string returns write_file hint", func(t *testing.T) {
		hint := sameToolJSONFixHint("")
		if !strings.Contains(hint, "write_file") {
			t.Errorf("hint should contain 'write_file' for default, got: %s", hint)
		}
	})

	t.Run("whitespace around edit_file is trimmed", func(t *testing.T) {
		hint := sameToolJSONFixHint("  edit_file  ")
		if !strings.Contains(hint, "edit_file") {
			t.Errorf("hint should contain 'edit_file', got: %s", hint)
		}
	})

	t.Run("unknown tool returns write_file hint", func(t *testing.T) {
		hint := sameToolJSONFixHint("some_other_tool")
		if !strings.Contains(hint, "write_file") {
			t.Errorf("hint should default to 'write_file', got: %s", hint)
		}
	})
}

// TestHandleEditFile_TracksFullFileContent verifies that handleEditFile
// records the FULL file content (original + proposed) in the change
// tracker, NOT the edit fragments (oldStr/newStr). Recovery/rollback
// writes the tracked content back to disk, so a fragment would destroy
// the file. Regression test for the C2 bug where the non-approval
// branch passed oldStr/newStr to TrackFileEdit instead of the full
// before/after content.
func TestHandleEditFile_TracksFullFileContent(t *testing.T) {
	agent := NewTestAgent()
	ws := t.TempDir()
	// Resolve symlinks — macOS /var/folders → /private/var/folders
	// causes the filesystem gate to reject the path as "outside workspace".
	if resolved, err := filepath.EvalSymlinks(ws); err == nil {
		ws = resolved
	}
	agent.workspaceRoot = ws
	agent.changeTracker = NewChangeTracker(agent, "test")
	agent.changeTracker.Enable()

	filePath := filepath.Join(ws, "main.go")
	originalContent := "package main\n\nfunc a() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filePath, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	oldStr := "return 1"
	newStr := "return 2"
	if _, err := handleEditFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"old_str": oldStr,
		"new_str": newStr,
	}); err != nil {
		t.Fatalf("handleEditFile: %v", err)
	}

	changes := agent.changeTracker.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 tracked change, got %d", len(changes))
	}
	change := changes[0]

	// The tracker MUST record the full original file, not the "return 1" fragment.
	if change.OriginalCode != originalContent {
		t.Errorf("OriginalCode should be the FULL original file content, got %q (want %q)",
			change.OriginalCode, originalContent)
	}

	// The tracker MUST record the full proposed file (with the replacement applied).
	expectedNew := strings.Replace(originalContent, oldStr, newStr, 1)
	if change.NewCode != expectedNew {
		t.Errorf("NewCode should be the FULL proposed file content, got %q (want %q)",
			change.NewCode, expectedNew)
	}

	// Sanity: the fragments themselves must NOT appear as the full content.
	if change.OriginalCode == oldStr {
		t.Errorf("OriginalCode is the edit fragment %q — this is the C2 bug", oldStr)
	}
	if change.NewCode == newStr {
		t.Errorf("NewCode is the edit fragment %q — this is the C2 bug", newStr)
	}
}
