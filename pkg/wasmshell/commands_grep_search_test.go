package wasmshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The browser workspace search (webui cloudWasmHandlers handleWasmSearch)
// runs grep with -F for a plain query, -E for a regex, -w for whole word and
// -i unless case-sensitive, the query after -e. These pin that each option
// changes the hits as the daemon's search does.
func TestGrep_SearchPanelOptions(t *testing.T) {
	dir := t.TempDir()
	content := "a.b literal\naxb wildcard\na.bc suffix\nA.B upper\n-flag dash\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		flags string
		query string
		want  []string
	}{
		{"literal, case-insensitive", "-i -F", "a.b", []string{"a.b literal", "a.bc suffix", "A.B upper"}},
		{"literal, case-sensitive", "-F", "a.b", []string{"a.b literal", "a.bc suffix"}},
		{"literal, whole word", "-i -F -w", "a.b", []string{"a.b literal", "A.B upper"}},
		{"regex", "-E", "a.b", []string{"a.b literal", "axb wildcard", "a.bc suffix"}},
		{"regex, whole word", "-E -w", "a.b", []string{"a.b literal", "axb wildcard"}},
		{"query starting with a dash", "-F", "-flag", []string{"-flag dash"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := "grep " + tc.flags + " -rn --include='*' -e '" + tc.query + "' '" + dir + "'"
			r := ParseAndExecute(cmd)
			if r.ExitCode != 0 {
				t.Fatalf("%s: exit = %d stderr = %q", cmd, r.ExitCode, r.Stderr)
			}
			var got []string
			for _, line := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
				parts := strings.SplitN(line, ":", 3)
				if len(parts) == 3 {
					got = append(got, parts[2])
				}
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("%s: hits = %q, want %q", cmd, got, tc.want)
			}
		})
	}
}
