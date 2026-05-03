package tools

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// getVenvPythonPath
// ---------------------------------------------------------------------------

func TestGetVenvPythonPath_CurrentGOOS(t *testing.T) {
	tests := []struct {
		name     string
		venvDir  string
		wantPath string
	}{
		{
			name:     "unix root",
			venvDir:  "/home/user/config/pdf_venv",
			wantPath: "/home/user/config/pdf_venv/bin/python",
		},
		{
			name:     "unix relative",
			venvDir:  "my_venv",
			wantPath: "my_venv/bin/python",
		},
		{
			name:     "unix nested",
			venvDir:  "/a/b/c/venv",
			wantPath: "/a/b/c/venv/bin/python",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getVenvPythonPath(tt.venvDir)
			// Normalize for cross-platform comparison
			got = filepath.ToSlash(got)
			want := filepath.ToSlash(tt.wantPath)
			if runtime.GOOS == "windows" {
				// On Windows we expect Scripts/python.exe
				wantWin := strings.Replace(want, "/bin/python", "/Scripts/python.exe", 1)
				if got != wantWin {
					t.Errorf("getVenvPythonPath(%q) = %q, want %q", tt.venvDir, got, wantWin)
				}
			} else {
				if got != want {
					t.Errorf("getVenvPythonPath(%q) = %q, want %q", tt.venvDir, got, want)
				}
			}
		})
	}
}

func TestGetVenvPythonPath_WindowsPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		got := getVenvPythonPath(`C:\Users\me\config\pdf_venv`)
		if !strings.HasSuffix(got, "Scripts\\python.exe") {
			t.Errorf("expected Windows path ending with 'Scripts\\python.exe', got %q", got)
		}
	}
}

func TestGetVenvPythonPath_UnixPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		got := getVenvPythonPath("/home/user/.config/sprout/pdf_venv")
		expected := "/home/user/.config/sprout/pdf_venv/bin/python"
		if got != expected {
			t.Errorf("getVenvPythonPath() = %q, want %q", got, expected)
		}
	}
}

func TestGetVenvPythonPath_EmptyDir(t *testing.T) {
	got := getVenvPythonPath("")
	if runtime.GOOS == "windows" {
		if got != "Scripts\\python.exe" && got != "Scripts/python.exe" {
			t.Errorf("getVenvPythonPath(\"\") = %q, expected Scripts/python.exe on Windows", got)
		}
	} else {
		expected := "bin/python"
		if got != expected {
			t.Errorf("getVenvPythonPath(\"\") = %q, want %q", got, expected)
		}
	}
}

// ---------------------------------------------------------------------------
// pdfVenvDirName constant
// ---------------------------------------------------------------------------

func TestPDFVenvDirName(t *testing.T) {
	// Verify the constant is set to the expected value
	if pdfVenvDirName != "pdf_venv" {
		t.Errorf("pdfVenvDirName = %q, want \"pdf_venv\"", pdfVenvDirName)
	}
}

// ---------------------------------------------------------------------------
// pdfPythonMinMinor constant
// ---------------------------------------------------------------------------

func TestPDFPythonMinMinor(t *testing.T) {
	if pdfPythonMinMinor != 10 {
		t.Errorf("pdfPythonMinMinor = %d, want 10", pdfPythonMinMinor)
	}
}
