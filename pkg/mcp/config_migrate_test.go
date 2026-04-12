package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupConfigTestEnv creates a temp config dir, sets environment variables,
// and forces the file-based credential backend so tests do not depend on an OS
// keyring being present. Returns the temp directory path (the .ledit subdirectory).
//
// NOTE: getConfigDir() in config.go now respects LEDIT_CONFIG, so we set both
// HOME (for os.UserHomeDir fallback) and LEDIT_CONFIG to ensure MCP config is
// written to the correct location. The credentials package also uses LEDIT_CONFIG.
func setupConfigTestEnv(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".ledit")
	t.Setenv("HOME", homeDir)
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	// Prevent env var overrides from interfering with LoadMCPConfig
	t.Setenv("LEDIT_MCP_ENABLED", "")
	t.Setenv("LEDIT_MCP_AUTO_START", "")
	t.Setenv("LEDIT_MCP_AUTO_DISCOVER", "")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")
	credentials.ResetStorageBackend()
	return configDir
}

// rawMCPConfigFile mirrors just the on-disk JSON structure for reading back
// the saved file (so we can inspect env values without deserialising through
// the custom UnmarshalJSON which would resolve the timeout field).
type rawMCPConfigFile struct {
	Enabled      bool                         `json:"enabled"`
	Servers      map[string]rawServerOnDisk   `json:"servers"`
	AutoStart    bool                         `json:"auto_start"`
	AutoDiscover bool                         `json:"auto_discover"`
	Timeout      interface{}                  `json:"timeout"` // string or number
}

type rawServerOnDisk struct {
	Name        string            `json:"name"`
	Type        string            `json:"type,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Credentials map[string]string `json:"credentials,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Timeout     interface{}      `json:"timeout,omitempty"`
	AutoStart   bool              `json:"auto_start"`
	MaxRestarts int               `json:"max_restarts"`
}

// readRawConfigFile reads and parses the mcp_config.json from the config dir
// (the `.ledit` directory, i.e. what getConfigDir() returns).
func readRawConfigFile(t *testing.T, configDir string) rawMCPConfigFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(configDir, "mcp_config.json"))
	require.NoError(t, err, "should be able to read mcp_config.json from %s", configDir)

	var cfg rawMCPConfigFile
	require.NoError(t, json.Unmarshal(data, &cfg), "should parse mcp_config.json")
	return cfg
}

// ---------------------------------------------------------------------------
// TestSaveMCPConfig_MigratesSecretsBeforePersisting
// ---------------------------------------------------------------------------

func TestSaveMCPConfig_MigratesSecretsBeforePersisting(t *testing.T) {
	dir := setupConfigTestEnv(t)

	// Build a config with plaintext secrets and non-secret env vars
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"testserver": {
				Name:        "testserver",
				Command:     "npx",
				Args:        []string{"-y", "some-mcp-server"},
				AutoStart:   true,
				MaxRestarts: 3,
				Env: map[string]string{
					"OPENAI_API_KEY": "sk-test123", // secret
					"PATH":           "/usr/bin",   // non-secret
				},
			},
		},
	}

	// Act
	err := SaveMCPConfig(&config)
	require.NoError(t, err)

	// Assert: the in-memory config was updated with refs in Credentials, removed from Env
	require.NotNil(t, config.Servers["testserver"].Credentials,
		"credentials map should exist in memory")
	assert.True(t, IsSecretRef(config.Servers["testserver"].Credentials["OPENAI_API_KEY"]),
		"OPENAI_API_KEY should be a credential ref in Credentials after save")
	_, stillInEnv := config.Servers["testserver"].Env["OPENAI_API_KEY"]
	assert.False(t, stillInEnv,
		"OPENAI_API_KEY should be removed from Env after migration")
	assert.Equal(t, "/usr/bin", config.Servers["testserver"].Env["PATH"],
		"PATH should be unchanged in memory")

	// Assert: the on-disk file contains the ref in credentials, not env
	raw := readRawConfigFile(t, dir)
	require.NotNil(t, raw.Servers["testserver"].Credentials,
		"credentials map should exist on disk")
	assert.True(t, IsSecretRef(raw.Servers["testserver"].Credentials["OPENAI_API_KEY"]),
		"OPENAI_API_KEY should be a credential ref in credentials on disk")
	_, secretStillInEnv := raw.Servers["testserver"].Env["OPENAI_API_KEY"]
	assert.False(t, secretStillInEnv,
		"OPENAI_API_KEY should not be in env on disk")
	assert.Equal(t, "/usr/bin", raw.Servers["testserver"].Env["PATH"],
		"PATH should be /usr/bin on disk")

	// Assert: the credential store has the actual secret
	val, _, err := credentials.GetFromActiveBackend("mcp/testserver/OPENAI_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-test123", val, "credential store should contain the plaintext secret")
}

