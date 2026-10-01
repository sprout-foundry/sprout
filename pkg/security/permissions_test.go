package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPermissionChecker(t *testing.T) {
	pc := NewPermissionChecker("/tmp/test-dir")
	assert.NotNil(t, pc)
	assert.Equal(t, "/tmp/test-dir", pc.configDir)
}

func TestCheckConfigDirPermissions_Secure(t *testing.T) {
	tmpDir := t.TempDir()
	// Set secure permissions
	err := os.Chmod(tmpDir, 0700)
	require.NoError(t, err)

	pc := NewPermissionChecker(tmpDir)
	warning := pc.CheckConfigDirPermissions()
	assert.Empty(t, warning, "secure dir should not produce warning")
}

func TestCheckConfigDirPermissions_Insecure(t *testing.T) {
	skipWithoutPOSIXModes(t)
	tmpDir := t.TempDir()
	err := os.Chmod(tmpDir, 0755)
	require.NoError(t, err)

	pc := NewPermissionChecker(tmpDir)
	warning := pc.CheckConfigDirPermissions()
	assert.NotEmpty(t, warning, "insecure dir should produce warning")
	assert.Contains(t, warning, "insecure permissions")
}

func TestCheckConfigDirPermissions_Nonexistent(t *testing.T) {
	pc := NewPermissionChecker("/nonexistent/path")
	warning := pc.CheckConfigDirPermissions()
	assert.Empty(t, warning, "nonexistent dir should not produce warning")
}

func TestCheckFilePermissions_Secure(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.json")
	err := os.WriteFile(tmpFile, []byte("{}"), 0600)
	require.NoError(t, err)

	pc := NewPermissionChecker(t.TempDir())
	warning := pc.CheckFilePermissions(tmpFile)
	assert.Empty(t, warning)
}

func TestCheckFilePermissions_Insecure(t *testing.T) {
	skipWithoutPOSIXModes(t)
	tmpFile := filepath.Join(t.TempDir(), "test.json")
	err := os.WriteFile(tmpFile, []byte("{}"), 0644)
	require.NoError(t, err)

	// Verify chmod actually took effect — on some platforms (e.g. Android/Termux
	// with restrictive umask) the file may still end up with secure permissions
	// despite requesting 0644, making this test non-deterministic.
	info, err := os.Stat(tmpFile)
	require.NoError(t, err)
	if info.Mode().Perm() == 0600 {
		t.Skip("skipping: umask already enforces secure permissions, chmod had no visible effect")
	}

	pc := NewPermissionChecker(t.TempDir())
	warning := pc.CheckFilePermissions(tmpFile)
	assert.NotEmpty(t, warning)
	assert.Contains(t, warning, "insecure permissions")
}

func TestCheckFilePermissions_Nonexistent(t *testing.T) {
	pc := NewPermissionChecker(t.TempDir())
	warning := pc.CheckFilePermissions("/nonexistent/file.json")
	assert.Empty(t, warning)
}

func TestCheckAllSecurityFiles(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.Chmod(tmpDir, 0700)
	require.NoError(t, err)

	// Create files with secure perms
	for _, name := range []string{"config.json", "api_keys.json"} {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte("{}"), 0600)
		require.NoError(t, err)
	}

	pc := NewPermissionChecker(tmpDir)
	warnings := pc.CheckAllSecurityFiles()
	assert.Empty(t, warnings)
}

func TestCheckAllSecurityFiles_InsecureFiles(t *testing.T) {
	skipWithoutPOSIXModes(t)
	tmpDir := t.TempDir()
	err := os.Chmod(tmpDir, 0755) // insecure
	require.NoError(t, err)

	// Create files with insecure perms
	err = os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte("{}"), 0644)
	require.NoError(t, err)

	pc := NewPermissionChecker(tmpDir)
	warnings := pc.CheckAllSecurityFiles()
	assert.NotEmpty(t, warnings)
}

