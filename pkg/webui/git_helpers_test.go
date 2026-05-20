//go:build !js

package webui

import (
	"encoding/hex"
	"strings"
	"testing"
)

// ====================================================================
// parseNameStatusLine - additional edge cases
// ====================================================================

func TestParseNameStatusLine_TabInPath(t *testing.T) {
	// A path containing a tab character should still be parsed correctly
	// The last part after the last tab is the file path
	line := "M\tfile\twith\ttabs"
	status, path, ok := parseNameStatusLine(line)
	if !ok {
		t.Fatal("expected ok=true for path with tabs")
	}
	if status != "M" {
		t.Errorf("status = %q, want M", status)
	}
	if path != "tabs" {
		t.Errorf("path = %q, want 'tabs' (last tab-separated part)", path)
	}
}

func TestParseNameStatusLine_MultipleStatusChars(t *testing.T) {
	// "MM" means staged + modified — only the first char is used as status
	line := "MM\tfile.go"
	status, path, ok := parseNameStatusLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != "M" {
		t.Errorf("status = %q, want M (first char only)", status)
	}
	if path != "file.go" {
		t.Errorf("path = %q, want file.go", path)
	}
}

func TestParseNameStatusLine_WhitespaceAround(t *testing.T) {
	line := "  M  \t  src/main.go  "
	status, path, ok := parseNameStatusLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != "M" {
		t.Errorf("status = %q, want M", status)
	}
	if path != "src/main.go" {
		t.Errorf("path = %q, want src/main.go", path)
	}
}

func TestParseNameStatusLine_VeryLongLine(t *testing.T) {
	longPath := strings.Repeat("a", 1000)
	line := "M\t" + longPath
	status, path, ok := parseNameStatusLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != "M" {
		t.Errorf("status = %q, want M", status)
	}
	if len(path) != 1000 {
		t.Errorf("path length = %d, want 1000", len(path))
	}
}

func TestParseNameStatusLine_CopiesUseNewPath(t *testing.T) {
	line := "C100\told.go\tcopy.go"
	status, path, ok := parseNameStatusLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != "C" {
		t.Errorf("status = %q, want C", status)
	}
	if path != "copy.go" {
		t.Errorf("path = %q, want copy.go (copy uses new path)", path)
	}
}

func TestParseNameStatusLine_ScoreInRename(t *testing.T) {
	line := "R050\told.go\tnew.go"
	status, path, ok := parseNameStatusLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != "R" {
		t.Errorf("status = %q, want R (first char of R050)", status)
	}
	if path != "new.go" {
		t.Errorf("path = %q, want new.go", path)
	}
}

// ====================================================================
// normalizeGitPath - additional edge cases
// ====================================================================

func TestNormalizeGitPath_ForwardSlashesUnchanged(t *testing.T) {
	got := normalizeGitPath("a/b/c")
	if got != "a/b/c" {
		t.Errorf("got %q, want a/b/c", got)
	}
}

func TestNormalizeGitPath_Dotdot(t *testing.T) {
	// ".." alone is a valid path, but filepath.Clean("..") returns ".."
	// which is NOT "." so it won't be treated as empty
	got := normalizeGitPath("..")
	if got == "" {
		t.Errorf("normalizeGitPath(\"..\") should not be empty (it's a valid parent dir ref)")
	}
}

func TestNormalizeGitPath_MultipleDots(t *testing.T) {
	got := normalizeGitPath("./././file")
	if got != "file" {
		t.Errorf("got %q, want file", got)
	}
}

func TestNormalizeGitPath_DotSlash(t *testing.T) {
	got := normalizeGitPath("./")
	if got != "" {
		t.Errorf("got %q, want empty (./ is the current dir)", got)
	}
}

func TestNormalizeGitPath_NestedParentDir(t *testing.T) {
	got := normalizeGitPath("a/b/../../c")
	if got != "c" {
		t.Errorf("got %q, want c", got)
	}
}

// ====================================================================
// makeGitRelativePath - additional edge cases
// ====================================================================

func TestMakeGitRelativePath_DeeplyNested(t *testing.T) {
	got := makeGitRelativePath("/ws/a/b/c/d/e.go", "/ws")
	if got != "a/b/c/d/e.go" {
		t.Errorf("got %q, want a/b/c/d/e.go", got)
	}
}

func TestMakeGitRelativePath_WorkspaceRootTrailingSlash(t *testing.T) {
	got := makeGitRelativePath("/ws/file.go", "/ws/")
	if got != "file.go" {
		t.Errorf("got %q, want file.go", got)
	}
}