// ---------------------------------------------------------------------------
// TestLoadMCPConfig_AutoMigratesSecrets
// ---------------------------------------------------------------------------

func TestLoadMCPConfig_AutoMigratesSecrets(t *testing.T) {
	dir := setupConfigTestEnv(t)

	// Arrange: write a config file with plaintext secrets to disk
	configDir := dir
	require.NoError(t, os.MkdirAll(configDir, 0755))
	configPath := filepath.Join(configDir, "mcp_config.json")

	originalJSON := `{
		"enabled": true,
		"servers": {
			"myserver": {
				"name": "myserver",
				"command": "npx",
				"args": ["-y", "some-mcp"],
				"auto_start": true,
				"max_restarts": 3,
				"env": {
					"AUTH_TOKEN": "plaintext-secret-token",
					"PATH": "/usr/local/bin"
				}
			}
		}
	}`
	require.NoError(t, os.WriteFile(configPath, []byte(originalJSON), 0600))

	// Act: load the config (which triggers auto-migration)
	loaded, err := LoadMCPConfig()
	require.NoError(t, err)

	// Assert: the returned config has refs in Credentials, not Env
	require.NotNil(t, loaded.Servers["myserver"].Credentials,
		"credentials map should exist after load")
	assert.True(t, IsSecretRef(loaded.Servers["myserver"].Credentials["AUTH_TOKEN"]),
		"AUTH_TOKEN should be a credential ref in Credentials after LoadMCPConfig")
	_, secretInEnv := loaded.Servers["myserver"].Env["AUTH_TOKEN"]
	assert.False(t, secretInEnv,
		"AUTH_TOKEN should be removed from Env after migration")
	assert.Equal(t, "/usr/local/bin", loaded.Servers["myserver"].Env["PATH"],
		"PATH should be unchanged")

	// Assert: credential store was populated
	val, _, err := credentials.GetFromActiveBackend("mcp/myserver/AUTH_TOKEN")
	require.NoError(t, err)
	assert.Equal(t, "plaintext-secret-token", val,
		"credential store should have the original secret value")

	// Assert: the on-disk file was re-saved with refs in credentials
	raw := readRawConfigFile(t, configDir)
	require.NotNil(t, raw.Servers["myserver"].Credentials,
		"credentials map should exist on disk after migration")
	assert.True(t, IsSecretRef(raw.Servers["myserver"].Credentials["AUTH_TOKEN"]),
		"AUTH_TOKEN should be migrated to a ref in credentials on disk")
	_, secretOnDiskEnv := raw.Servers["myserver"].Env["AUTH_TOKEN"]
	assert.False(t, secretOnDiskEnv,
		"AUTH_TOKEN should not be in env on disk after migration")
	assert.Equal(t, "/usr/local/bin", raw.Servers["myserver"].Env["PATH"],
		"PATH should remain /usr/local/bin on disk")
}

// ---------------------------------------------------------------------------
// TestMigrateSecretsOnLoad_Idempotent
// ---------------------------------------------------------------------------

func TestMigrateSecretsOnLoad_Idempotent(t *testing.T) {
	dir := setupConfigTestEnv(t)

	// Arrange: write a config file with plaintext secrets
	configDir := dir
	require.NoError(t, os.MkdirAll(configDir, 0755))
	configPath := filepath.Join(configDir, "mcp_config.json")

	originalJSON := `{
		"enabled": true,
		"servers": {
			"idemserver": {
				"name": "idemserver",
				"command": "npx",
				"args": ["-y", "some-mcp"],
				"auto_start": false,
				"max_restarts": 3,
				"env": {
					"AUTH_TOKEN": "first-secret-value"
				}
			}
		}
	}`
	require.NoError(t, os.WriteFile(configPath, []byte(originalJSON), 0600))

	// Act (first load) — should migrate
	first, err := LoadMCPConfig()
	require.NoError(t, err)
	require.NotNil(t, first.Servers["idemserver"].Credentials,
		"credentials map should exist after first load")
	assert.True(t, IsSecretRef(first.Servers["idemserver"].Credentials["AUTH_TOKEN"]),
		"first load should migrate AUTH_TOKEN to a ref in Credentials")
	_, envHasKey := first.Servers["idemserver"].Env["AUTH_TOKEN"]
	assert.False(t, envHasKey,
		"first load should remove AUTH_TOKEN from Env")

	// Verify credential store has the original value (not overwritten)
	val, _, err := credentials.GetFromActiveBackend("mcp/idemserver/AUTH_TOKEN")
	require.NoError(t, err)
	assert.Equal(t, "first-secret-value", val,
		"credential store should have the value from the first migration")

	// Act (second load) — file now has refs in Credentials, should be idempotent
	second, err := LoadMCPConfig()
	require.NoError(t, err)

	// The ref should still be a ref in Credentials (not double-migrated or altered)
	require.NotNil(t, second.Servers["idemserver"].Credentials,
		"credentials map should exist after second load")
	assert.True(t, IsSecretRef(second.Servers["idemserver"].Credentials["AUTH_TOKEN"]),
		"second load should not change the migrated ref in Credentials")
	_, envHasKey2 := second.Servers["idemserver"].Env["AUTH_TOKEN"]
	assert.False(t, envHasKey2,
		"AUTH_TOKEN should not be in Env after second load")

	// Verify the credential is still the original value
	val2, _, err := credentials.GetFromActiveBackend("mcp/idemserver/AUTH_TOKEN")
	require.NoError(t, err)
	assert.Equal(t, "first-secret-value", val2,
		"credential store value should not change on second load")
}

