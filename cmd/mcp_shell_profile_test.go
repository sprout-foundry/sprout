//go:build !js

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// detectShellProfilePath tests
// =============================================================================

func TestDetectShellProfilePath_Bash(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/bash")

	// .bashrc doesn't exist yet, but bash path detection returns ~/.bashrc
	// regardless of existence (it falls back to .bash_profile only if
	// .bashrc doesn't exist and .bash_profile does).
	// Create .bashrc so it gets picked.
	bashrc := filepath.Join(tmp, ".bashrc")
	if err := os.WriteFile(bashrc, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	got := detectShellProfilePath()
	if got != bashrc {
		t.Errorf("detectShellProfilePath() = %q; want %q", got, bashrc)
	}
}

func TestDetectShellProfilePath_BashFallbackToBashProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/bash")

	// .bashrc missing, .bash_profile exists → should use .bash_profile
	bashProfile := filepath.Join(tmp, ".bash_profile")
	if err := os.WriteFile(bashProfile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	got := detectShellProfilePath()
	if got != bashProfile {
		t.Errorf("detectShellProfilePath() = %q; want %q", got, bashProfile)
	}
}

func TestDetectShellProfilePath_BashNoExistingFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/bash")

	// Neither .bashrc nor .bash_profile exist.
	got := detectShellProfilePath()
	// Falls back to .bashrc (the canonical bash path, even if it doesn't exist
	// yet — the caller will create it on write).
	want := filepath.Join(tmp, ".bashrc")
	if got != want {
		t.Errorf("detectShellProfilePath() = %q; want %q", got, want)
	}
}

func TestDetectShellProfilePath_Zsh(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/zsh")

	want := filepath.Join(tmp, ".zshrc")
	got := detectShellProfilePath()
	if got != want {
		t.Errorf("detectShellProfilePath() = %q; want %q", got, want)
	}
}

func TestDetectShellProfilePath_Fish(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/usr/bin/fish")

	want := filepath.Join(tmp, ".config", "fish", "config.fish")
	got := detectShellProfilePath()
	if got != want {
		t.Errorf("detectShellProfilePath() = %q; want %q", got, want)
	}
}

func TestDetectShellProfilePath_FallbackToProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/usr/bin/ksh")

	want := filepath.Join(tmp, ".profile")
	got := detectShellProfilePath()
	if got != want {
		t.Errorf("detectShellProfilePath() = %q; want %q", got, want)
	}
}

func TestDetectShellProfilePath_MissingHome(t *testing.T) {
	t.Setenv("HOME", "")
	got := detectShellProfilePath()
	if got != "" {
		t.Errorf("detectShellProfilePath() with empty HOME = %q; want empty", got)
	}
}

// =============================================================================
// writeEnvToShellProfile tests
// =============================================================================

func TestWriteEnvToShellProfile_CreatesNewFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	if err := writeEnvToShellProfile(path, "FOO", "bar"); err != nil {
		t.Fatalf("writeEnvToShellProfile() error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error: %v", err)
	}

	body := string(content)
	if !strings.Contains(body, `export FOO="bar"`) {
		t.Errorf("expected export FOO=\"bar\" in file, got:\n%s", body)
	}
	if !strings.Contains(body, "# sprout-managed: FOO") {
		t.Errorf("expected sprout-managed marker in file, got:\n%s", body)
	}
}

func TestWriteEnvToShellProfile_PreservesBareLine(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	// Seed file with a bare assignment — possibly intentional (e.g.
	// secret-manager command substitution). The function must NOT
	// clobber it; instead, append a managed block with the new value.
	existing := `export GITHUB_PERSONAL_ACCESS_TOKEN="$(op read op://GitHub/PAT)"
`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeEnvToShellProfile(path, "GITHUB_PERSONAL_ACCESS_TOKEN", "new_value"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)

	if !strings.Contains(body, `export GITHUB_PERSONAL_ACCESS_TOKEN="$(op read op://GitHub/PAT)"`) {
		t.Errorf("expected existing intentional line to be preserved, got:\n%s", body)
	}
	if !strings.Contains(body, `export GITHUB_PERSONAL_ACCESS_TOKEN="new_value"`) {
		t.Errorf("expected new managed-block export to be appended, got:\n%s", body)
	}
}

func TestWriteEnvToShellProfile_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	// Write the same value twice.
	if err := writeEnvToShellProfile(path, "FOO", "bar"); err != nil {
		t.Fatal(err)
	}
	if err := writeEnvToShellProfile(path, "FOO", "bar"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)

	count := strings.Count(body, "export FOO=")
	if count != 1 {
		t.Errorf("expected exactly 1 export FOO= line, got %d; file:\n%s", count, body)
	}
}

