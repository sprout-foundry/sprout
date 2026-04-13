package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// apiEnvKeyPatterns defines the suffixes and prefixes for API keys and related credentials.
var apiEnvKeyPatterns = []string{
	"_API_KEY",
	"_TOKEN",
	"_SECRET",
	"_ACCESS_KEY",
	"_SECRET_KEY",
}

// apiEnvKeyPrefixes defines specific prefixes to capture.
var apiEnvKeyPrefixes = []string{
	"LEDIT_PROVIDER",
	"LEDIT_SUBAGENT_PROVIDER",
	"LEDIT_SUBAGENT_MODEL",
}

// matchesAPIKeyPattern returns true if the environment variable name appears to be an API key.
func matchesAPIKeyPattern(key string) bool {
	upperKey := strings.ToUpper(key)
	for _, prefix := range apiEnvKeyPrefixes {
		if strings.HasPrefix(upperKey, prefix) {
			return true
		}
	}
	for _, suffix := range apiEnvKeyPatterns {
		if strings.HasSuffix(upperKey, suffix) {
			return true
		}
	}
	return false
}

// captureAPIKeysFromEnv filters the current environment for API keys and related credentials.
// It returns a slice of "KEY=value" strings for matching environment variables.
func captureAPIKeysFromEnv() []string {
	var matches []string
	for _, env := range os.Environ() {
		if idx := strings.IndexByte(env, '='); idx > 0 {
			key := env[:idx]
			if matchesAPIKeyPattern(key) {
				matches = append(matches, env)
			}
		}
	}
	return matches
}

// serviceEnvPath returns the path to the service.env file.
func serviceEnvPath(homeDir string) string {
	return filepath.Join(homeDir, ".ledit", "service.env")
}

// generateServiceEnvFile captures API keys from the current environment and writes them
// to ~/.ledit/service.env with restricted permissions (0600).
func generateServiceEnvFile(homeDir string) error {
	// Ensure the .ledit directory exists
	leditDir := filepath.Join(homeDir, ".ledit")
	if err := os.MkdirAll(leditDir, 0755); err != nil {
		return fmt.Errorf("failed to create .ledit directory: %w", err)
	}

	// Capture API keys from current environment
	envVars := captureAPIKeysFromEnv()
	envPath := serviceEnvPath(homeDir)

	// Write to a random temp file first, then rename for atomicity.
	tmpFile, err := os.CreateTemp(leditDir, ".service.env.*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if len(envVars) == 0 {
		fmt.Println("No API key environment variables found in current environment.")
		fmt.Println("If you need to set API keys, export them before running 'ledit service install'.")
		// Create an empty file anyway (systemd tolerates this with the - prefix).
		if err := tmpFile.Close(); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to close service.env: %w", err)
		}
		if err := os.Rename(tmpPath, envPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to write service.env: %w", err)
		}
		return nil
	}

	for _, env := range envVars {
		if _, err := tmpFile.WriteString(env + "\n"); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("failed to write to service.env: %w", err)
		}
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close service.env: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, envPath); err != nil {
		return fmt.Errorf("failed to rename service.env: %w", err)
	}

	// Log what was captured
	fmt.Printf("Captured %d API key(s) from environment to %s\n", len(envVars), envPath)

	// List captured keys (without values for security)
	var capturedKeys []string
	for _, env := range envVars {
		if idx := strings.IndexByte(env, '='); idx > 0 {
			capturedKeys = append(capturedKeys, env[:idx])
		}
	}
	for i, key := range capturedKeys {
		if i > 0 && i%4 == 0 {
			fmt.Println()
		}
		fmt.Printf("  %s", key)
		if i < len(capturedKeys)-1 {
			fmt.Print(",")
		}
	}
	if len(capturedKeys) > 0 {
		fmt.Println()
	}

	return nil
}

// loadServiceEnvFile reads ~/.ledit/service.env and returns a map of key-value pairs.
// If the file doesn't exist or is empty, returns an empty map.
func loadServiceEnvFile(homeDir string) (map[string]string, error) {
	envPath := serviceEnvPath(homeDir)

	file, err := os.Open(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return empty map
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("failed to open service.env: %w", err)
	}
	defer file.Close()

	envMap := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			key := line[:idx]
			value := line[idx+1:]
			envMap[key] = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read service.env: %w", err)
	}

	return envMap, nil
}