// ---------------------------------------------------------------------------
// TestSaveMCPConfig_AlreadyMigratedRefsPreserved
// ---------------------------------------------------------------------------

func TestSaveMCPConfig_AlreadyMigratedRefsPreserved(t *testing.T) {
	dir := setupConfigTestEnv(t)

	// Pre-store a credential so the ref is resolvable
	key := CredentialKey("refserver", "OPENAI_API_KEY")
	err := credentials.SetToActiveBackend(key, "sk-pre-existing")
	require.NoError(t, err)

	expectedRef := SecretRef("refserver", "OPENAI_API_KEY")

	// Build a config that already has refs (simulating a previously-migrated config)
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"refserver": {
				Name:        "refserver",
				Command:     "npx",
				Args:        []string{"-y", "some-server"},
				AutoStart:   true,
				MaxRestarts: 3,
				Env: map[string]string{
					"OPENAI_API_KEY": expectedRef, // Already a ref
					"PATH":           "/usr/bin",
				},
			},
		},
	}

	// Act
	err = SaveMCPConfig(&config)
	require.NoError(t, err)

	// Assert: in-memory ref was moved from Env to Credentials
	require.NotNil(t, config.Servers["refserver"].Credentials,
		"credentials map should exist in memory")
	assert.Equal(t, expectedRef, config.Servers["refserver"].Credentials["OPENAI_API_KEY"],
		"ref should be in Credentials in memory after save")
	_, refStillInEnv := config.Servers["refserver"].Env["OPENAI_API_KEY"]
	assert.False(t, refStillInEnv,
		"OPENAI_API_KEY should be removed from Env after migration to Credentials")
	assert.Equal(t, "/usr/bin", config.Servers["refserver"].Env["PATH"],
		"non-secret PATH should remain in Env")

	// Assert: on-disk ref is in credentials, not env
	raw := readRawConfigFile(t, dir)
	require.NotNil(t, raw.Servers["refserver"].Credentials,
		"credentials map should exist on disk")
	assert.Equal(t, expectedRef, raw.Servers["refserver"].Credentials["OPENAI_API_KEY"],
		"ref should be in credentials on disk")
	_, refOnDiskEnv := raw.Servers["refserver"].Env["OPENAI_API_KEY"]
	assert.False(t, refOnDiskEnv,
		"OPENAI_API_KEY should not be in env on disk")
	assert.Equal(t, "/usr/bin", raw.Servers["refserver"].Env["PATH"],
		"non-secret PATH should remain /usr/bin on disk")

	// Assert: the credential store still has the original value
	val, _, err := credentials.GetFromActiveBackend(key)
	require.NoError(t, err)
	assert.Equal(t, "sk-pre-existing", val,
		"credential store value should not be overwritten")
}

// ---------------------------------------------------------------------------
// TestSaveMCPConfig_MultipleServersMigrateIndependently
// ---------------------------------------------------------------------------