func TestWriteEnvToShellProfile_UpdatesSproutManagedBlock(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	// Write initial value via the managed-block path.
	if err := writeEnvToShellProfile(path, "FOO", "v1"); err != nil {
		t.Fatal(err)
	}

	// Update to new value — must replace the block in-place, not append.
	if err := writeEnvToShellProfile(path, "FOO", "v2"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)

	if strings.Contains(body, "v1") {
		t.Errorf("expected v1 to be replaced, got:\n%s", body)
	}
	if !strings.Contains(body, "v2") {
		t.Errorf("expected v2 in file, got:\n%s", body)
	}

	// Verify markers are intact (start + end exactly once each).
	if strings.Count(body, "# sprout-managed: FOO") != 1 {
		t.Errorf("expected exactly 1 start marker, got %d", strings.Count(body, "# sprout-managed: FOO"))
	}
	if strings.Count(body, "# sprout-managed: end") != 1 {
		t.Errorf("expected exactly 1 end marker, got %d", strings.Count(body, "# sprout-managed: end"))
	}
}

func TestWriteEnvToShellProfile_RejectsBadValue(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	// Write with a double-quote in the value.
	err := writeEnvToShellProfile(path, "FOO", `bar"baz`)
	if err == nil {
		t.Fatal("expected error for value with quote, got nil")
	}

	// File should not have been created.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to not exist after rejection, got: %v", err)
	}
}

func TestWriteEnvToShellProfile_RejectsNewline(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	err := writeEnvToShellProfile(path, "FOO", "bar\nbaz")
	if err == nil {
		t.Fatal("expected error for value with newline, got nil")
	}
}

func TestWriteEnvToShellProfile_PreservesOtherLines(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	// Seed with multiple lines.
	existing := `# My bashrc
export PATH=$PATH:/usr/local/bin
alias ls='ls -la'
`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeEnvToShellProfile(path, "FOO", "bar"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)

	if !strings.Contains(body, "# My bashrc") {
		t.Error("expected original comment to be preserved")
	}
	if !strings.Contains(body, `export PATH=$PATH:/usr/local/bin`) {
		t.Error("expected PATH export to be preserved")
	}
	if !strings.Contains(body, `alias ls='ls -la'`) {
		t.Error("expected alias to be preserved")
	}
	if !strings.Contains(body, `export FOO="bar"`) {
		t.Error("expected new export to be appended")
	}
}

func TestWriteEnvToShellProfile_MultipleEnvVars(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	if err := writeEnvToShellProfile(path, "FOO", "bar"); err != nil {
		t.Fatal(err)
	}
	if err := writeEnvToShellProfile(path, "BAZ", "qux"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)

	if !strings.Contains(body, `export FOO="bar"`) {
		t.Error("expected FOO export")
	}
	if !strings.Contains(body, `export BAZ="qux"`) {
		t.Error("expected BAZ export")
	}
	if strings.Count(body, "export FOO=") != 1 || strings.Count(body, "export BAZ=") != 1 {
		t.Errorf("expected exactly 1 of each export, got:\n%s", body)
	}
}

func TestWriteEnvToShellProfile_UpdatesBareAssignment(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	// Seed with a bare assignment (no export keyword) — possibly
	// intentional. The function must NOT clobber it; instead, append a
	// managed block with the new value.
	if err := os.WriteFile(path, []byte("FOO=old_value\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeEnvToShellProfile(path, "FOO", "new_value"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)

	if !strings.Contains(body, "FOO=old_value") {
		t.Errorf("expected bare assignment to be preserved, got:\n%s", body)
	}
	if !strings.Contains(body, `export FOO="new_value"`) {
		t.Errorf("expected new managed-block export to be appended, got:\n%s", body)
	}
}

func TestWriteEnvToShellProfile_Atomictemp(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	if err := writeEnvToShellProfile(path, "FOO", "bar"); err != nil {
		t.Fatal(err)
	}

	// Verify no temp file was left behind.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file %s still exists after successful write", tmpPath)
	}
}

// =============================================================================
// validateShellValue unit tests
// =============================================================================

func TestWriteEnvToShellProfile_RejectsBangChar(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".bashrc")

	// `!` triggers zsh history expansion inside double quotes.
	err := writeEnvToShellProfile(path, "FOO", "bar!baz")
	if err == nil {
		t.Fatal("expected error for value containing `!`, got nil")
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to not exist after rejection, got: %v", err)
	}
}

func TestValidateShellValue_RejectsBang(t *testing.T) {
	if err := validateShellValue("foo!bar"); err == nil {
		t.Error("validateShellValue(\"foo!bar\") = nil; want error")
	}
}

// =============================================================================
// validateShellValue unit tests
// =============================================================================

func TestValidateShellValue(t *testing.T) {
	cases := []struct {
		value  string
		expect bool // true = should pass
	}{
		{"ghp_abc123", true},
		{"ghp_abc-def_123", true},
		{"simple_token", true},
		{"foo\"bar", false},
		{"foo\nbar", false},
		{"foo\rbar", false},
		{"foo\\bar", false},
		{"foo$bar", false},
		{"foo`bar", false},
		{"foo!bar", false},
	}

	for _, tc := range cases {
		err := validateShellValue(tc.value)
		passed := err == nil
		if passed != tc.expect {
			if tc.expect {
				t.Errorf("validateShellValue(%q) = %v; want nil", tc.value, err)
			} else {
				t.Errorf("validateShellValue(%q) = nil; want error", tc.value)
			}
		}
	}
}
