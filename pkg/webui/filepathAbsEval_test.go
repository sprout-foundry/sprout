package webui

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// TestFilepathAbsEval_HomeEnvVar verifies that filepathAbsEval expands $HOME
// to the actual OS home directory.
func TestFilepathAbsEval_HomeEnvVar(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() failed: %v", err)
	}

	input := "$HOME"
	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}

	// The home dir always exists, so EvalSymlinks should resolve symlinks.
	absHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error: %v", err)
	}

	if got != absHome {
		t.Errorf("filepathAbsEval(%q) = %q, want %q (resolved home)", input, got, absHome)
	}
}

// TestFilepathAbsEval_Tilde verifies that filepathAbsEval expands ~ to the
// actual OS home directory.
func TestFilepathAbsEval_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() failed: %v", err)
	}

	input := "~"
	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}

	absHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error: %v", err)
	}

	if got != absHome {
		t.Errorf("filepathAbsEval(%q) = %q, want %q (resolved home)", input, got, absHome)
	}
}

// TestFilepathAbsEval_HomeSubPath verifies that filepathAbsEval expands
// $HOME/sub/path to the home directory joined with sub/path.
func TestFilepathAbsEval_HomeSubPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() failed: %v", err)
	}

	// Create a subdirectory under home so EvalSymlinks can resolve it.
	subDir := ".ledit_filepathAbsEval_test_sub"
	fullPath := filepath.Join(home, subDir)
	if err := os.MkdirAll(fullPath, 0o755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	defer os.RemoveAll(fullPath)

	input := "$HOME/" + subDir
	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}

	absHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error: %v", err)
	}
	want := filepath.Join(absHome, subDir)

	if got != want {
		t.Errorf("filepathAbsEval(%q) = %q, want %q", input, got, want)
	}
}

// TestFilepathAbsEval_TildeSubPath verifies that filepathAbsEval expands
// ~/sub/path to the home directory joined with sub/path.
func TestFilepathAbsEval_TildeSubPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() failed: %v", err)
	}

	// Create a subdirectory under home so EvalSymlinks can resolve it.
	subDir := ".ledit_filepathAbsEval_test_tsub"
	fullPath := filepath.Join(home, subDir)
	if err := os.MkdirAll(fullPath, 0o755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	defer os.RemoveAll(fullPath)

	input := "~/" + subDir
	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}

	absHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error: %v", err)
	}
	want := filepath.Join(absHome, subDir)

	if got != want {
		t.Errorf("filepathAbsEval(%q) = %q, want %q", input, got, want)
	}
}

// TestFilepathAbsEval_RelativePath verifies that a plain relative path is
// resolved to an absolute path (via filepath.Abs + EvalSymlinks).
func TestFilepathAbsEval_RelativePath(t *testing.T) {
	got, err := filepathAbsEval(".")
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", ".", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error: %v", err)
	}
	absCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(cwd) error: %v", err)
	}

	if got != absCwd {
		t.Errorf("filepathAbsEval(%q) = %q, want %q", ".", got, absCwd)
	}
}

// TestFilepathAbsEval_AlreadyAbsolute verifies that an already-absolute path
// is resolved through symlink evaluation.
func TestFilepathAbsEval_AlreadyAbsolute(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() failed: %v", err)
	}

	input, err := filepath.Abs(home)
	if err != nil {
		t.Fatalf("filepath.Abs(home) error: %v", err)
	}

	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}

	absHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error: %v", err)
	}

	if got != absHome {
		t.Errorf("filepathAbsEval(%q) = %q, want %q", input, got, absHome)
	}
}

// TestFilepathAbsEval_NonExistentPath verifies that a non-existent path
// falls back to the unresolved absolute path instead of returning an error.
func TestFilepathAbsEval_NonExistentPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() failed: %v", err)
	}

	nonExistent := filepath.Join(home, ".ledit_test_nonexistent_dir_12345")
	got, err := filepathAbsEval(nonExistent)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", nonExistent, err)
	}

	absWant, err := filepath.Abs(nonExistent)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error: %v", nonExistent, err)
	}

	if got != absWant {
		t.Errorf("filepathAbsEval(%q) = %q, want %q", nonExistent, got, absWant)
	}
}

// TestFilepathAbsEval_BraceHome verifies that filepathAbsEval expands ${HOME}
// (brace syntax) to the actual OS home directory.
func TestFilepathAbsEval_BraceHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() failed: %v", err)
	}
	input := "${HOME}"
	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}
	want, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", home, err)
	}
	if got != want {
		t.Errorf("filepathAbsEval(%q) = %q, want %q", input, got, want)
	}
}

// TestFilepathAbsEval_BraceHomeSubPath verifies that filepathAbsEval expands
// ${HOME}/sub/path to the home directory joined with sub/path.
func TestFilepathAbsEval_BraceHomeSubPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() failed: %v", err)
	}

	// Create a subdirectory under home so EvalSymlinks can resolve it.
	subDir := ".ledit_filepathAbsEval_test_brace_sub"
	fullPath := filepath.Join(home, subDir)
	if err := os.MkdirAll(fullPath, 0o755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	defer os.RemoveAll(fullPath)

	input := "${HOME}/" + subDir
	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}

	absHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error: %v", err)
	}
	want := filepath.Join(absHome, subDir)

	if got != want {
		t.Errorf("filepathAbsEval(%q) = %q, want %q", input, got, want)
	}
}

// TestFilepathAbsEval_OtherEnvVar verifies that environment variables other
// than $HOME are NOT expanded (expandHomeVar is intentionally restricted to
// $HOME / ${HOME} only).
func TestFilepathAbsEval_OtherEnvVar(t *testing.T) {
	t.Setenv("LEDIT_TEST_VAR", "/tmp/ledit_should_not_expand")

	input := "$LEDIT_TEST_VAR"
	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error: %v", err)
	}
	absCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(cwd) error: %v", err)
	}
	want := filepath.Join(absCwd, input)

	if got != want {
		t.Errorf("filepathAbsEval(%q) = %q, want %q (non-HOME env var should not be expanded)", input, got, want)
	}
}

// TestFilepathAbsEval_OtherEnvVarSubPath verifies that a custom env var with a
// sub-path is NOT expanded since expandHomeVar only handles $HOME.
func TestFilepathAbsEval_OtherEnvVarSubPath(t *testing.T) {
	t.Setenv("LEDIT_TMPDIR", "/tmp/ledit_should_not_expand")

	subDir := "nested"
	input := "$LEDIT_TMPDIR/" + subDir
	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error: %v", err)
	}
	absCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(cwd) error: %v", err)
	}
	want := filepath.Join(absCwd, input)

	if got != want {
		t.Errorf("filepathAbsEval(%q) = %q, want %q (non-HOME env var should not be expanded)", input, got, want)
	}
}

// TestFilepathAbsEval_UsersTilde verifies that ~username is NOT expanded
// (only bare ~ and ~/ should expand). The function should treat ~username
// as a relative directory name.
func TestFilepathAbsEval_UsersTilde(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current() failed: %v", err)
	}

	// ~username should NOT be expanded — os.ExpandEnv doesn't expand tildes,
	// and our tilde expansion only triggers on bare "~" or "~/..." prefixes.
	input := "~" + currentUser.Username
	got, err := filepathAbsEval(input)
	if err != nil {
		t.Fatalf("filepathAbsEval(%q) error: %v", input, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error: %v", err)
	}
	want := filepath.Join(cwd, input)

	if got != want {
		t.Errorf("filepathAbsEval(%q) = %q, want %q", input, got, want)
	}
}
