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
	tmpFile := filepath.Join(t.TempDir(), "test.json")
	err := os.WriteFile(tmpFile, []byte("{}"), 0644)
	require.NoError(t, err)

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
	tmpDir := t.TempDir()
	err := os.Chmod(tmpDir, 0700)
	require.NoError(t, err)

	warnings := RunStartupCheck(tmpDir)
	assert.False(t, warnings)
}

func TestRunStartupCheck_Warnings(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.Chmod(tmpDir, 0755)
	require.NoError(t, err)

	warnings := RunStartupCheck(tmpDir)
	assert.True(t, warnings)
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
	tmpFile := filepath.Join(t.TempDir(), "test.json")
	err := os.WriteFile(tmpFile, []byte("{}"), 0600)
	require.NoError(t, err)

	mode, err := GetFileMode(tmpFile)
	assert.NoError(t, err)
	assert.Equal(t, "600", mode)
}

func TestGetFileMode_Nonexistent(t *testing.T) {
	_, err := GetFileMode("/nonexistent")
	assert.Error(t, err)
}

func TestGetDirMode(t *testing.T) {
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
	err = os.Symlink(target, link)
	require.NoError(t, err)

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
	err = os.Symlink(target, link)
	require.NoError(t, err)

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
