package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/validation"
)

// ---------------------------------------------------------------------------
// parseGofmtError — pure helper
// ---------------------------------------------------------------------------

func TestParseGofmtError_StandardInput(t *testing.T) {
	line, col, ok := parseGofmtError("<standard input>:42:5: expected declaration")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if line != 42 {
		t.Errorf("line = %d, want 42", line)
	}
	if col != 5 {
		t.Errorf("col = %d, want 5", col)
	}
}

func TestParseGofmtError_Stdin(t *testing.T) {
	line, col, ok := parseGofmtError("<stdin>:10:2: expected 'package'")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if line != 10 {
		t.Errorf("line = %d, want 10", line)
	}
	if col != 2 {
		t.Errorf("col = %d, want 2", col)
	}
}

func TestParseGofmtError_WithSyntaxPrefix(t *testing.T) {
	line, col, ok := parseGofmtError("syntax error: <standard input>:5:3: expected ';'")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if line != 5 {
		t.Errorf("line = %d, want 5", line)
	}
	if col != 3 {
		t.Errorf("col = %d, want 3", col)
	}
}

func TestParseGofmtError_NoColons(t *testing.T) {
	_, _, ok := parseGofmtError("just an error message")
	if ok {
		t.Error("expected ok=false for message with no colons")
	}
}

func TestParseGofmtError_OnlyOneColon(t *testing.T) {
	_, _, ok := parseGofmtError("foo:bar")
	if ok {
		t.Error("expected ok=false for single colon")
	}
}

func TestParseGofmtError_NonNumericLine(t *testing.T) {
	_, _, ok := parseGofmtError("<file>:abc:5: error")
	if ok {
		t.Error("expected ok=false for non-numeric line")
	}
}

func TestParseGofmtError_NonNumericCol(t *testing.T) {
	_, _, ok := parseGofmtError("<file>:10:xyz: error")
	if ok {
		t.Error("expected ok=false for non-numeric column")
	}
}

func TestParseGofmtError_EmptyString(t *testing.T) {
	_, _, ok := parseGofmtError("")
	if ok {
		t.Error("expected ok=false for empty string")
	}
}

func TestParseGofmtError_MissingSecondColon(t *testing.T) {
	_, _, ok := parseGofmtError("<file>:10:5")
	if ok {
		t.Error("expected ok=false for missing second colon (no message after col)")
	}
}

func TestParseGofmtError_LargeNumbers(t *testing.T) {
	line, col, ok := parseGofmtError("<standard input>:999:100: some error")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if line != 999 {
		t.Errorf("line = %d, want 999", line)
	}
	if col != 100 {
		t.Errorf("col = %d, want 100", col)
	}
}

// ---------------------------------------------------------------------------
// lineColToOffsets — pure helper
// ---------------------------------------------------------------------------

func TestLineColToOffsets_FirstLine(t *testing.T) {
	content := "hello world"
	from, to := lineColToOffsets(1, 1, content)
	if from != 0 {
		t.Errorf("from = %d, want 0", from)
	}
	// to should be extended to cover a token
	if to < from {
		t.Errorf("to = %d, should be >= from = %d", to, from)
	}
}

func TestLineColToOffsets_SecondLine(t *testing.T) {
	content := "line1\nline2"
	from, _ := lineColToOffsets(2, 1, content)
	// offset of "line2" start: 5+1=6
	if from != 6 {
		t.Errorf("from = %d, want 6", from)
	}
}

func TestLineColToOffsets_MiddleOfLine(t *testing.T) {
	content := "hello world"
	from, _ := lineColToOffsets(1, 7, content)
	if from != 6 {
		t.Errorf("from = %d, want 6", from)
	}
}

func TestLineColToOffsets_BeyondLine(t *testing.T) {
	content := "hello"
	from, _ := lineColToOffsets(1, 100, content)
	if from != 5 {
		t.Errorf("from = %d, want 5 (clamped to end of line)", from)
	}
}

func TestLineColToOffsets_BeyondContent(t *testing.T) {
	content := "a\nb"
	from, to := lineColToOffsets(10, 1, content)
	if from != len(content) {
		t.Errorf("from = %d, want %d (clamped to content end)", from, len(content))
	}
	if to != len(content) {
		t.Errorf("to = %d, want %d", to, len(content))
	}
}

