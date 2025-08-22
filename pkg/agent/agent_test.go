package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/utils"
)

func TestInferCategory(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"please add unit tests", "test"},
		{"fix bug in parsing", "fix"},
		{"update docs and header comment", "docs"},
		{"code review please", "review"},
		{"implement feature", "code"},
	}
	for _, c := range cases {
		got := inferCategory(c.in)
		if got != c.want {
			t.Fatalf("inferCategory(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInferComplexity(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"please refactor module", "complex"},
		{"architect new layout", "complex"},
		{"touch multiple files", "complex"},
		{"design changes needed", "complex"},
		{"add comment", "simple"},
		{"fix typo in single file", "simple"},
		{"some generic work", "moderate"},
	}
	for _, c := range cases {
		got := inferComplexity(c.in)
		if got != c.want {
			t.Fatalf("inferComplexity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractExplicitPath(t *testing.T) {
	in := "Please modify pkg/agent/agent.go to add docs"
	got := extractExplicitPath(in)
	if !strings.HasSuffix(got, filepath.ToSlash("pkg/agent/agent.go")) {
		t.Fatalf("extractExplicitPath(%q) = %q, want suffix %q", in, got, "pkg/agent/agent.go")
	}
}

func TestParseSimpleReplacement(t *testing.T) {
	cases := []struct {
		in       string
		ok       bool
		old, new string
	}{
		{"change 'old' to 'new'", true, "old", "new"},
		{"Change \"foo\" to \"bar\"", true, "foo", "bar"},
		{"change hello to world.", true, "hello", "world"},
		{"nothing to do", false, "", ""},
	}
	for _, c := range cases {
		old, new, ok := parseSimpleReplacement(c.in)
		if ok != c.ok || old != c.old || new != c.new {
			t.Fatalf("parseSimpleReplacement(%q) = (%q,%q,%v) want (%q,%q,%v)", c.in, old, new, ok, c.old, c.new, c.ok)
		}
	}
}

func TestParseSimpleAppend(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		text string
	}{
		{"append the word delta to the end of file", true, "delta"},
		{"append \"gamma\" to the end", true, "gamma"},
		{"no append here", false, ""},
	}
	for _, c := range cases {
		text, ok := parseSimpleAppend(c.in)
		if ok != c.ok || text != c.text {
			t.Fatalf("parseSimpleAppend(%q) = (%q,%v) want (%q,%v)", c.in, text, ok, c.text, c.ok)
		}
	}
}

func TestWantsEnthusiasmBoost(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"make it more enthusiastic", true},
		{"make it more excited", true},
		{"friendlier tone", true},
		{"neutral", false},
	}
	for _, c := range cases {
		got := wantsEnthusiasmBoost(c.in)
		if got != c.want {
			t.Fatalf("wantsEnthusiasmBoost(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestInterpretEscapes(t *testing.T) {
	in := "line1\\nline2\\tX\\r"
	want := "line1\nline2\tX\r"
	if got := interpretEscapes(in); got != want {
		t.Fatalf("interpretEscapes got %q want %q", got, want)
	}
}

func TestEnsureLineAtFunctionStart_InsertsOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tmp.go")
	content := "package tmp\n\nfunc greet() {\n\tprintln(\"hi\")\n}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	line := "println(\"started\")"
	if err := ensureLineAtFunctionStart(path, "greet", line); err != nil {
		t.Fatalf("ensureLineAtFunctionStart error: %v", err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if !strings.Contains(s, "\n\t"+line+"\n") {
		t.Fatalf("expected inserted line, got:\n%s", s)
	}
	// Idempotent on second call
	if err := ensureLineAtFunctionStart(path, "greet", line); err != nil {
		t.Fatalf("second ensureLineAtFunctionStart error: %v", err)
	}
	b2, _ := os.ReadFile(path)
	if strings.Count(string(b2), line) != 1 {
		t.Fatalf("expected single occurrence of inserted line")
	}
}

func TestTryRemoveTrailingExtraBrace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tmp.go")
	content := "package tmp\n\nfunc x() {}\n}\n\n" // trailing extra '}' with trailing newline
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	logger := utils.GetLogger(true)
	if err := tryRemoveTrailingExtraBrace(path, logger); err != nil {
		t.Fatalf("tryRemoveTrailingExtraBrace error: %v", err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if strings.Contains(s, "\n}\n\n") {
		t.Fatalf("expected trailing extra brace line removed, got content:\n%s", s)
	}
}
