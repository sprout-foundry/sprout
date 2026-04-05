package git

import (
	"strings"
	"testing"
)

// =============================================================================
// parseStagedFileChanges — edge cases
// =============================================================================

func TestParseStagedFileChanges_WhitespaceOnlyLines(t *testing.T) {
	// Lines of pure whitespace should be skipped (fields produces no parts)
	input := "\t  \n   \n A\tfile.go\n \n"
	result := parseStagedFileChanges(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 change, got %d", len(result))
	}
	assertChange(t, result[0], "A", "file.go")
}

func TestParseStagedFileChanges_FilePathWithSpaces(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"tab separated with spaces", "M\tpath with spaces in name.go", "path with spaces in name.go"},
		{"space separated with spaces", "M path with spaces in name.go", "path with spaces in name.go"},
		{"multiple spaces mixed", "A  deeply   nested   path.go", "deeply nested path.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseStagedFileChanges(tt.line)
			if len(result) != 1 {
				t.Fatalf("expected 1 change, got %d", len(result))
			}
			if result[0].Path != tt.want {
				t.Errorf("path = %q, want %q", result[0].Path, tt.want)
			}
		})
	}
}

func TestParseStagedFileChanges_MixedStatuses(t *testing.T) {
	input := "A\tnew.go\nM\tmod.go\nD\told.go\nR100\ta.go\tb.go\nC100\torig.go\tcopy.go"
	result := parseStagedFileChanges(input)
	expected := []struct {
		status, path string
	}{
		{"A", "new.go"},
		{"M", "mod.go"},
		{"D", "old.go"},
		{"R100", "a.go b.go"},
		{"C100", "orig.go copy.go"},
	}
	if len(result) != len(expected) {
		t.Fatalf("expected %d changes, got %d", len(expected), len(result))
	}
	for i, exp := range expected {
		assertChange(t, result[i], exp.status, exp.path)
	}
}

func TestParseStagedFileChanges_SingleFileTab(t *testing.T) {
	input := "A\tsingle.go"
	result := parseStagedFileChanges(input)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	assertChange(t, result[0], "A", "single.go")
}

func TestParseStagedFileChanges_SingleFileSpace(t *testing.T) {
	input := "M my_file.go"
	result := parseStagedFileChanges(input)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	assertChange(t, result[0], "M", "my_file.go")
}

func TestParseStagedFileChanges_StatusOnlyLine(t *testing.T) {
	// "M" alone on a line has only 1 field, should be skipped
	input := "M\nA\treal.go"
	result := parseStagedFileChanges(input)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	assertChange(t, result[0], "A", "real.go")
}

// =============================================================================
// generateFallbackCommitMessage — additional edge cases
// =============================================================================

func TestGenerateFallbackCommitMessage_UnknownStatusFallsToModified(t *testing.T) {
	// Status like "T" (type change) or "X" or "C" are not A or D, so they
	// fall into the default bucket which counts as "modified".
	changes := []CommitFileChange{
		{Status: "T", Path: "symlink.go"},
		{Status: "X", Path: "unknown.go"},
		{Status: "C", Path: "copied.go"},
	}
	result := generateFallbackCommitMessage(changes)
	if !strings.Contains(result, "Update 3 Files") {
		t.Errorf("expected 'Update 3 Files' for unknown statuses, got %q", result)
	}
}

func TestGenerateFallbackCommitMessage_AllCategories(t *testing.T) {
	changes := []CommitFileChange{
		{Status: "A", Path: "a.go"},
		{Status: "A", Path: "b.go"},
		{Status: "M", Path: "c.go"},
		{Status: "D", Path: "d.go"},
		{Status: "D", Path: "e.go"},
		{Status: "D", Path: "f.go"},
	}
	result := generateFallbackCommitMessage(changes)
	if !strings.Contains(result, "Add 2 Files") {
		t.Errorf("expected 'Add 2 Files', got %q", result)
	}
	if !strings.Contains(result, "Update 1 Files") {
		t.Errorf("expected 'Update 1 Files', got %q", result)
	}
	if !strings.Contains(result, "Delete 3 Files") {
		t.Errorf("expected 'Delete 3 Files', got %q", result)
	}
	// Verify the parts are comma-separated
	parts := strings.Split(result, ", ")
	if len(parts) != 3 {
		t.Errorf("expected 3 comma-separated parts, got %d: %q", len(parts), result)
	}
}

// =============================================================================
// actionFromStatus — additional edge cases
// =============================================================================