func TestLineColToOffsets_ZeroLine(t *testing.T) {
	content := "hello"
	from, _ := lineColToOffsets(0, 1, content)
	// line < 1 → clamped to 1
	if from != 0 {
		t.Errorf("from = %d, want 0 (line 0 clamped to 1, col 1 → offset 0)", from)
	}
}

func TestLineColToOffsets_NegativeCol(t *testing.T) {
	content := "hello"
	from, _ := lineColToOffsets(1, -5, content)
	// col < 1 → clamped to 1
	if from != 0 {
		t.Errorf("from = %d, want 0", from)
	}
}

func TestLineColToOffsets_LastLineMultiLine(t *testing.T) {
	content := "a\nb\nc"
	from, _ := lineColToOffsets(3, 1, content)
	// offset of "c" start: 1+1+1+1=4
	if from != 4 {
		t.Errorf("from = %d, want 4", from)
	}
}

func TestLineColToOffsets_ToAtLeastFrom(t *testing.T) {
	// When extendToTokenEnd returns the same byte, to = from+1
	content := " "
	_, to := lineColToOffsets(1, 1, content)
	if to < 1 {
		t.Errorf("to = %d, should be >= 1", to)
	}
}

func TestLineColToOffsets_MultiByteContent(t *testing.T) {
	content := "hello\n世界"
	from, _ := lineColToOffsets(2, 1, content)
	// offset of "世界" start: 5+1=6
	if from != 6 {
		t.Errorf("from = %d, want 6", from)
	}
}

// ---------------------------------------------------------------------------
// extendToTokenEnd — pure helper
// ---------------------------------------------------------------------------

func TestExtendToTokenEnd_BeyondContent(t *testing.T) {
	content := "hello"
	got := extendToTokenEnd(content, 10)
	if got != 10 {
		t.Errorf("got = %d, want 10 (clamped to byteOffset when >= len)", got)
	}
}

func TestExtendToTokenEnd_NegativeOffset(t *testing.T) {
	content := "hello"
	got := extendToTokenEnd(content, -5)
	if got == -5 {
		t.Error("negative offset should be clamped to 0")
	}
	if got < 0 {
		t.Errorf("got = %d, should be >= 0", got)
	}
}

func TestExtendToTokenEnd_SimpleWord(t *testing.T) {
	content := "hello world"
	got := extendToTokenEnd(content, 0)
	// "hello" is 5 bytes; next char is ' ' (delimiter)
	if got != 5 {
		t.Errorf("got = %d, want 5 (end of 'hello')", got)
	}
}

func TestExtendToTokenEnd_WordMiddle(t *testing.T) {
	content := "hello world"
	got := extendToTokenEnd(content, 6)
	// "world" is 5 bytes; content ends at 11
	if got != 11 {
		t.Errorf("got = %d, want 11 (end of 'world')", got)
	}
}

func TestExtendToTokenEnd_AtDelimiter(t *testing.T) {
	content := "hello world"
	got := extendToTokenEnd(content, 5)
	// space is a delimiter; end == byteOffset, so end = byteOffset + 1
	if got != 6 {
		t.Errorf("got = %d, want 6 (one char past delimiter)", got)
	}
}

func TestExtendToTokenEnd_FullIdentifier(t *testing.T) {
	content := "funcName() { return x }"
	got := extendToTokenEnd(content, 0)
	// "funcName" is 8 chars; '(' is delimiter
	if got != 8 {
		t.Errorf("got = %d, want 8 (end of 'funcName')", got)
	}
}

func TestExtendToTokenEnd_ParenthesisDelimits(t *testing.T) {
	content := "foo(bar)"
	got := extendToTokenEnd(content, 0)
	if got != 3 {
		t.Errorf("got = %d, want 3 (end of 'foo' before '(')", got)
	}
}

func TestExtendToTokenEnd_EmptyContent(t *testing.T) {
	content := ""
	got := extendToTokenEnd(content, 0)
	if got != 0 {
		t.Errorf("got = %d, want 0", got)
	}
}

