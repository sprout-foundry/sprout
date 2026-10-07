package langguard

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExtractProse is the table test over prose extraction:
// code, URLs, file paths and quoted spans are removed, while genuine
// prose — including prose that merely contains slashes or apostrophes —
// survives.
func TestExtractProse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		// keep: substrings that must remain in the extracted prose.
		keep []string
		// gone: substrings that must not remain in the extracted prose.
		gone []string
	}{
		{
			name: "fenced code block with language tag is stripped",
			in:   "Here is the fix:\n\n```go\nfunc main() {\n\tfmt.Println(\"hola mundo\")\n}\n```\n\nApply it and re-run.",
			keep: []string{"Here is the fix:", "Apply it and re-run."},
			gone: []string{"func main", "fmt.Println", "hola mundo", "```"},
		},
		{
			name: "tilde fenced block is stripped",
			in:   "Before:\n~~~python\nx = 1\n~~~\nAfter.",
			keep: []string{"Before:", "After."},
			gone: []string{"x = 1", "~~~"},
		},
		{
			name: "unclosed fence strips to the end",
			in:   "intro ```go\ntruncated code",
			keep: []string{"intro"},
			gone: []string{"truncated code"},
		},
		{
			name: "inline code, URL, path and quoted span are stripped",
			in: "Run `go test ./pkg/auth/...` first, see https://example.com/docs " +
				"and /Users/dev/sprout/main.go, you said \"hola, ¿estás bien?\" earlier.",
			keep: []string{"Run first", "you said earlier"},
			gone: []string{"go test", "./pkg/auth", "https://example.com", "/Users/dev/sprout", "hola"},
		},
		{
			name: "www-prefixed domain is stripped",
			in:   "visit www.example.com today",
			keep: []string{"visit today"},
			gone: []string{"www.example.com"},
		},
		{
			name: "home, relative and Windows paths are stripped",
			in:   "edit ~/config/app.yaml, run ./scripts/build.sh and copy C:\\Users\\dev\\notes.txt now",
			keep: []string{"edit", "run", "copy", "now"},
			gone: []string{"~/config/app.yaml", "./scripts/build.sh", `C:\Users\dev\notes.txt`},
		},
		{
			name: "absolute paths with letters are stripped",
			in:   "See /docs/api page for details.",
			keep: []string{"See", "page for details."},
			gone: []string{"/docs/api"},
		},
		{
			name: "ordinary prose with slashes survives",
			in:   "It is high/low and 3/4 of the ratio 1/2 and/or style",
			keep: []string{"high/low", "3/4", "1/2", "and/or"},
		},
		{
			name: "dates and ratios survive",
			in:   "5/6 of the plan, section 5/6, date 2026-10-04",
			keep: []string{"5/6", "2026-10-04"},
		},
		{
			name: "double-quoted span is stripped",
			in:   "You said \"hola, ¿todo bien?\" earlier.",
			keep: []string{"You said", "earlier."},
			gone: []string{"hola"},
		},
		{
			name: "typographic double-quoted span is stripped",
			in:   "El usuario dijo “hola, ¿cómo estás?” y se marchó.",
			keep: []string{"El usuario dijo", "y se marchó."},
			gone: []string{"hola"},
		},
		{
			name: "guillemet spans are stripped",
			in:   "« bonjour à tous » and „hallo wie geht es dir“ rest",
			keep: []string{"and", "rest"},
			gone: []string{"bonjour", "hallo wie geht es dir"},
		},
		{
			name: "single-quoted span is stripped but apostrophes survive",
			in:   "It doesn't matter if it's a 'big deal' for the parser.",
			keep: []string{"doesn't", "it's a", "for the parser."},
			gone: []string{"big deal"},
		},
		{
			name: "pure prose is whitespace-normalized",
			in:   "The\n\n\tbuild  passed.\n",
			keep: []string{"The build passed."},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractProse(tc.in)
			for _, k := range tc.keep {
				assert.Contains(t, got, k, "extracted prose: %q", got)
			}
			for _, g := range tc.gone {
				assert.NotContains(t, got, g, "extracted prose: %q", got)
			}
		})
	}
}

// TestExtractProseExact pins exact outputs for the degenerate inputs:
// empty and garbage input yields "" without failing (the check is
// in-process and must never take a reply path down).
func TestExtractProseExact(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty input", "", ""},
		{"whitespace only", "  \n\t  ", ""},
		{"prose-only normalization", "The\n\n\tbuild  passed.\n", "The build passed."},
		{"code-only message", "```\nfunc main() {}\n```", ""},
		{"truncated unclosed block", "```\nunclosed", ""},
		{"binary garbage passes through", "\x00\x01", "\x00\x01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ExtractProse(tc.in))
		})
	}
}

// TestExtractProseIsTotal checks extraction on edge inputs that must not
// panic: a lone fence delimiter, an unclosed inline span or quote, and a
// long input.
func TestExtractProseIsTotal(t *testing.T) {
	inputs := []string{
		"```",
		"~~~",
		"`",
		"`unclosed",
		"````code```",
		"quote \"unclosed",
		"«unclosed",
		"„unclosed",
		"it's",
		"'unclosed",
		"/a/b",
		"C:\\",
		"~~~python",
		"~~~python\nx",
		strings.Repeat("word ", 1000),
	}
	for i, in := range inputs {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// Must not panic; the result may be anything sensible.
			_ = ExtractProse(in)
		})
	}
}

// TestExtractProseIdempotent: extracting an already-extracted string must
// not change it — the pipeline (extract once, then judge) is stable.
func TestExtractProseIdempotent(t *testing.T) {
	inputs := []string{
		"Here is the fix:\n\n```go\nfunc main() {}\n```\n\nApply it.",
		"Run `go test ./x/...` at https://example.com/docs in /Users/dev/a.go",
		"It doesn't matter if it's a 'big deal'.",
		"« bonjour » and „hallo“ and \"hola\"",
		"visit www.example.com and copy C:\\Users\\dev\\notes.txt now",
	}
	for _, in := range inputs {
		once := ExtractProse(in)
		assert.Equal(t, once, ExtractProse(once), "second extraction changed the result: %q", once)
	}
}
