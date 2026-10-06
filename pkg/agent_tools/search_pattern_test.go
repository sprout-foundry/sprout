//go:build !js

package tools

import "testing"

func TestCompileSearchPattern(t *testing.T) {
	cases := []struct {
		name          string
		pattern       string
		caseSensitive bool
		line          string
		want          bool
	}{
		{"alternation", "fooBar|bazQux", false, "x := bazQux()", true},
		{"wildcard", "func.*Handle", false, "func (s *S) HandleAPI()", true},
		{"char class", "SPROUT_[A-Z_]+", true, `os.Getenv("SPROUT_CONFIG")`, true},
		{"escaped paren", `getFileURI\(`, false, "getFileURI(path)", true},
		{"invalid regex falls back to literal", "getFileURI(", false, "getFileURI(path)", true},
		{"invalid regex literal does not overmatch", "getFileURI(", false, "getFileURI path", false},
		{"case insensitive by default", "handleapi", false, "HandleAPI", true},
		{"case sensitive", "handleapi", true, "HandleAPI", false},
		{"slash wrapped", "/^func /", false, "func main() {", true},
		{"plain text", "wakeup budget", false, "the wakeup budget resets", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			re, err := compileSearchPattern(tc.pattern, tc.caseSensitive)
			if err != nil {
				t.Fatalf("compileSearchPattern(%q): %v", tc.pattern, err)
			}
			if got := re.MatchString(tc.line); got != tc.want {
				t.Fatalf("pattern %q on %q: got %v, want %v", tc.pattern, tc.line, got, tc.want)
			}
		})
	}
}

func TestCompileSearchPatternStrictSlashRegex(t *testing.T) {
	if _, err := compileSearchPattern("/foo(/", false); err == nil {
		t.Fatal("an invalid /slash-wrapped/ regex should error, not fall back")
	}
}

func TestLiteralPatternForProseMatchesStems(t *testing.T) {
	re, err := compileSearchPattern(literalPatternFor("where do we retry failed connections"), false)
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("func retryConnect() error") {
		t.Fatalf("prose stems %q should match a line containing one stem", re.String())
	}
}
