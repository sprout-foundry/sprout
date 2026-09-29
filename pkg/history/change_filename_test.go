package history

import (
	"runtime"
	"strings"
	"testing"
)

func TestSafeChangeFilename(t *testing.T) {
	if got := SafeChangeFilename(`/ws/pkg\a.go`); got != "_ws_pkg_a.go" {
		t.Fatalf("SafeChangeFilename separators = %q", got)
	}
	got := SafeChangeFilename(`C:\Users\dev\a|b?.go`)
	if runtime.GOOS == "windows" && strings.ContainsAny(got, `:*?"<>|`) {
		t.Fatalf("SafeChangeFilename left a reserved Windows character in %q", got)
	}
}