func TestActionFromStatus_ExtraCases(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   string
	}{
		{"lowercase a", "a", "Updates"},
		{"lowercase d", "d", "Updates"},
		{"lowercase r", "r", "Updates"},
		{"type change", "T", "Updates"},
		{"copy", "C", "Updates"},
		{"rename with whitespace", "\tR\t", "Renames"},
		{"delete with whitespace", "  D  ", "Deletes"},
		{"add with tab", "\tA", "Adds"},
		{"unknown status X", "X", "Updates"},
		{"unmerge UU", "UU", "Updates"},
		{"pair merge AA", "AA", "Updates"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionFromStatus(tt.status)
			if got != tt.want {
				t.Errorf("actionFromStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// =============================================================================
// isDefaultBranch — additional edge cases
// =============================================================================

func TestIsDefaultBranch_ExtraCases(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		want   bool
	}{
		{"uppercase MAIN", "MAIN", false},
		{"uppercase MASTER", "MASTER", false},
		{"mixed case Main", "Main", false},
		{"leading space main", " main", true},
		{"trailing space main", "main ", true},
		{"tab main", "\tmain", true},
		{"tab main trailing", "\tmain\t", true},
		{"newline main", "main\n", true},
		{"bare develop", "develop", true},
		{"bare dev", "dev", true},
		{"develop with leading space", " develop", true},
		{"branch with slash", "feature/new-thing", false},
		{"branch with prefix main-", "main-branch", false},
		{"branch with suffix -main", "my-main", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDefaultBranch(tt.branch)
			if got != tt.want {
				t.Errorf("isDefaultBranch(%q) = %v, want %v", tt.branch, got, tt.want)
			}
		})
	}
}

// =============================================================================
// NormalizeShortTitle — additional edge cases
// =============================================================================

func TestNormalizeShortTitle_ExtraCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "backticks with whitespace inside",
			input: "`  hello world  `",
			want:  "hello world",
		},
		{
			name:  "title: no space after colon",
			input: "title:Add auth",
			want:  "Add auth",
		},
		{
			name:  "Title: no space after colon",
			input: "Title:Add auth",
			want:  "Add auth",
		},
		{
			name:  "multiple leading newlines",
			input: "\n\n\nHello world",
			want:  "Hello world",
		},
		{
			name:  "multiple newlines before title prefix",
			input: "title: Fix bug\n\nmore info\n\nmore",
			want:  "Fix bug",
		},
		{
			name:  "only backticks",
			input: "```",
			want:  "",
		},
		{
			name:  "empty with backticks and newline",
			input: "```\n",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   \t  ",
			want:  "",
		},
		{
			name:  "title prefix on multiline with trailing newline on first line",
			input: "Title: Hello\nDesc\n",
			want:  "Hello",
		},
		{
			name:  "just a newline",
			input: "\n",
			want:  "",
		},
		{
			name:  "backtick inside colon space",
			// Lead/trail backticks stripped → "Title:` Hello"
			// TrimPrefix("Title:") → "` Hello" → TrimSpace stays (only leading space)
			input: "`Title:` Hello`",
			want:  "` Hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeShortTitle(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeShortTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// WrapText — additional edge cases
// =============================================================================

func TestWrapText_ExtraCases(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		lineLength int
		want       string
	}{
		{
			name:       "zero line length each word on own line",
			text:       "hello world",
			lineLength: 0,
			want:       "hello\nworld",
		},
		{
			name:       "zero line length single character words",
			text:       "a b c",
			lineLength: 0,
			want:       "a\nb\nc",
		},
		{
			name:       "single newline not paragraph break",
			text:       "hello\nworld",
			lineLength: 72,
			// strings.Fields splits on \n too, so words are joined with spaces when wrapped
			want:       "hello world",
		},
		{
			name:       "single newline with wrapping",
			text:       "a very long line\nthat continues here",
			lineLength: 6,
			// Split on \n\n → single paragraph "a very long line\nthat continues here"
			// strings.Fields splits on all whitespace → [a, very, long, line, that, continues, here]
			want: "a very\nlong\nline\nthat\ncontinues\nhere",
		},
		{
			name:       "single word longer than line length",
			text:       "supercalifragilisticexpialidocious",
			lineLength: 5,
			want:       "supercalifragilisticexpialidocious",
		},
		{
			name:       "negative line length",
			text:       "hello world",
			lineLength: -1,
			want:       "hello\nworld",
		},
		{
			name:       "line length of 1 with single char words",
			text:       "a b c",
			lineLength: 1,
			want:       "a\nb\nc",
		},
		{
			name:       "line length of 1 with multi char words",
			text:       "ab cd ef",
			lineLength: 1,
			want:       "ab\ncd\nef",
		},
		{
			name:       "multiple paragraphs one empty",
			text:       "First paragraph\n\n\nThird paragraph",
			lineLength: 72,
			// Splits on \n\n: ["First paragraph", "\nThird paragraph"]
			// Wait - "First paragraph\n\n\nThird paragraph" split on \n\n:
			// ["First paragraph", "\nThird paragraph"]
			// Second paragraph: "\nThird paragraph" → fields = ["Third", "paragraph"] → wrapped
			want: "First paragraph\n\nThird paragraph",
		},
		{
			name:       "existing newlines within long paragraph",
			text:       "This is a test\nof wrapping",
			lineLength: 4,
			// Paragraph: "This is a test\nof wrapping"
			// Fields: [This, is, a, test, of, wrapping]
			// lineLen=4:
			//   "This" (4), "is" → 4+1+2=7 > 4 → new line "is"
			//   "a" → 2+1+1=4 <= 4 → "is a"
			//   "test" → 4+1+4=9 > 4 → new line "test"
			//   "of" → 4+1+2=7 > 4 → new line "of"
			//   "wrapping" → 2+1+8=11 > 4 → new line "wrapping"
			want: "This\nis a\ntest\nof\nwrapping",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapText(tt.text, tt.lineLength)
			if got != tt.want {
				t.Errorf("WrapText() mismatch\n  text: %q, lineLength: %d\n   got: %q\n  want: %q", tt.text, tt.lineLength, got, tt.want)
			}
		})
	}
}

