package credentials

import (
	"fmt"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"os"
	"path/filepath"
	"strings"
)

const (
	configDirName      = ".sprout"
	apiKeysFileName    = "api_keys.json"
	machineKeyFileName = "key.age"
	encryptedMagic     = "age-encryption.org/v1"
)

// Store holds the encrypted API key store.
type Store map[string]string

// Resolved contains a resolved credential with source information.
type Resolved struct {
	Provider string
	EnvVar   string
	Value    string
	Source   string
}

// GetConfigDir returns the configuration directory path, creating it if it doesn't exist.
func GetConfigDir() (string, error) {
	configDir := strings.TrimSpace(envutil.GetEnvSimple("CONFIG"))
	if configDir == "" {
		xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if xdgConfigHome != "" {
			configDir = filepath.Join(xdgConfigHome, "sprout")
		} else {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get home directory: %w", err)
			}
			configDir = filepath.Join(homeDir, configDirName)
		}
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return configDir, nil
}

// GetAPIKeysPath returns the path to the API keys file.
func GetAPIKeysPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return filepath.Join(configDir, apiKeysFileName), nil
}

// GetAPIKeysPathFromDir returns the path to the API keys file in a specific config directory.
func GetAPIKeysPathFromDir(configDir string) (string, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return filepath.Join(configDir, apiKeysFileName), nil
}

// GetAPIKeysLockPath returns the path to the API keys lock file.
func GetAPIKeysLockPath() (string, error) {
	path, err := GetAPIKeysPath()
	if err != nil {
		return "", err
	}
	return path + ".lock", nil
}

// GetAPIKeysLockPathFromDir returns the path to the API keys lock file in a specific config directory.
func GetAPIKeysLockPathFromDir(configDir string) (string, error) {
	path, err := GetAPIKeysPathFromDir(configDir)
	if err != nil {
		return "", err
	}
	return path + ".lock", nil
}

// GetMachineKeyPath returns the path to the machine key file.
func GetMachineKeyPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return filepath.Join(configDir, machineKeyFileName), nil
}

// GetMachineKeyPathFromDir returns the path to the machine key file in a specific config directory.
func GetMachineKeyPathFromDir(configDir string) (string, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return filepath.Join(configDir, machineKeyFileName), nil
}

// encryptionModePath returns the path to the encryption mode file.
// The mode file tracks whether API keys are encrypted with "machine-key" or "passphrase".
func encryptionModePath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "api_keys.mode"), nil
}

// encryptionModePathFromDir returns the path to the encryption mode file in a specific config directory.
func encryptionModePathFromDir(configDir string) (string, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(configDir, "api_keys.mode"), nil
}

// GetEncryptionMode returns the current encryption mode ("machine-key", "passphrase", or "").
// Returns an empty string if no mode file exists (legacy or plaintext files).
func GetEncryptionMode() (string, error) {
	modePath, err := encryptionModePath()
	if err != nil {
		return "", fmt.Errorf("failed to get mode file path: %w", err)
	}

	data, err := os.ReadFile(modePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // No mode file yet
		}
		return "", fmt.Errorf("failed to read mode file: %w", err)
	}

	mode := strings.TrimSpace(string(data))
	if mode == "machine-key" || mode == "passphrase" {
		return mode, nil
	}
	return "", nil
}

// GetEncryptionModeFromDir returns the current encryption mode from a specific config directory.
// Returns an empty string if no mode file exists (legacy or plaintext files).
func GetEncryptionModeFromDir(configDir string) (string, error) {
	modePath, err := encryptionModePathFromDir(configDir)
	if err != nil {
		return "", fmt.Errorf("failed to get mode file path: %w", err)
	}

	data, err := os.ReadFile(modePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // No mode file yet
		}
		return "", fmt.Errorf("failed to read mode file: %w", err)
	}

	mode := strings.TrimSpace(string(data))
	if mode == "machine-key" || mode == "passphrase" {
		return mode, nil
	}
	return "", nil
}

// SetEncryptionMode writes the encryption mode file.
// mode should be "machine-key" or "passphrase".
func SetEncryptionMode(mode string) error {
	if mode != "machine-key" && mode != "passphrase" {
		return fmt.Errorf("invalid encryption mode: %q (must be 'machine-key' or 'passphrase')", mode)
	}
	modePath, err := encryptionModePath()
	if err != nil {
		return fmt.Errorf("failed to get mode file path: %w", err)
	}
	return AtomicWriteFile(modePath, []byte(mode+"\n"), 0600)
}

// SetEncryptionModeForDir writes the encryption mode file in a specific config directory.
// mode should be "machine-key" or "passphrase".
func SetEncryptionModeForDir(configDir, mode string) error {
	if mode != "machine-key" && mode != "passphrase" {
		return fmt.Errorf("invalid encryption mode: %q (must be 'machine-key' or 'passphrase')", mode)
	}
	modePath, err := encryptionModePathFromDir(configDir)
	if err != nil {
		return fmt.Errorf("failed to get mode file path: %w", err)
	}
	return AtomicWriteFile(modePath, []byte(mode+"\n"), 0600)
}

// MaskValue returns a masked version of the credential value for safe logging.
func MaskValue(value string) string {
	if value == "" {
		return ""
	}
	if len(value) >= 8 {
		return value[:4] + "****"
	}
	if len(value) >= 4 {
		return value[:2] + "****"
	}
	return "****"
}

// String returns a safe string representation with the value always masked.
func (r Resolved) String() string {
	return fmt.Sprintf(`Resolved{Provider: %q, EnvVar: %q, Value: %q, Source: %q}`,
		r.Provider, r.EnvVar, MaskValue(r.Value), r.Source)
}

// AtomicWriteFile writes data to a file atomically using temp file + rename pattern.
// This prevents data corruption if the process crashes during the write.
// The file is created with the specified permissions.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".tmp-*.sprout")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := os.Chmod(tmpPath, perm); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to set permissions on temp file: %w", err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to replace file: %w", err)
	}
	return nil
}
