package git

import (
	"strings"
	"testing"
)

// --- actionFromStatus ---

func TestActionFromStatus(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"A", "Adds"},
		{"D", "Deletes"},
		{"R", "Renames"},
		{"M", "Updates"},
		{"C", "Updates"},
		{"T", "Updates"},
		{"", "Updates"},
		{"  A  ", "Adds"},
		{"  M  ", "Updates"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := actionFromStatus(tt.status)
			if got != tt.want {
				t.Errorf("actionFromStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// --- isDefaultBranch ---

func TestIsDefaultBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   bool
	}{
		{"main", true},
		{"master", true},
		{"develop", true},
		{"dev", true},
		{"feature/foo", false},
		{"", false},
		{"  main  ", true},
		{"  main\n", true},
		{"release/1.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := isDefaultBranch(tt.branch)
			if got != tt.want {
				t.Errorf("isDefaultBranch(%q) = %v, want %v", tt.branch, got, tt.want)
			}
		})
	}
}

// --- NormalizeShortTitle ---

func TestNormalizeShortTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "clean title",
			input: "Adds user authentication",
			want:  "Adds user authentication",
		},
		{
			name:  "strips leading/trailing spaces",
			input: "  Adds auth  ",
			want:  "Adds auth",
		},
		{
			name:  "trims multiline to first line",
			input: "Adds auth\nSome description\nMore text",
			want:  "Adds auth",
		},
		{
			name:  "strips backticks",
			input: "`Adds auth`",
			want:  "Adds auth",
		},
		{
			name:  "strips single backtick prefix",
			input: "`Adds auth",
			want:  "Adds auth",
		},
		{
			name:  "strips single backtick suffix",
			input: "Adds auth`",
			want:  "Adds auth",
		},
		{
			name:  "strips title: prefix",
			input: "Title: Adds auth",
			want:  "Adds auth",
		},
		{
			name:  "strips title: prefix lowercase",
			input: "title: Adds auth",
			want:  "Adds auth",
		},
		{
			name:  "strips multiline and backticks and title prefix",
			// First line: "title: `Adds auth" — no trailing backtick on first line,
			// so backtick before "Adds" remains (Trim only removes leading/trailing).
			input: "title: `Adds auth\nmore stuff`",
			want:  "`Adds auth",
		},
		{
			name:  "multiline first line has backticks and title prefix",
			// First line after split: "title:   `Adds auth`"
			// Trim(backtick) removes trailing ` → "title:   `Adds auth"
			// TrimPrefix("title:") → "   `Adds auth" → TrimSpace → "`Adds auth"
			input: "  title:   `Adds auth`  \nDescription here",
			want:  "`Adds auth",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
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

// --- TruncateRunes ---

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{
			name: "shorter than max",
			s:    "hello",
			max:  10,
			want: "hello",
		},
		{
			name: "exact length",
			s:    "hello",
			max:  5,
			want: "hello",
		},
		{
			name: "over max with room for ellipsis",
			s:    "Hello, World!",
			max:  10,
			want: "Hello,...",
		},
		{
			name: "3 chars max",
			s:    "abcdef",
			max:  3,
			want: "abc",
		},
		{
			name: "0 max",
			s:    "anything",
			max:  0,
			want: "",
		},
		{
			name: "negative max",
			s:    "anything",
			max:  -1,
			want: "",
		},
		{
			name: "empty string",
			s:    "",
			max:  10,
			want: "",
		},
		{
			name: "max of 4 (just above ellipsis threshold)",
			s:    "abcdefgh",
			max:  4,
			want: "a...",
		},
		{
			name: "unicode characters",
			s:    "foobarbaz",
			max:  9,
			want: "foobarbaz",
		},
		{
			name: "unicode exact length",
			s:    "foobarbaz",
			max:  9,
			want: "foobarbaz",
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

func TestTruncateRunes_Unicode(t *testing.T) {
	// For unicode, max-3 runes + "..." should produce max runes total (3 chars + 3 dots)
	got := TruncateRunes("foobarbaz", 6)
	want := "foo..."
	if got != want {
		t.Errorf("TruncateRunes(%q, 6) = %q, want %q", "foobarbaz", got, want)
	}
}

// --- WrapText ---

func TestWrapText(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		lineLength int
		want       string
	}{
		{
			name:       "empty text",
			text:       "",
			lineLength: 72,
			want:       "",
		},
		{
			name:       "single word",
			text:       "hello",
			lineLength: 72,
			want:       "hello",
		},
		{
			name:       "multiple words fit on one line",
			text:       "hello world foo bar",
			lineLength: 72,
			want:       "hello world foo bar",
		},
		{
			name:       "long paragraph wraps at line length",
			text:       "This is a somewhat long paragraph that should be wrapped into multiple lines at the specified line length boundary.",
			lineLength: 40,
			want: "This is a somewhat long paragraph that\nshould be wrapped into multiple lines at\nthe specified line length boundary.",
		},
		{
			name:       "multiple paragraphs",
			text:       "First paragraph with some text.\n\nSecond paragraph here.",
			lineLength: 72,
			want:       "First paragraph with some text.\n\nSecond paragraph here.",
		},
		{
			name:       "multiple paragraphs with long lines",
			text:       "This is a very long first paragraph that will need to be wrapped across multiple lines in the output.\n\nThis is the second paragraph also quite lengthy and requiring wrapping.",
			lineLength: 40,
			want: "This is a very long first paragraph that\nwill need to be wrapped across multiple\nlines in the output.\n\nThis is the second paragraph also quite\nlengthy and requiring wrapping.",
		},
		{
			name:       "single word longer than line length",
			text:       "supercalifragilisticexpialidocious",
			lineLength: 10,
			want:       "supercalifragilisticexpialidocious",
		},
		{
			name:       "whitespace-only paragraph",
			text:       "   \n\nword",
			lineLength: 72,
			want:       "\n\nword",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapText(tt.text, tt.lineLength)
			if got != tt.want {
				t.Errorf("WrapText() mismatch\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// --- CleanCommitMessage ---

func TestCleanCommitMessage_ThinkTags(t *testing.T) {
	// The regex matches exactly <think>...</think> (with space before >)
	input := "<think>This is my thinking process</think>This is my reasoning.\n\nAdds user auth."
	got := CleanCommitMessage(input)
	if strings.Contains(got, "<think") {
		t.Errorf("think tags should be removed, got: %q", got)
	}
	if strings.Contains(got, "my thinking process") {
		t.Errorf("thinking content should be removed, got: %q", got)
	}
	if !strings.Contains(got, "This is my reasoning.") {
		t.Errorf("expected content after closing tag to remain, got: %q", got)
	}
	if !strings.Contains(got, "Adds user auth.") {
		t.Errorf("expected commit content to remain, got: %q", got)
	}
}

func TestCleanCommitMessage_ThinkTagsMultiline(t *testing.T) {
	input := "<think>\nMultiple lines\nof thinking\n</think>Multimedia comment\n\nThe actual commit message"
	got := CleanCommitMessage(input)
	if strings.Contains(got, "<think") {
		t.Errorf("think tags should be removed, got: %q", got)
	}
	if strings.Contains(got, "</think") {
		t.Errorf("closing think tag should be removed, got: %q", got)
	}
	if strings.Contains(got, "of thinking") {
		t.Errorf("thinking content should be removed, got: %q", got)
	}
	if !strings.Contains(got, "Multimedia comment") {
		t.Errorf("expected content after opening tag to remain, got: %q", got)
	}
	if !strings.Contains(got, "The actual commit message") {
		t.Errorf("expected actual message to remain, got: %q", got)
	}
}

func TestCleanCommitMessage_NoThinkTags(t *testing.T) {
	input := "feat: add user authentication\n\nImplements login."
	got := CleanCommitMessage(input)
	if got != input {
		t.Errorf("normal message should be unchanged, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONFunctionCall(t *testing.T) {
	input := `{"type": "function", "name": "generateCommitMessage", "parameters": {"commitMessageFormat": "feat: add user auth"}}`
	got := CleanCommitMessage(input)
	if got != "feat: add user auth" {
		t.Errorf("expected function call commit message, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONFunctionCallOriginalRequest(t *testing.T) {
	input := `{"type": "function", "name": "generateCommitMessage", "parameters": {"originalUserRequest": "Add authentication"}}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "add authentication") {
		t.Errorf("expected original request in message, got: %q", got)
	}
	if !strings.HasPrefix(got, "[>>] feat:") {
		t.Errorf("expected feat prefix, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONSingleKey(t *testing.T) {
	input := `{"Add authentication": "Implement login functionality and user sessions"}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "feat") && !strings.Contains(got, "Add authentication") {
		t.Errorf("expected formatted commit with key, got: %q", got)
	}
	if !strings.Contains(got, "Implement login functionality") {
		t.Errorf("expected description in commit, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONSingleKeyFix(t *testing.T) {
	input := `{"Fix login bug": "Correct password validation logic"}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "[bug]") {
		t.Errorf("expected bug emoji for fix, got: %q", got)
	}
	if !strings.Contains(got, "fix:") {
		t.Errorf("expected fix prefix, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONSingleKeyDocs(t *testing.T) {
	input := `{"Update README": "Add installation instructions"}`
	got := CleanCommitMessage(input)
	// "document" contains "doc" which matches the docs check
	if !strings.Contains(got, "Update README") {
		t.Errorf("expected key in commit, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONSingleKeyDocsExact(t *testing.T) {
	// "docs:" prefix is only triggered when titleLower contains "doc"
	input := `{"Document API": "Add Swagger docs"}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "[edit]") {
		t.Errorf("expected edit emoji for docs, got: %q", got)
	}
	if !strings.Contains(got, "docs:") {
		t.Errorf("expected docs prefix, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONSingleKeyEnhance(t *testing.T) {
	input := `{"Enhance performance": "Optimize database queries"}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "[*]") {
		t.Errorf("expected enhance emoji, got: %q", got)
	}
	if !strings.Contains(got, "enhance:") {
		t.Errorf("expected enhance prefix, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONSingleKeyRefactor(t *testing.T) {
	input := `{"Refactor code": "Simplify authentication flow"}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "[recycle]") {
		t.Errorf("expected recycle emoji for refactor, got: %q", got)
	}
	if !strings.Contains(got, "refactor:") {
		t.Errorf("expected refactor prefix, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONSingleKeyTest(t *testing.T) {
	input := `{"Test coverage": "Add unit tests for auth module"}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "[test]") {
		t.Errorf("expected test emoji, got: %q", got)
	}
	if !strings.Contains(got, "test:") {
		t.Errorf("expected test prefix, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONFallback(t *testing.T) {
	// JSON that is valid but doesn't match any specific pattern
	input := `{"key": 123}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "Add new functionality") {
		t.Errorf("expected JSON fallback message, got: %q", got)
	}
}

func TestCleanCommitMessage_JSONInvalid(t *testing.T) {
	// Invalid JSON but starts/ends with braces
	input := `{invalid json content here}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "Add new functionality") {
		t.Errorf("expected JSON fallback for invalid JSON, got: %q", got)
	}
}

func TestCleanCommitMessage_MarkdownFences(t *testing.T) {
	input := "```\nfeat: add user auth\n\nImplements login functionality\n```"
	got := CleanCommitMessage(input)
	if strings.HasPrefix(got, "```") || strings.HasSuffix(got, "```") {
		t.Errorf("markdown fences should be removed, got: %q", got)
	}
	if !strings.Contains(got, "feat: add user auth") {
		t.Errorf("expected content preserved after fence removal, got: %q", got)
	}
}

func TestCleanCommitMessage_MarkdownFencesWithLanguage(t *testing.T) {
	input := "```git\nfeat: add user auth\n\nImplements login\n```"
	got := CleanCommitMessage(input)
	if strings.HasPrefix(got, "```") || strings.HasPrefix(got, "git") {
		t.Errorf("language specifier should be removed, got: %q", got)
	}
	if !strings.Contains(got, "feat: add user auth") {
		t.Errorf("expected content preserved, got: %q", got)
	}
}

func TestCleanCommitMessage_MultipleBlankLines(t *testing.T) {
	input := "feat: add user auth\n\n\n\n\nImplements login functionality"
	got := CleanCommitMessage(input)
	// Should normalize to exactly one blank line between title and description
	parts := strings.SplitN(got, "\n\n", 2)
	if len(parts) != 2 {
		t.Fatalf("expected title and description separated by one blank line, got: %q", got)
	}
	if parts[0] != "feat: add user auth" {
		t.Errorf("title wrong: %q", parts[0])
	}
	if !strings.Contains(parts[1], "Implements login functionality") {
		t.Errorf("description wrong: %q", parts[1])
	}
	// Verify no multiple consecutive blank lines
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("should not have multiple consecutive blank lines, got: %q", got)
	}
}

func TestCleanCommitMessage_Normal(t *testing.T) {
	input := "feat: add user auth\n\nImplements login and registration."
	got := CleanCommitMessage(input)
	if got != input {
		t.Errorf("normal message should be unchanged, got: %q", got)
	}
}

func TestCleanCommitMessage_NormalWithTrailingWhitespace(t *testing.T) {
	input := "feat: add user auth  \n\nImplements login."
	got := CleanCommitMessage(input)
	if !strings.HasPrefix(got, "feat: add user auth") {
		t.Errorf("title should be preserved, got: %q", got)
	}
}

func TestCleanCommitMessage_JsonWithTwoKeys(t *testing.T) {
	// JSON with 2 keys doesn't match the single-key branch, so it falls through to the multi-key JSON fallback
	input := `{"title": "desc", "other": "value"}`
	got := CleanCommitMessage(input)
	if !strings.Contains(got, "Add new functionality") {
		t.Errorf("expected JSON fallback for multi-key JSON, got: %q", got)
	}
}

// --- ParseCommitMessage ---

func TestParseCommitMessage_Normal(t *testing.T) {
	input := "feat: add user auth\n\nThis implements login and registration\nfor the application."
	note, desc, err := ParseCommitMessage(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if note != "feat: add user auth" {
		t.Errorf("note = %q, want %q", note, "feat: add user auth")
	}
	if desc != "This implements login and registration\nfor the application." {
		t.Errorf("desc = %q, want %q", desc, "This implements login and registration\nfor the application.")
	}
}

func TestParseCommitMessage_WithExtraBlankLine(t *testing.T) {
	input := "feat: add user auth\n\n\nExtra description."
	note, desc, err := ParseCommitMessage(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if note != "feat: add user auth" {
		t.Errorf("note = %q", note)
	}
	// ParseCommitMessage joins lines[2:], so the third line is empty
	if !strings.Contains(desc, "Extra description.") {
		t.Errorf("desc = %q, want to contain 'Extra description.'", desc)
	}
}

func TestParseCommitMessage_TooFewLines(t *testing.T) {
	// Single line message
	_, _, err := ParseCommitMessage("just a title")
	if err == nil {
		t.Error("expected error for single-line message")
	}

	// Empty message
	_, _, err = ParseCommitMessage("")
	if err == nil {
		t.Error("expected error for empty message")
	}

	// Only title + one blank line (2 lines total, need at least 2: len(lines) < 2 is false)
	// Actually with "title\n" we have 2 lines, so it passes but description = lines[2:] = ""
	note, desc, err := ParseCommitMessage("title\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if note != "title" {
		t.Errorf("note = %q, want %q", note, "title")
	}
	if desc != "" {
		t.Errorf("desc = %q, want empty", desc)
	}
}