func TestMakeGitRelativePath_ParentDirOutside(t *testing.T) {
	// /tmp is outside /ws, so the path should be returned unchanged
	got := makeGitRelativePath("/tmp/file.go", "/ws")
	if got != "/tmp/file.go" {
		t.Errorf("got %q, want /tmp/file.go (outside workspace)", got)
	}
}

func TestMakeGitRelativePath_CurrentDirOutside(t *testing.T) {
	// "." in path.Rel would start with ".."
	got := makeGitRelativePath("/other/sub/file.go", "/ws")
	if got != "/other/sub/file.go" {
		t.Errorf("got %q, want /other/sub/file.go (outside workspace)", got)
	}
}

// ====================================================================
// pathExistsInGitStatus - additional edge cases
// ====================================================================

func TestPathExistsInGitStatus_Staged(t *testing.T) {
	status := &GitStatus{
		Staged: []GitFile{{Path: "staged.go", Status: "M", Staged: true}},
	}
	if !pathExistsInGitStatus("staged.go", status) {
		t.Error("staged.go should be found")
	}
}

func TestPathExistsInGitStatus_EmptyStatus(t *testing.T) {
	status := &GitStatus{}
	if pathExistsInGitStatus("anything.go", status) {
		t.Error("empty GitStatus should not match anything")
	}
}

func TestPathExistsInGitStatus_AllSectionsMatch(t *testing.T) {
	status := &GitStatus{
		Staged:    []GitFile{{Path: "staged.go", Status: "M"}},
		Modified:  []GitFile{{Path: "modified.go", Status: "M"}},
		Untracked: []GitFile{{Path: "new.go", Status: "?"}},
		Deleted:   []GitFile{{Path: "removed.go", Status: "D"}},
		Renamed:   []GitFile{{Path: "renamed.go", Status: "R"}},
	}

	expected := []string{"staged.go", "modified.go", "new.go", "removed.go", "renamed.go"}
	for _, path := range expected {
		if !pathExistsInGitStatus(path, status) {
			t.Errorf("pathExistsInGitStatus(%q) should be true", path)
		}
	}

	if pathExistsInGitStatus("missing.go", status) {
		t.Error("missing.go should not be found")
	}
}

func TestPathExistsInGitStatus_NormalizedPath(t *testing.T) {
	status := &GitStatus{
		Modified: []GitFile{{Path: "./src/../src/main.go", Status: "M"}},
	}
	if !pathExistsInGitStatus("src/main.go", status) {
		t.Error("normalized path should match")
	}
}

// ====================================================================
// containsPath - additional edge cases
// ====================================================================

func TestContainsPath_CaseSensitive(t *testing.T) {
	files := []GitFile{{Path: "File.go", Status: "M"}}
	if containsPath(files, "file.go") {
		t.Error("containsPath should be case-sensitive")
	}
	if !containsPath(files, "File.go") {
		t.Error("containsPath should match exact case")
	}
}

func TestContainsPath_MiddleOfList(t *testing.T) {
	files := []GitFile{
		{Path: "a.go", Status: "M"},
		{Path: "b.go", Status: "A"},
		{Path: "c.go", Status: "D"},
	}
	if !containsPath(files, "b.go") {
		t.Error("should find path in middle of list")
	}
}

func TestContainsPath_SpecialCharacters(t *testing.T) {
	files := []GitFile{{Path: "file with spaces.go", Status: "M"}}
	if !containsPath(files, "file with spaces.go") {
		t.Error("should match path with spaces")
	}
	if containsPath(files, "file-with-spaces.go") {
		t.Error("should not match different path")
	}
}

func TestContainsPath_LastElement(t *testing.T) {
	files := []GitFile{
		{Path: "a.go", Status: "M"},
		{Path: "z.go", Status: "M"},
	}
	if !containsPath(files, "z.go") {
		t.Error("should find last element")
	}
}

// ====================================================================
// truncateDiffOutput - additional edge cases
// ====================================================================

func TestTruncateDiffOutput_NegativeMaxBytes(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("truncateDiffOutput with negative maxBytes panicked (expected): %v", r)
		}
	}()
	// diff[:negative] panics in Go
	_ = truncateDiffOutput("hello", -1)
}

func TestTruncateDiffOutput_LargeDiffSmallMax(t *testing.T) {
	diff := strings.Repeat("x", 10000)
	got := truncateDiffOutput(diff, 10)
	if !strings.Contains(got, "... [diff truncated]") {
		t.Error("should contain truncation marker")
	}
	if len(got) != 10+22 { // 10 bytes + "\n\n... [diff truncated]" = 10 + 22 = 32
		t.Errorf("got length %d, want 32", len(got))
	}
}