// =============================================================================
// TruncateRunes — additional edge cases
// =============================================================================

func TestTruncateRunes_UnicodeEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{
			name: "japanese chars max 3",
			s:    "日本語テスト",
			max:  3,
			want: "日本語",
		},
				{
			name: "japanese chars max 6",
			s:    "日本語テスト",
			max:  6,
			// 6 runes == 6 max → returns full string
			want: "日本語テスト",
		},
		{
			name: "japanese exact length",
			s:    "日本語",
			max:  3,
			want: "日本語",
		},
		{
			name: "japanese shorter",
			s:    "日",
			max:  3,
			want:  "日",
		},
		{
			name: "emoji chars max 3",
			s:    "😀😎🎉🚀",
			max:  3,
			want: "😀😎🎉",
		},
		{
			name: "emoji chars max 4 with ellipsis",
			s:    "😀😎🎉🚀",
			max:  4,
			// 4 runes == 4 max → returns full string (not truncated)
			want: "😀😎🎉🚀",
		},
		{
			name: "emoji chars 5 truncated to 4",
			s:    "😀😎🎉🚀✨",
			max:  4,
			// 5 runes > 4: max <= 3? No (4). runes[:1]="😀" + "..." = "😀..."
			want: "😀...",
		},
		{
			name: "mixed ascii and unicode",
			s:    "Hello世界",
			max:  7,
			// 7 runes == 7 max → returns full string
			want: "Hello世界",
		},
		{
			name: "mixed ascii and unicode truncated",
			s:    "Hello世界",
			max:  6,
			// 7 runes > 6: runes[:3]="Hel" + "..." = "Hel..."
			want: "Hel...",
		},{
			name: "zero max unicode",
			s:    "日本語",
			max:  0,
			want:  "",
		},
		{
			name: "negative max unicode",
			s:    "日本語",
			max:  -5,
			want:  "",
		},
		{
			name: "max 1 with unicode",
			s:    "日本語",
			max:  1,
			want:  "日",
		},
		{
			name: "max 2 with unicode",
			s:    "日本語",
			max:  2,
			want:  "日本",
		},
		{
			name: "empty string unicode max",
			s:    "",
			max:  5,
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateRunes(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("TruncateRunes(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

func TestTruncateRunes_TrailingSpaces(t *testing.T) {
	// When the runes before the ellipsis end with spaces, TrimSpace removes them
	// "abc    def" max=8 → runes[:5] = "abc  " → TrimSpace → "abc" + "..." = "abc..."
	got := TruncateRunes("abc    def", 8)
	want := "abc..."
	if got != want {
		t.Errorf("TruncateRunes(%q, 8) = %q, want %q", "abc    def", got, want)
	}
}

func TestTruncateRunes_AllSpacesBeforeEllipsis(t *testing.T) {
	// "    abcdef" max=5 → runes[:2] = "  " → TrimSpace → "" + "..." = "..."
	got := TruncateRunes("    abcdef", 5)
	want := "..."
	if got != want {
		t.Errorf("TruncateRunes(%q, 5) = %q, want %q", "    abcdef", got, want)
	}
}

// =============================================================================
// Compile-time accessibility check
// =============================================================================

// Compile-time check that all target functions exist and are callable.
// If any function is removed or renamed, this file will fail to compile.
var (
	_ func(string) []CommitFileChange   = parseStagedFileChanges
	_ func(string) string                = actionFromStatus
	_ func(string) bool                  = isDefaultBranch
	_ func(string) string                = NormalizeShortTitle
	_ func(string, int) string           = WrapText
	_ func(string, int) string           = TruncateRunes
	_ func([]CommitFileChange) string     = generateFallbackCommitMessage
)
