package agent

import "testing"

func TestShellLooksReadOnly(t *testing.T) {
	cases := []struct {
		cmd      string
		readOnly bool
	}{
		// Bare-program reads — should short-circuit.
		{"ls", true},
		{"ls -la", true},
		{"cat README.md", true},
		{"grep -r foo .", true},
		{"head -n 5 file.txt", true},
		{"pwd", true},
		{"echo hello", true},
		{"tree pkg/", true},
		{"find . -name '*.go'", true},
		{"diff a.go b.go", true},
		{"sha256sum file.bin", true},
		{"rg pattern .", true},

		// Path-prefixed reads.
		{"/usr/bin/ls -la", true},
		{"/usr/local/bin/grep foo bar.txt", true},

		// Git read-only subcommands.
		{"git status", true},
		{"git log --oneline", true},
		{"git diff HEAD", true},
		{"git show HEAD", true},
		{"git blame foo.go", true},
		{"git -C /path log", true},
		{"git --no-pager status", true},
		{"git ls-files", true},

		// Git writes.
		{"git add foo.go", false},
		{"git commit -m 'msg'", false},
		{"git push", false},
		{"git reset --hard", false},
		{"git checkout main", false},
		{"git stash", false},

		// Find with dangerous ops.
		{"find . -name '*.tmp' -delete", false},
		{"find . -name '*.go' -exec rm {} \\;", false},
		{"find / -okdir mv {} /tmp \\;", false},

		// sed -i is a write.
		{"sed -i 's/foo/bar/' file.txt", false},
		{"sed -i.bak 's/foo/bar/' file.txt", false},
		{"sed --in-place 's/foo/bar/' file.txt", false},
		// sed without -i is read-only.
		{"sed 's/foo/bar/' file.txt", true},

		// awk -i inplace is a write.
		{"awk -i inplace '{print $1}' file.txt", false},
		{"gawk --inplace '{print}' file.txt", false},
		// awk without inplace is read-only.
		{"awk '{print $1}' file.txt", true},

		// Redirects ALWAYS force snapshot (even for read-only base programs).
		{"echo hi > out.txt", false},
		{"cat foo.txt >> bar.txt", false},
		{"ls > listing.txt", false},

		// Pipes force snapshot (the right side could be `xargs rm`).
		{"grep -l foo . | xargs rm", false},
		{"find . -name '*.tmp' | xargs rm", false},
		// Even read|read is conservatively flagged.
		{"cat foo | wc -l", false},

		// Chains force snapshot.
		{"ls && pwd", false},
		{"echo a; echo b", false},
		{"true || rm foo", false},

		// Subshells force snapshot.
		{"echo $(date)", false},
		{"ls `pwd`", false},

		// Background.
		{"sleep 5 &", false},

		// Unknown programs default to NOT read-only (conservative).
		{"unknown_tool", false},
		{"mything --foo", false},

		// Known mutators are never read-only.
		{"rm file.txt", false},
		{"mv a b", false},
		{"cp a b", false},
		{"touch foo", false},
		{"mkdir x", false},

		// Go subcommands.
		{"go version", true},
		{"go vet ./...", true},
		{"go env", true},
		{"go build ./...", false}, // writes binary
		{"go test ./...", false},  // writes cache
		{"go mod tidy", false},    // rewrites go.sum

		// JS pkg managers — read-only subcommands only.
		{"npm ls", true},
		{"npm info react", true},
		{"yarn list", true},
		{"npm install", false},
		{"npm run build", false},

		// Empty / whitespace.
		{"", true},
		{"   ", true},
	}

	for _, c := range cases {
		t.Run(c.cmd, func(t *testing.T) {
			got := shellLooksReadOnly(c.cmd)
			if got != c.readOnly {
				t.Errorf("shellLooksReadOnly(%q) = %v, want %v", c.cmd, got, c.readOnly)
			}
		})
	}
}