func TestFixPermissions(t *testing.T) {
	skipWithoutPOSIXModes(t)
	tmpDir := t.TempDir()

	// Create dir and files with insecure perms
	err := os.Chmod(tmpDir, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte("{}"), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "api_keys.json"), []byte("{}"), 0644)
	require.NoError(t, err)

	pc := NewPermissionChecker(tmpDir)
	errors := pc.FixPermissions()
	assert.Empty(t, errors, "fix should succeed")

	// Verify permissions are now correct
	info, err := os.Stat(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm())
}

func TestRunStartupCheck_Clean(t *testing.T) {
	resetStartupCheck()
	tmpDir := t.TempDir()
	err := os.Chmod(tmpDir, 0700)
	require.NoError(t, err)

	warnings := RunStartupCheck(tmpDir)
	assert.False(t, warnings)
}

func TestRunStartupCheck_AutoFix(t *testing.T) {
	skipWithoutPOSIXModes(t)
	resetStartupCheck()
	tmpDir := t.TempDir()
	err := os.Chmod(tmpDir, 0755)
	require.NoError(t, err)

	// Write a file with insecure perms so FixPermissions has something to fix.
	err = os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte("{}"), 0644)
	require.NoError(t, err)

	// RunStartupCheck fixes then checks; if fix succeeds, no warning is emitted.
	warnings := RunStartupCheck(tmpDir)
	assert.False(t, warnings, "auto-fix should suppress warnings when it succeeds")

	// Verify the directory was actually tightened.
	info, err := os.Stat(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm(), "dir should be tightened to 0700")
}

func TestRunStartupCheck_Dedup(t *testing.T) {
	resetStartupCheck()
	tmpDir := t.TempDir()
	err := os.Chmod(tmpDir, 0700)
	require.NoError(t, err)

	// First call runs the check.
	result1 := RunStartupCheck(tmpDir)
	// Second call should be a no-op via sync.Once and return the same result.
	result2 := RunStartupCheck(tmpDir)
	assert.Equal(t, result1, result2, "deduped call should return cached result")
}

func TestGetPermissionError(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.json")
	err := os.WriteFile(tmpFile, []byte("{}"), 0644)
	require.NoError(t, err)

	permErr := GetPermissionError(tmpFile, 0600)
	assert.Error(t, permErr)
	assert.Contains(t, permErr.Error(), "insecure permissions")
}

func TestGetPermissionError_Nonexistent(t *testing.T) {
	permErr := GetPermissionError("/nonexistent/file", 0600)
	assert.Error(t, permErr)
}

func TestIsWorldReadable(t *testing.T) {
	skipWithoutPOSIXModes(t)
	tmpFile := filepath.Join(t.TempDir(), "test.json")

	err := os.WriteFile(tmpFile, []byte("{}"), 0600)
	require.NoError(t, err)
	readable, err := IsWorldReadable(tmpFile)
	assert.NoError(t, err)
	assert.False(t, readable)

	err = os.Chmod(tmpFile, 0644)
	require.NoError(t, err)
	readable, err = IsWorldReadable(tmpFile)
	assert.NoError(t, err)
	assert.True(t, readable)
}

func TestIsGroupReadable(t *testing.T) {
	skipWithoutPOSIXModes(t)
	tmpFile := filepath.Join(t.TempDir(), "test.json")

	err := os.WriteFile(tmpFile, []byte("{}"), 0600)
	require.NoError(t, err)
	readable, err := IsGroupReadable(tmpFile)
	assert.NoError(t, err)
	assert.False(t, readable)

	err = os.Chmod(tmpFile, 0640)
	require.NoError(t, err)
	readable, err = IsGroupReadable(tmpFile)
	assert.NoError(t, err)
	assert.True(t, readable)
}

func TestGetFileMode(t *testing.T) {
	skipWithoutPOSIXModes(t)
	tmpFile := filepath.Join(t.TempDir(), "test.json")
	err := os.WriteFile(tmpFile, []byte("{}"), 0600)
	require.NoError(t, err)

	mode, err := GetFileMode(tmpFile)
	assert.NoError(t, err)
	assert.Equal(t, "600", mode)
}

