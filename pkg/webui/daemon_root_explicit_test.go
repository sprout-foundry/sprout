//go:build !js

package webui

import (
	"path/filepath"
	"testing"
)

func TestIsExplicitDaemonRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	cases := []struct {
		name   string
		root   string
		source string
		want   bool
	}{
		{"root chosen with --root", root, "SPROUT_DAEMON_ROOT", true},
		{"default root equals HOME", home, "SPROUT_DAEMON_ROOT", false},
		{"missing root", filepath.Join(root, "gone"), "SPROUT_DAEMON_ROOT", false},
		{"root derived from HOME", root, "$HOME", false},
		{"empty", "", "SPROUT_DAEMON_ROOT", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isExplicitDaemonRoot(tc.root, tc.source); got != tc.want {
				t.Errorf("isExplicitDaemonRoot(%q, %q) = %v, want %v", tc.root, tc.source, got, tc.want)
			}
		})
	}
}