func TestExtendToTokenEnd_NewlineDelimits(t *testing.T) {
	content := "hello\nworld"
	got := extendToTokenEnd(content, 0)
	if got != 5 {
		t.Errorf("got = %d, want 5 (end of 'hello' before newline)", got)
	}
}

func TestExtendToTokenEnd_TabDelimits(t *testing.T) {
	content := "hello\tworld"
	got := extendToTokenEnd(content, 0)
	if got != 5 {
		t.Errorf("got = %d, want 5", got)
	}
}

func TestExtendToTokenEnd_CommaDelimits(t *testing.T) {
	content := "a,b"
	got := extendToTokenEnd(content, 0)
	if got != 1 {
		t.Errorf("got = %d, want 1", got)
	}
}

func TestExtendToTokenEnd_CommaStart(t *testing.T) {
	content := ",hello"
	got := extendToTokenEnd(content, 0)
	// comma is delimiter, so end = 0 + 1 = 1
	if got != 1 {
		t.Errorf("got = %d, want 1", got)
	}
}

func TestExtendToTokenEnd_DigitWord(t *testing.T) {
	content := "123abc"
	got := extendToTokenEnd(content, 0)
	if got != 6 {
		t.Errorf("got = %d, want 6", got)
	}
}

func TestExtendToTokenEnd_UnderscoreInWord(t *testing.T) {
	content := "my_var_name"
	got := extendToTokenEnd(content, 0)
	if got != 11 {
		t.Errorf("got = %d, want 11", got)
	}
}

func TestExtendToTokenEnd_SingleChar(t *testing.T) {
	content := "a"
	got := extendToTokenEnd(content, 0)
	if got != 1 {
		t.Errorf("got = %d, want 1", got)
	}
}

func TestExtendToTokenEnd_AtEndOfContent(t *testing.T) {
	content := "hello"
	got := extendToTokenEnd(content, 5)
	if got != 5 {
		t.Errorf("got = %d, want 5", got)
	}
}

// ---------------------------------------------------------------------------
// isExtDelimiter — pure helper
// ---------------------------------------------------------------------------

func TestIsExtDelimiter_AllDelimiters(t *testing.T) {
	delims := []rune{
		' ', '\t', '\n', '\r',
		'(', ')', '{', '}', '[', ']',
		',', ';', ':', '+', '-', '*', '/',
		'=', '!', '<', '>', '&', '|', '^', '%',
		'"', '\'',
	}
	for _, ch := range delims {
		if !isExtDelimiter(ch) {
			t.Errorf("isExtDelimiter(%q) = false, want true", ch)
		}
	}
}

func TestIsExtDelimiter_NonDelimiters(t *testing.T) {
	nonDelims := []rune{'a', 'z', 'A', 'Z', '0', '9', '_', '.', '@', '#', '$'}
	for _, ch := range nonDelims {
		if isExtDelimiter(ch) {
			t.Errorf("isExtDelimiter(%q) = true, want false", ch)
		}
	}
}

func TestIsExtDelimiter_NonASCII(t *testing.T) {
	if isExtDelimiter('é') {
		t.Error("isExtDelimiter('é') should be false")
	}
	if isExtDelimiter('世') {
		t.Error("isExtDelimiter('世') should be false")
	}
}

// ---------------------------------------------------------------------------
// diagnosticToOffsets — pure helper
// ---------------------------------------------------------------------------

func TestDiagnosticToOffsets_GofmtWithLineCol(t *testing.T) {
	content := "package main\n\nfunc foo() {"
	d := validation.Diagnostic{
		Source:  "gofmt",
		Message: "<standard input>:3:6: expected declaration",
	}
	from, to := diagnosticToOffsets(d, content)
	// line 3 is "func foo() {", col 6 should be at "foo" or "f" offset within that line
	// line 3 starts at offset 16 (len("package main\n\n")=14+2), col 6 is at offset 21
	if from < 13 || from > 30 {
		t.Logf("from = %d (line 3, col 6 parsed from gofmt message)", from)
	}
	if to < from {
		t.Errorf("to = %d, should be >= from = %d", to, from)
	}
}