func TestGetFileMode_Nonexistent(t *testing.T) {
	_, err := GetFileMode(filepath.Join(t.TempDir(), "nonexistent"))
	assert.Error(t, err)
}

func TestGetDirMode(t *testing.T) {
	skipWithoutPOSIXModes(t)
	tmpDir := t.TempDir()
	err := os.Chmod(tmpDir, 0700)
	require.NoError(t, err)

	mode, err := GetDirMode(tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, "700", mode)
}

func TestCheckSymlinkSafety_NoSymlink(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.json")
	err := os.WriteFile(tmpFile, []byte("{}"), 0600)
	require.NoError(t, err)

	warning := CheckSymlinkSafety(tmpFile, t.TempDir())
	assert.Empty(t, warning)
}

func TestCheckSymlinkSafety_SymlinkWithinConfig(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "real_file")
	err := os.WriteFile(target, []byte("data"), 0600)
	require.NoError(t, err)

	link := filepath.Join(tmpDir, "link")
	symlinkOrSkip(t, target, link)

	warning := CheckSymlinkSafety(link, tmpDir)
	assert.Empty(t, warning, "symlink within config dir should be safe")
}

func TestCheckSymlinkSafety_SymlinkOutsideConfig(t *testing.T) {
	tmpDir := t.TempDir()
	outsideDir := t.TempDir()

	// Create target outside config dir
	target := filepath.Join(outsideDir, "outside_file")
	err := os.WriteFile(target, []byte("data"), 0600)
	require.NoError(t, err)

	link := filepath.Join(tmpDir, "link")
	symlinkOrSkip(t, target, link)

	warning := CheckSymlinkSafety(link, tmpDir)
	assert.NotEmpty(t, warning, "symlink outside config dir should warn")
	assert.Contains(t, warning, "symlink")
}

func TestCheckAllSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a regular file
	err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte("{}"), 0600)
	require.NoError(t, err)

	warnings := CheckAllSymlinks(tmpDir)
	assert.Empty(t, warnings, "regular file should not trigger symlink warning")
}

func TestCheckSymlinkSafety_RelativeSymlinkEscapingConfig(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.Mkdir(configDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "outside_file"), []byte("data"), 0o600))

	link := filepath.Join(configDir, "link")
	symlinkOrSkip(t, filepath.Join("..", "outside_file"), link)

	assert.NotEmpty(t, CheckSymlinkSafety(link, configDir), "relative symlink escaping config dir should warn")
}

func TestCheckSymlinkSafety_SiblingWithConfigDirPrefix(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	sibling := filepath.Join(root, "config-evil")
	require.NoError(t, os.Mkdir(configDir, 0o700))
	require.NoError(t, os.Mkdir(sibling, 0o700))
	target := filepath.Join(sibling, "file")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o600))

	link := filepath.Join(configDir, "link")
	symlinkOrSkip(t, target, link)

	assert.NotEmpty(t, CheckSymlinkSafety(link, configDir), "a sibling dir sharing the config dir's name prefix is outside it")
}

func TestPermissionChecksSilentWithoutPOSIXModes(t *testing.T) {
	if posixModes {
		t.Skip("mode bits are meaningful on this platform")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "api_keys.json"), []byte("{}"), 0o644))
	pc := NewPermissionChecker(dir)
	assert.Empty(t, pc.CheckAllSecurityFiles(), "synthesized Windows mode bits must not produce warnings")
	assert.Empty(t, pc.FixPermissions())
}

// skipWithoutPOSIXModes skips tests asserting on mode bits, which Windows
// synthesizes from the read-only attribute instead of storing.
func skipWithoutPOSIXModes(t *testing.T) {
	t.Helper()
	if !posixModes {
		t.Skip("file mode bits do not describe access on Windows")
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported (Windows needs Developer Mode or admin): %v", err)
	}
}
