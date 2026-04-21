package configuration

import (
	"fmt"
	"log"
)

// MigrationFunc transforms a raw JSON config from one version to the next.
// It receives and returns a map[string]interface{} representing the parsed config JSON.
// The function should set "version" to the target version on success.
type MigrationFunc func(raw map[string]interface{}) error

// migrationStep represents a single migration from one version to another.
type migrationStep struct {
	from string
	to   string
	fn   MigrationFunc
}

// migrationRegistry holds all registered migration steps.
var migrationRegistry []migrationStep

// RegisterMigration adds a migration step to the global registry.
// It panics if a migration from the same source version is already registered
// (at-most-one-step-per-source prevents ambiguous chains).
func RegisterMigration(from, to string, fn MigrationFunc) {
	for _, m := range migrationRegistry {
		if m.from == from {
			panic(fmt.Sprintf("config migration: duplicate source version %q", from))
		}
	}
	migrationRegistry = append(migrationRegistry, migrationStep{from: from, to: to, fn: fn})
}

// MigrateConfig applies all necessary migration steps to bring raw config up to the target version.
// It takes a raw JSON map, determines the current version, and runs each step in order.
// Returns the migrated raw config or an error if a step fails or the chain cannot reach the target.
func MigrateConfig(raw map[string]interface{}, targetVersion string) (map[string]interface{}, error) {
	currentVersion, _ := raw["version"].(string)
	if currentVersion == "" {
		currentVersion = "0.0" // Treat unversioned configs as "0.0"
	}
	if currentVersion == targetVersion {
		return raw, nil
	}

	// Build ordered migration chain
	steps := buildMigrationChain(currentVersion, targetVersion)
	if steps == nil {
		return raw, fmt.Errorf("config migration: no migration path from %q to %q", currentVersion, targetVersion)
	}

	for _, step := range steps {
		if err := step.fn(raw); err != nil {
			return raw, fmt.Errorf("config migration %q → %q failed: %w", step.from, step.to, err)
		}
		raw["version"] = step.to
		log.Printf("[config] migrated config from %q to %q", step.from, step.to)
	}

	return raw, nil
}

// buildMigrationChain returns an ordered slice of migration steps from fromVersion to toVersion.
// Returns nil if no valid chain exists.
func buildMigrationChain(fromVersion, toVersion string) []migrationStep {
	// Build a lookup: source version → step
	lookup := make(map[string]migrationStep, len(migrationRegistry))
	for _, m := range migrationRegistry {
		lookup[m.from] = m
	}

	// Walk the chain
	var chain []migrationStep
	current := fromVersion
	seen := make(map[string]bool)
	for current != toVersion {
		if seen[current] {
			return nil // cycle detected
		}
		seen[current] = true

		step, ok := lookup[current]
		if !ok {
			return nil // no migration step from this version
		}
		chain = append(chain, step)
		current = step.to
	}
	return chain
}

// applyAPITimeoutDefaults ensures all API timeout fields have values.
// It preserves any existing non-zero values and applies defaults to missing or zero fields.
func applyAPITimeoutDefaults(raw map[string]interface{}) {
	const (
		defaultConnectionTimeout      = 300.0
		defaultFirstChunkTimeout      = 600.0
		defaultChunkTimeout           = 600.0
		defaultOverallTimeout         = 1800.0
		defaultCommitMessageTimeout   = 300.0
	)

	var apiTimeouts map[string]interface{}
	if existing, ok := raw["api_timeouts"].(map[string]interface{}); ok {
		apiTimeouts = existing
	} else {
		apiTimeouts = make(map[string]interface{})
	}

	// Helper to set default if missing or zero
	setTimeoutDefault := func(field string, defaultValue float64) {
		if apiTimeouts[field] == nil {
			apiTimeouts[field] = defaultValue
			return
		}
		// Check if it's a zero numeric value
		if val, ok := apiTimeouts[field].(float64); ok && val == 0 {
			apiTimeouts[field] = defaultValue
		}
	}

	setTimeoutDefault("connection_timeout_sec", defaultConnectionTimeout)
	setTimeoutDefault("first_chunk_timeout_sec", defaultFirstChunkTimeout)
	setTimeoutDefault("chunk_timeout_sec", defaultChunkTimeout)
	setTimeoutDefault("overall_timeout_sec", defaultOverallTimeout)
	setTimeoutDefault("commit_message_timeout_sec", defaultCommitMessageTimeout)

	raw["api_timeouts"] = apiTimeouts
}

// applyPDFOCRDefaults ensures PDF OCR fields have values.
// It preserves any existing non-default values and applies defaults only if all three fields
// are at their "unset" values (false for bool, empty string for string, nil for missing).
func applyPDFOCRDefaults(raw map[string]interface{}) {
	enabled, hasEnabled := raw["pdf_ocr_enabled"]
	provider, hasProvider := raw["pdf_ocr_provider"]
	model, hasModel := raw["pdf_ocr_model"]

	// Check if fields are at their unset values
	enabledUnset := !hasEnabled || enabled == nil || enabled == false
	providerUnset := !hasProvider || provider == nil || provider == ""
	modelUnset := !hasModel || model == nil || model == ""

	// Only apply defaults if all three fields are unset
	if enabledUnset && providerUnset && modelUnset {
		raw["pdf_ocr_enabled"] = true
		raw["pdf_ocr_provider"] = "ollama"
		raw["pdf_ocr_model"] = "glm-ocr"
	}
}

// applyZshCommandDetectionDefaults ensures zsh command detection fields have values.
// It sets defaults only if the fields don't exist (preserving existing values).
func applyZshCommandDetectionDefaults(raw map[string]interface{}) {
	if _, exists := raw["enable_zsh_command_detection"]; !exists {
		raw["enable_zsh_command_detection"] = true
	}
	if _, exists := raw["auto_execute_detected_commands"]; !exists {
		raw["auto_execute_detected_commands"] = true
	}
}

// applyV2Defaults applies all version 2.0 default values to a config.
// This function is idempotent and can be called multiple times safely.
func applyV2Defaults(raw map[string]interface{}) error {
	applyAPITimeoutDefaults(raw)
	applyPDFOCRDefaults(raw)
	applyZshCommandDetectionDefaults(raw)
	return nil
}

// migrateV0ToV2 handles migration from pre-versioned configs (0.0) to version 2.0.
// It applies default values for fields that were introduced after the initial
// config format and handles the version field.
func migrateV0ToV2(raw map[string]interface{}) error {
	// Set version
	if raw["version"] == nil {
		raw["version"] = "2.0"
	}

	// Apply all version 2.0 defaults
	return applyV2Defaults(raw)
}

// migrateV1ToV2 handles migration from version 1.0 to version 2.0.
// It applies the same defaults as migrateV0ToV2, but preserves any existing
// values that were already set in version 1.0 configs.
func migrateV1ToV2(raw map[string]interface{}) error {
	// Apply all version 2.0 defaults
	return applyV2Defaults(raw)
}

func init() {
	// Register the migration from pre-versioned configs (0.0) to version 2.0
	RegisterMigration("0.0", "2.0", migrateV0ToV2)
	// Register the migration from version 1.0 to version 2.0
	RegisterMigration("1.0", "2.0", migrateV1ToV2)
}