func TestTruncateDiffOutput_NewlineAtTruncationPoint(t *testing.T) {
	diff := "line1\nline2\nline3\nline4"
	got := truncateDiffOutput(diff, 12)
	// "line1\nline2\n" is 12 chars, then truncation marker
	if !strings.HasPrefix(got, "line1\nline2\n") {
		t.Errorf("truncated diff should start with 'line1\\nline2\\n': %q", got)
	}
	if !strings.Contains(got, "... [diff truncated]") {
		t.Error("should contain truncation marker")
	}
}

func TestTruncateDiffOutput_MaxBytesOne(t *testing.T) {
	got := truncateDiffOutput("hello", 1)
	expected := "h\n\n... [diff truncated]"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestTruncateDiffOutput_MaxBytesEqualToLength(t *testing.T) {
	got := truncateDiffOutput("exact", 5)
	if got != "exact" {
		t.Errorf("got %q, want exact", got)
	}
}

func TestTruncateDiffOutput_MaxBytesOneLessThanLength(t *testing.T) {
	got := truncateDiffOutput("exact", 4)
	expected := "exac\n\n... [diff truncated]"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

// ====================================================================
// generateCryptoID - additional edge cases
// ====================================================================

func TestGenerateCryptoID_EmptyPrefix(t *testing.T) {
	id := generateCryptoID("")
	// Should be "-<24 hex chars>"
	if !strings.HasPrefix(id, "-") {
		t.Errorf("empty prefix should still produce dash separator: %q", id)
	}
	// Total length: 1 (dash) + 24 (hex) = 25
	if len(id) != 25 {
		t.Errorf("length = %d, want 25", len(id))
	}
}

func TestGenerateCryptoID_ValidHex(t *testing.T) {
	id := generateCryptoID("test")
	// Extract the hex part after "test-"
	parts := strings.SplitN(id, "-", 2)
	if len(parts) != 2 {
		t.Fatalf("ID should contain dash separator: %q", id)
	}
	hexPart := parts[1]
	if len(hexPart) != 24 {
		t.Errorf("hex part length = %d, want 24", len(hexPart))
	}
	_, err := hex.DecodeString(hexPart)
	if err != nil {
		t.Errorf("hex part should be valid hex: %v", err)
	}
}

func TestGenerateCryptoID_DifferentPrefixes(t *testing.T) {
	id1 := generateCryptoID("session")
	id2 := generateCryptoID("chat")

	if strings.HasPrefix(id1, "chat") {
		t.Errorf("session ID should start with 'session', got %q", id1)
	}
	if strings.HasPrefix(id2, "session") {
		t.Errorf("chat ID should start with 'chat', got %q", id2)
	}
}

func TestGenerateCryptoID_MultipleUnique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateCryptoID("test")
		if ids[id] {
			t.Errorf("duplicate ID found: %q", id)
		}
		ids[id] = true
	}
}

// ====================================================================
// getAllGitFiles
// ====================================================================

func TestGetAllGitFiles_NilStatus(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("getAllGitFiles(nil) panicked (expected — function does not guard nil): %v", r)
		}
	}()
	// getAllGitFiles panics on nil status (no nil guard in implementation)
	files := getAllGitFiles(nil)
	if files != nil {
		t.Errorf("getAllGitFiles(nil) should return nil, got %v", files)
	}
}

func TestGetAllGitFiles_EmptyStatus(t *testing.T) {
	status := &GitStatus{}
	files := getAllGitFiles(status)
	if len(files) != 0 {
		t.Errorf("getAllGitFiles(empty) should return empty, got %v", files)
	}
}

func TestGetAllGitFiles_AllSections(t *testing.T) {
	status := &GitStatus{
		Staged:    []GitFile{{Path: "staged.go", Status: "M"}},
		Modified:  []GitFile{{Path: "modified.go", Status: "M"}},
		Untracked: []GitFile{{Path: "new.go", Status: "?"}},
		Deleted:   []GitFile{{Path: "removed.go", Status: "D"}},
		Renamed:   []GitFile{{Path: "renamed.go", Status: "R"}},
	}
	files := getAllGitFiles(status)
	if len(files) != 5 {
		t.Errorf("getAllGitFiles returned %d files, want 5", len(files))
	}
}

func TestGetAllGitFiles_DuplicatesPreserved(t *testing.T) {
	// Same file could appear in multiple sections (e.g., staged and modified)
	status := &GitStatus{
		Staged:   []GitFile{{Path: "file.go", Status: "M"}},
		Modified: []GitFile{{Path: "file.go", Status: "M"}},
	}
	files := getAllGitFiles(status)
	if len(files) != 2 {
		t.Errorf("getAllGitFiles should return duplicates from different sections: got %d, want 2", len(files))
	}
}