func TestDiagnosticToOffsets_GofmtNoParseableLine(t *testing.T) {
	content := "hello"
	d := validation.Diagnostic{
		Source:  "gofmt",
		Message: "not parseable at all",
		Line:    0,
		Column:  0,
	}
	from, to := diagnosticToOffsets(d, content)
	// Falls through to Line=0, then to len(content)
	if from != 0 {
		t.Errorf("from = %d, want 0", from)
	}
	if to != len(content) {
		t.Errorf("to = %d, want %d", to, len(content))
	}
}

func TestDiagnosticToOffsets_GoimportsEntireFile(t *testing.T) {
	content := "package main"
	d := validation.Diagnostic{
		Source: "goimports",
		Line:   1,
		Column: 1,
	}
	from, to := diagnosticToOffsets(d, content)
	if from != 0 {
		t.Errorf("from = %d, want 0", from)
	}
	if to != len(content) {
		t.Errorf("to = %d, want %d (entire file)", to, len(content))
	}
}

func TestDiagnosticToOffsets_GoimportsSpecificLine(t *testing.T) {
	content := "package main"
	d := validation.Diagnostic{
		Source: "goimports",
		Line:   1,
		Column: 5, // Not col=1, so not the "entire file" case
	}
	from, _ := diagnosticToOffsets(d, content)
	// Falls through to lineColToOffsets
	if from != 4 {
		t.Errorf("from = %d, want 4", from)
	}
}

func TestDiagnosticToOffsets_DirectLineCol(t *testing.T) {
	content := "line1\nline2\nline3"
	d := validation.Diagnostic{
		Source: "other",
		Line:   2,
		Column: 3,
	}
	from, _ := diagnosticToOffsets(d, content)
	// line 2 starts at offset 6 (5+1)
	if from != 8 {
		t.Errorf("from = %d, expected ~8", from)
	}
}

func TestDiagnosticToOffsets_FallbackEntireContent(t *testing.T) {
	content := "some content"
	d := validation.Diagnostic{
		Source: "unknown",
		Line:   0,
		Column: 0,
	}
	from, to := diagnosticToOffsets(d, content)
	if from != 0 || to != len(content) {
		t.Errorf("fallback: from=%d, to=%d, want 0, %d", from, to, len(content))
	}
}

// ---------------------------------------------------------------------------
// validationToFrontend — pure helper
// ---------------------------------------------------------------------------

func TestValidationToFrontend_Basic(t *testing.T) {
	content := "package main"
	d := validation.Diagnostic{
		Severity: "error",
		Message:  "missing import",
		Source:   "goimports",
		Line:     1,
		Column:   1,
	}
	fe := validationToFrontend(d, content)
	if fe.Severity != "error" {
		t.Errorf("Severity = %q, want %q", fe.Severity, "error")
	}
	if fe.Message != "missing import" {
		t.Errorf("Message = %q, want %q", fe.Message, "missing import")
	}
	if fe.Source != "goimports" {
		t.Errorf("Source = %q, want %q", fe.Source, "goimports")
	}
	// goimports with Line=1, Column=1 → entire file
	if fe.From != 0 || fe.To != len(content) {
		t.Errorf("From=%d, To=%d, want 0, %d", fe.From, fe.To, len(content))
	}
}

func TestValidationToFrontend_GofmtParsed(t *testing.T) {
	content := "package main\n\nfunc foo() { return }"
	d := validation.Diagnostic{
		Severity: "error",
		Message:  "<standard input>:3:6: expected declaration",
		Source:   "gofmt",
	}
	fe := validationToFrontend(d, content)
	if fe.Source != "gofmt" {
		t.Errorf("Source = %q, want %q", fe.Source, "gofmt")
	}
	if fe.From < 0 || fe.To > len(content) {
		t.Errorf("offsets out of bounds: from=%d, to=%d, len=%d", fe.From, fe.To, len(content))
	}
}

func TestValidationToFrontend_DirectLineCol(t *testing.T) {
	content := "line1\nline2"
	d := validation.Diagnostic{
		Severity: "warning",
		Message:  "unused variable",
		Source:   "vet",
		Line:     2,
		Column:   1,
	}
	fe := validationToFrontend(d, content)
	if fe.Severity != "warning" {
		t.Errorf("Severity = %q, want %q", fe.Severity, "warning")
	}
	if fe.Message != "unused variable" {
		t.Errorf("Message = %q, want %q", fe.Message, "unused variable")
	}
}
