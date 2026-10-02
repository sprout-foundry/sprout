//go:build darwin && arm64 && cgo

package localmodel

import "testing"

func TestShouldReexec(t *testing.T) {
	lib := RuntimeLibPath()
	cases := []struct {
		name              string
		loaded, installed bool
		guard, mlxCLib    string
		want              bool
	}{
		{"runtime installed, MLX missing", false, true, "", "", true},
		{"MLX already loaded", true, true, "", "", false},
		{"no runtime", false, false, "", "", false},
		{"already restarted", false, true, "1", "", false},
		{"already pointed at runtime", false, true, "", lib, false},
		{"user MLX_C_LIB failed to load", false, true, "", "/elsewhere/libmlxc.dylib", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldReexec(c.loaded, c.installed, c.guard, c.mlxCLib); got != c.want {
				t.Fatalf("shouldReexec = %v, want %v", got, c.want)
			}
		})
	}
}
