//go:build windows

package agent

import "testing"

func TestResolveShellCdArg_WindowsRootedPaths(t *testing.T) {
	cwd := `C:\work\repo`
	cases := map[string]string{
		"/c/Users/me": `C:\Users\me`,
		"/d":          `D:\`,
		"/etc":        "/etc",
		"sub":         `C:\work\repo\sub`,
		`D:\other`:    `D:\other`,
	}
	for arg, want := range cases {
		if got := resolveShellCdArg(arg, cwd); got != want {
			t.Errorf("resolveShellCdArg(%q) = %q, want %q", arg, got, want)
		}
	}
}
