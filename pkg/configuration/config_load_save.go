package configuration

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// Load loads the configuration from file
func Load() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, fmt.Errorf("get config path for default: %w", err)
	}

	// If config doesn't exist, return new default config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return NewConfig(), nil
	}

	// Migrate any api_key values from config.json custom_providers to the credential store
	// before the Config struct unmarshal (which would silently discard api_key fields).
	if err := MigrateConfigFileAPIKeys(configPath); err != nil {
		log.Printf("[config] warning: config.json api_key migration failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Snapshot the file's stat for SP-034-4b conflict detection. Stat
	// AFTER the read so a concurrent writer that landed in between sees
	// us as having the older view (we'll detect the conflict on next
	// Save). Using ModTime + Size — both must match on save or we treat
	// it as a divergence.
	loadStat, statErr := os.Stat(configPath)
	var loadedMod time.Time
	var loadedSize int64
	if statErr == nil {
		loadedMod = loadStat.ModTime()
		loadedSize = loadStat.Size()
	}

	// Run version-based migrations on the raw JSON before struct unmarshaling.
	var rawConfig map[string]interface{}
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config file for migration: %w", err)
	}
	rawConfig, err = MigrateConfig(rawConfig, ConfigVersion)
	if err != nil {
		log.Printf("[config] warning: config migration failed, using as-is: %v", err)
		// Continue with original data — don't block startup
	} else {
		data, err = json.Marshal(rawConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to re-marshal migrated config: %w", err)
		}
	}

	config := NewConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Defensive nil-checks for map fields (migration ensures these exist in raw JSON,
	// but these checks provide Go-level safety for edge cases).
	if config.ProviderModels == nil {
		config.ProviderModels = make(map[string]string)
	}
	if config.Preferences == nil {
		config.Preferences = make(map[string]interface{})
	}
	if config.MCP.Servers == nil {
		config.MCP.Servers = make(map[string]mcp.MCPServerConfig)
	}
	if config.DismissedPrompts == nil {
		config.DismissedPrompts = make(map[string]bool)
	}
	if config.CustomProviders == nil {
		config.CustomProviders = make(map[string]CustomProviderConfig)
	}
	// Personas are catalog-fixed and never loaded from user config — hydrate
	// the in-memory map fresh from the embedded catalog every time so any
	// stale `subagent_types` data from a pre-removal config gets discarded.
	config.SubagentTypes = defaultSubagentTypes()
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	// Merge missing default skills so that skills added to embedded defaults
	// after the user's config was already at v2.0 are still available.
	mergeMissingDefaultSkills(config)

	// Post-unmarshal operations that truly need struct-level access
	fileCustomProviders, err := MigrateLegacyCustomProviders(config)
	if err != nil {
		return nil, fmt.Errorf("get config path: %w", err)
	}
	config.CustomProviders = fileCustomProviders

	if err := MigrateEmbeddedAPIKeys(config.CustomProviders); err != nil {
		log.Printf("[config] warning: credential migration failed: %v", err)
	}

	// Stamp the on-disk metadata BEFORE the discovery passes below so
	// that conflict detection only ever compares the canonical file
	// (skill discovery adds in-memory entries that aren't persisted).
	config.loadedModTime = loadedMod
	config.loadedSize = loadedSize

	// Discover user-level and project-specific skills.
	if discovered := config.discoverSkills(); len(discovered) > 0 {
		log.Printf("[skills] Discovered %d skill(s): %s",
			len(discovered), strings.Join(discovered, ", "))
	}

	// Self-heal a config that was poisoned by a leaky test run (e.g. a
	// past version of the codebase that wrote "test" to disk before
	// the Save-time guard existed). On the next CLI start the value
	// gets cleared and the user is prompted normally instead of
	// /commit silently routing to a no-op mock.
	sanitizeTestProvider(config)

	// Migrate legacy approved_shell_commands to unified command_policies
	MigrateCommandPolicies(config)

	return config, nil
}

// Save saves the configuration to file
func (c *Config) Save() error {
	// Defense-in-depth: never persist "test" as the active provider.
	// The TestClientType sentinel is for in-process test fixtures only;
	// if it ever reaches disk, the next CLI run picks it up and tries
	// to route requests (including /commit) to a no-op mock. Strip it
	// here so even tests that bypass Manager.SetProvider can't poison
	// the real config.
	sanitizeTestProvider(c)

	// Migrate any plaintext secrets in MCP server env blocks to the
	// credential store before persisting. This is defense-in-depth: most
	// callers already migrate before reaching here, but this ensures the
	// main config file never contains raw token values regardless.
	for name := range c.MCP.Servers {
		s := c.MCP.Servers[name]
		count, err := mcp.MigrateEnvSecretsFromServer(name, &s)
		if err != nil {
			log.Printf("[config] Warning: failed to migrate MCP secrets for server %s: %v", name, err)
		} else if count > 0 {
			c.MCP.Servers[name] = s
		}
	}

	configPath, err := GetConfigPath()
	if err != nil {
		return fmt.Errorf("get config path for save: %w", err)
	}

	// SP-034-4b: detect concurrent writers. Only enforce the check when
	// this Config was actually loaded from a file (loadedModTime set);
	// fresh-from-NewConfig() saves bypass the check by design — they're
	// either the first ever save, or an explicit "reset to defaults"
	// that should overwrite whatever's there.
	if !c.loadedModTime.IsZero() {
		if stat, statErr := os.Stat(configPath); statErr == nil {
			if !stat.ModTime().Equal(c.loadedModTime) || stat.Size() != c.loadedSize {
				return &ConfigConflictError{
					Path:           configPath,
					LoadedModTime:  c.loadedModTime,
					LoadedSize:     c.loadedSize,
					CurrentModTime: stat.ModTime(),
					CurrentSize:    stat.Size(),
				}
			}
		}
	}

	c.Version = ConfigVersion
	persisted := *c
	persisted.Version = ConfigVersion
	persisted.CustomProviders = nil
	data, err := json.MarshalIndent(&persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write with explicit 0600 permissions (owner read/write only)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// Refresh the loaded-stat snapshot so the NEXT Save's conflict
	// check compares against the file we just wrote, not the stale
	// pre-write state. Failure to re-stat is non-fatal — it just means
	// the next save's conflict check might false-positive once.
	if stat, statErr := os.Stat(configPath); statErr == nil {
		c.loadedModTime = stat.ModTime()
		c.loadedSize = stat.Size()
	}

	return nil
}

// SaveToDir saves the configuration to a specific directory, bypassing
// GetConfigPath() (which reads the SPROUT_CONFIG/LEDIT_CONFIG env vars).
// Use this when a Manager has an explicit configDir so that saves go to
// the correct location even after the env var has been restored.
func (c *Config) SaveToDir(dir string) error {
	// Same defense as Save() — refuse to persist the test sentinel even
	// when callers bypass GetConfigPath() and target an explicit dir.
	// See sanitizeTestProvider for context.
	sanitizeTestProvider(c)

	// Migrate any plaintext secrets in MCP server env blocks to the
	// credential store before persisting.
	for name := range c.MCP.Servers {
		s := c.MCP.Servers[name]
		count, err := mcp.MigrateEnvSecretsFromServer(name, &s)
		if err != nil {
			log.Printf("[config] Warning: failed to migrate MCP secrets for server %s: %v", name, err)
		} else if count > 0 {
			c.MCP.Servers[name] = s
		}
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}

	configPath := filepath.Join(dir, ConfigFileName)
	c.Version = ConfigVersion
	persisted := *c
	persisted.Version = ConfigVersion
	persisted.CustomProviders = nil
	data, err := json.MarshalIndent(&persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
