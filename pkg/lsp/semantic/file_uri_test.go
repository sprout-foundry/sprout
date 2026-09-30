package semantic

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileURI(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got, want := fileURI(`C:\src\my pkg\main.go`), "file:///C:/src/my%20pkg/main.go"; got != want {
			t.Errorf("fileURI = %q, want %q", got, want)
		}
		return
	}
	if got, want := fileURI("/src/my pkg/main.go"), "file:///src/my%20pkg/main.go"; got != want {
		t.Errorf("fileURI = %q, want %q", got, want)
	}
	abs, _ := filepath.Abs("main.go")
	if got, want := fileURI("main.go"), "file://"+filepath.ToSlash(abs); got != want {
		t.Errorf("fileURI(relative) = %q, want %q", got, want)
	}
}