func TestSaveMCPConfig_MultipleServersMigrateIndependently(t *testing.T) {
	dir := setupConfigTestEnv(t)

	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"server-a": {
				Name:        "server-a",
				Command:     "npx",
				Args:        []string{"-y", "pkg-a"},
				AutoStart:   true,
				MaxRestarts: 3,
				Env: map[string]string{
					"OPENAI_API_KEY": "sk-server-a",
					"MODEL":          "gpt-4", // non-secret
				},
			},
			"server-b": {
				Name:        "server-b",
				Command:     "npx",
				Args:        []string{"-y", "pkg-b"},
				AutoStart:   false,
				MaxRestarts: 3,
				Env: map[string]string{
					"AUTH_TOKEN": "bearer-server-b",
				},
			},
		},
	}

	err := SaveMCPConfig(&config)
	require.NoError(t, err)

	// Both servers should have migrated to Credentials
	require.NotNil(t, config.Servers["server-a"].Credentials,
		"server-a credentials map should exist")
	assert.True(t, IsSecretRef(config.Servers["server-a"].Credentials["OPENAI_API_KEY"]),
		"server-a OPENAI_API_KEY should be a credential ref in Credentials")
	_, secretAInEnv := config.Servers["server-a"].Env["OPENAI_API_KEY"]
	assert.False(t, secretAInEnv,
		"server-a OPENAI_API_KEY should be removed from Env")
	assert.Equal(t, "gpt-4", config.Servers["server-a"].Env["MODEL"])

	require.NotNil(t, config.Servers["server-b"].Credentials,
		"server-b credentials map should exist")
	assert.True(t, IsSecretRef(config.Servers["server-b"].Credentials["AUTH_TOKEN"]),
		"server-b AUTH_TOKEN should be a credential ref in Credentials")
	_, secretBInEnv := config.Servers["server-b"].Env["AUTH_TOKEN"]
	assert.False(t, secretBInEnv,
		"server-b AUTH_TOKEN should be removed from Env")

	// Check credentials
	valA, _, err := credentials.GetFromActiveBackend("mcp/server-a/OPENAI_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-server-a", valA)

	valB, _, err := credentials.GetFromActiveBackend("mcp/server-b/AUTH_TOKEN")
	require.NoError(t, err)
	assert.Equal(t, "bearer-server-b", valB)

	// Check on-disk
	raw := readRawConfigFile(t, dir)
	require.NotNil(t, raw.Servers["server-a"].Credentials,
		"server-a credentials map should exist on disk")
	assert.True(t, IsSecretRef(raw.Servers["server-a"].Credentials["OPENAI_API_KEY"]),
		"server-a OPENAI_API_KEY should be a credential ref in credentials on disk")
	_, secretAOnDiskEnv := raw.Servers["server-a"].Env["OPENAI_API_KEY"]
	assert.False(t, secretAOnDiskEnv,
		"server-a OPENAI_API_KEY should not be in env on disk")
	assert.Equal(t, "gpt-4", raw.Servers["server-a"].Env["MODEL"])
	require.NotNil(t, raw.Servers["server-b"].Credentials,
		"server-b credentials map should exist on disk")
	assert.True(t, IsSecretRef(raw.Servers["server-b"].Credentials["AUTH_TOKEN"]),
		"server-b AUTH_TOKEN should be a credential ref in credentials on disk")
	_, secretBOnDiskEnv := raw.Servers["server-b"].Env["AUTH_TOKEN"]
	assert.False(t, secretBOnDiskEnv,
		"server-b AUTH_TOKEN should not be in env on disk")
}

// ---------------------------------------------------------------------------
// TestSaveMCPConfig_NoServers_NoError
// ---------------------------------------------------------------------------

func TestSaveMCPConfig_NoServers_NoError(t *testing.T) {
	dir := setupConfigTestEnv(t)

	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{},
	}

	err := SaveMCPConfig(&config)
	require.NoError(t, err)

	// Verify the file was written and is valid JSON
	raw := readRawConfigFile(t, dir)
	assert.True(t, raw.Enabled)
	assert.Empty(t, raw.Servers)
}

// ---------------------------------------------------------------------------
// TestMigrateSecretsOnLoad_NilAndEmptyConfigs
// ---------------------------------------------------------------------------

func TestMigrateSecretsOnLoad_NilAndEmptyConfigs(t *testing.T) {
	t.Run("nil config does not panic", func(t *testing.T) {
		// This directly tests the unexported function since we're in the same package
		migrateSecretsOnLoad(nil) // Should not panic
	})

	t.Run("nil servers map does not panic", func(t *testing.T) {
		cfg := &MCPConfig{Servers: nil}
		migrateSecretsOnLoad(cfg) // Should not panic
	})

	t.Run("empty servers map does not error", func(t *testing.T) {
		cfg := &MCPConfig{Servers: map[string]MCPServerConfig{}}
		migrateSecretsOnLoad(cfg) // Should not panic
	})
}
