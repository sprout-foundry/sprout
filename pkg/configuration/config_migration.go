package configuration

// config_migration.go — the config migration framework: the MigrationFunc
// / migrationStep types, the migration registry (registerMigration, the
// sync.Once registration guard, ensureRegistered), the version comparison
// (compareConfigVersions), the MigrateConfig / buildMigrationChain dispatch,
// and the init registration. The individual migration step functions live in
// config_migration_steps.go.

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
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

// registerMigration adds a migration step to the global registry.
// It returns an error if a migration from the same source version is already
// registered (at-most-one-step-per-source prevents ambiguous chains).
func registerMigration(from, to string, fn MigrationFunc) error {
	for _, m := range migrationRegistry {
		if m.from == from {
			return fmt.Errorf("config migration: duplicate source version %q", from)
		}
	}
	migrationRegistry = append(migrationRegistry, migrationStep{from: from, to: to, fn: fn})
	return nil
}

// registrationOnce guards one-time population of migrationRegistry from the
// built-in migration table. init() cannot return errors, so we defer the
// registration into a sync.Once and surface any failure via MigrateConfig.
var (
	registrationOnce sync.Once
	registrationErr  error
)

func ensureRegistered() {
	registrationOnce.Do(func() {
		registrationErr = registerMigration("0.0", "2.0", migrateV0ToV2)
		if registrationErr != nil {
			return
		}
		registrationErr = registerMigration("1.0", "2.0", migrateV1ToV2)
		if registrationErr != nil {
			return
		}
		registrationErr = registerMigration("2.0", "2.1", migrateV2ToV2_1)
		if registrationErr != nil {
			return
		}
		registrationErr = registerMigration("2.1", "3.0", migrateV2ToV3)
	})
}

// migrationLogged dedupes successful migration log lines to one per unique migration.
// MigrateConfig may be called multiple times during startup (one per subsystem
// re-read), so we dedupe to avoid spamming the same migration message.
var migrationLogged sync.Map

// ConfigFromNewerBuildError reports that the on-disk config version is newer
// than this binary's ConfigVersion. This happens when a newer sprout (e.g. a
// dev build or a post-update install) has already migrated the config and an
// older binary is now reading it. The config is still parseable — unknown
// fields are ignored by json.Unmarshal — so callers should load it as-is
// rather than treat this as a hard failure.
type ConfigFromNewerBuildError struct {
	ConfigVersion string
	BuildVersion  string
}

func (e *ConfigFromNewerBuildError) Error() string {
	return fmt.Sprintf("config version %q is newer than this build's %q — run `sprout upgrade` to catch up (config is used as-is)",
		e.ConfigVersion, e.BuildVersion)
}

// compareConfigVersions compares dotted numeric version strings ("2.1" vs "2.10").
// Returns -1 if a < b, 0 if equal, 1 if a > b. Non-numeric or empty segments
// compare as 0 so unknown shapes never trigger the newer-config path.
func compareConfigVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	n := len(aParts)
	if len(bParts) > n {
		n = len(bParts)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(aParts) {
			av, _ = strconv.Atoi(strings.TrimSpace(aParts[i]))
		}
		if i < len(bParts) {
			bv, _ = strconv.Atoi(strings.TrimSpace(bParts[i]))
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

// MigrateConfig applies all necessary migration steps to bring raw config up to the target version.
// It takes a raw JSON map, determines the current version, and runs each step in order.
// Returns the migrated raw config or an error if a step fails or the chain cannot reach the target.
func MigrateConfig(raw map[string]interface{}, targetVersion string) (map[string]interface{}, error) {
	ensureRegistered()
	if registrationErr != nil {
		return raw, fmt.Errorf("config migration registry initialization failed: %w", registrationErr)
	}

	currentVersion, _ := raw["version"].(string)
	if currentVersion == "" {
		currentVersion = "0.0" // Treat unversioned configs as "0.0"
	}
	if currentVersion == targetVersion {
		return raw, nil
	}

	// A config written by a newer build (e.g. a dev build bumped
	// ConfigVersion and saved, then an older installed binary runs).
	// There is no downgrade path by design; surface a typed error so
	// callers can distinguish this benign case from a broken chain.
	if compareConfigVersions(currentVersion, targetVersion) > 0 {
		return raw, &ConfigFromNewerBuildError{ConfigVersion: currentVersion, BuildVersion: targetVersion}
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
		key := fmt.Sprintf("migrated config from %q to %q", step.from, step.to)
		if _, dup := migrationLogged.LoadOrStore(key, struct{}{}); !dup {
			log.Printf("[config] %s", key)
		}
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

func init() {
	// Migration registration happens lazily via ensureRegistered() (sync.Once)
	// so that a duplicate source version surfaces as an error from MigrateConfig
	// rather than panicking at process start.
}
