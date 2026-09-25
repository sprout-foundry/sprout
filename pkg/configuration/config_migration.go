package configuration

import (
	"encoding/json"
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

// applyAPITimeoutDefaults ensures all API timeout fields have values.
// It preserves any existing non-zero values and applies defaults to missing or zero fields.
func applyAPITimeoutDefaults(raw map[string]interface{}) {
	const (
		defaultConnectionTimeout    = 300.0
		defaultFirstChunkTimeout    = 600.0
		defaultChunkTimeout         = 600.0
		defaultOverallTimeout       = 1800.0
		defaultCommitMessageTimeout = 300.0
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

// applyPDFOCRDefaults strips the legacy PDF-OCR config keys. The
// second-tier OCR-fallback machinery they fed was removed (vision-first:
// pixels attach inline; non-vision primaries get native OCR); the keys are
// deleted from the map so they stop round-tripping through config saves.
func applyPDFOCRDefaults(raw map[string]interface{}) {
	delete(raw, "pdf_ocr_provider")
	delete(raw, "pdf_ocr_model")
	delete(raw, "pdf_ocr_enabled")
	delete(raw, "pdf_ocr_downloaded")
	delete(raw, "ocr_fallback_model")
	delete(raw, "vision_fallback_to_ocr")
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

// applyUnifiedRiskResolverDefault enables the unified risk resolver (SP-068)
// by default. The unified resolver collapses the former dual-gate security
// path (static classifier → persona cascade) into a single ResolveToolRisk
// assessment, removing ~50% of the security-system complexity. Users who
// need the legacy dual-gate behavior for compatibility can set
// "unified_risk_resolver": false in their config to opt out.
func applyUnifiedRiskResolverDefault(raw map[string]interface{}) {
	if _, exists := raw["unified_risk_resolver"]; !exists {
		raw["unified_risk_resolver"] = true
	}
}

// applyDaemonMultiSessionDefault enables N parallel browser windows
// per user on the daemon (SP-118 Phase 4) by default. The default
// flips the rollout so newly-spawned daemons accept multiple windows
// per user with no extra configuration. Operators who need the
// pre-SP-118 single-active-session behavior on the daemon can set
// "daemon_multi_session": false to opt out (e.g. for a temporary
// rollback during the rollout window).
//
// The agent path is unaffected — sprout agent always uses Mode 1
// regardless of this setting. The flag only gates Mode 2 in the
// daemon path; see pkg/webui/websocket_handler.go shouldUseMode1.
func applyDaemonMultiSessionDefault(raw map[string]interface{}) {
	if _, exists := raw["daemon_multi_session"]; !exists {
		raw["daemon_multi_session"] = true
	}
}

// applyMapInitializations ensures required map fields are initialized to empty maps.
// It only sets fields that are nil/missing — existing values are preserved.
func applyMapInitializations(raw map[string]interface{}) {
	// List of map fields that should always exist as empty objects
	mapFields := []string{
		"provider_models",
		"preferences",
		"custom_providers",
		"subagent_types",
		"skills",
	}

	for _, field := range mapFields {
		if _, exists := raw[field]; !exists {
			raw[field] = make(map[string]interface{})
		}
	}

	// MCP needs special handling: ensure mcp.servers exists
	if _, exists := raw["mcp"]; !exists {
		raw["mcp"] = map[string]interface{}{
			"servers": make(map[string]interface{}),
		}
	} else {
		mcp, ok := raw["mcp"].(map[string]interface{})
		if ok {
			if _, exists := mcp["servers"]; !exists {
				mcp["servers"] = make(map[string]interface{})
			}
		}
	}
}

// applyDefaultSubagentTypes merges default subagent type entries for any missing IDs.
// It uses json.Marshal/Unmarshal to convert SubagentType structs to raw JSON-compatible maps.
func applyDefaultSubagentTypes(raw map[string]interface{}) {
	subagentTypes, ok := raw["subagent_types"].(map[string]interface{})
	if !ok {
		subagentTypes = make(map[string]interface{})
		raw["subagent_types"] = subagentTypes
	}

	// Get default subagent types
	defaultTypes := defaultSubagentTypes()

	// For each default type, add it if not already present
	for id, persona := range defaultTypes {
		if _, exists := subagentTypes[id]; !exists {
			// Convert SubagentType struct to map[string]interface{}
			personaData, err := json.Marshal(persona)
			if err != nil {
				log.Printf("[config] warning: failed to serialize subagent type %q: %v", id, err)
				continue // Skip if serialization fails (shouldn't happen)
			}
			var personaMap map[string]interface{}
			if err := json.Unmarshal(personaData, &personaMap); err != nil {
				log.Printf("[config] warning: failed to unmarshal subagent type %q: %v", id, err)
				continue // Skip if deserialization fails
			}
			subagentTypes[id] = personaMap
		}
	}
}

// applyDefaultSkills merges default skill entries for any missing IDs.
// It uses json.Marshal/Unmarshal to convert Skill structs to raw JSON-compatible maps.
func applyDefaultSkills(raw map[string]interface{}) {
	skills, ok := raw["skills"].(map[string]interface{})
	if !ok {
		skills = make(map[string]interface{})
		raw["skills"] = skills
	}

	// Get default skills
	defaultSkills := defaultSkills()

	// For each default skill, add it if not already present
	for id, skill := range defaultSkills {
		if _, exists := skills[id]; !exists {
			// Convert Skill struct to map[string]interface{}
			skillData, err := json.Marshal(skill)
			if err != nil {
				log.Printf("[config] warning: failed to serialize skill %q: %v", id, err)
				continue // Skip if serialization fails (shouldn't happen)
			}
			var skillMap map[string]interface{}
			if err := json.Unmarshal(skillData, &skillMap); err != nil {
				log.Printf("[config] warning: failed to unmarshal skill %q: %v", id, err)
				continue // Skip if deserialization fails
			}
			skills[id] = skillMap
		}
	}
}

// applyLegacyToolAllowlistMigration adds structured tool names to legacy persona allowlists.
// This replicates the mergeLegacyStructuredToolsIntoPersonaAllowlists logic but on raw JSON maps.
func applyLegacyToolAllowlistMigration(raw map[string]interface{}) {
	subagentTypes, ok := raw["subagent_types"].(map[string]interface{})
	if !ok {
		return // No subagent types to migrate
	}

	defaults := defaultSubagentTypes()

	// First pass: add write_structured_file and patch_structured_file to personas
	// that have write_file or edit_file but not the structured tools
	for id, personaRaw := range subagentTypes {
		persona, ok := personaRaw.(map[string]interface{})
		if !ok {
			continue
		}

		// Skip if not a known default persona
		normalizedID := normalizePersonaID(id)
		if _, exists := defaults[normalizedID]; !exists {
			continue
		}

		// Get allowed_tools array
		toolsRaw, hasTools := persona["allowed_tools"]
		if !hasTools {
			continue
		}

		tools, ok := toolsRaw.([]interface{})
		if !ok || len(tools) == 0 {
			continue
		}

		// Check if persona has write_file or edit_file
		hasWriteFile := hasRawTool(tools, "write_file")
		hasEditFile := hasRawTool(tools, "edit_file")
		if !hasWriteFile && !hasEditFile {
			continue
		}

		changed := false

		// Add write_structured_file if missing
		if !hasRawTool(tools, "write_structured_file") {
			tools = append(tools, "write_structured_file")
			changed = true
		}

		// Add patch_structured_file if missing
		if !hasRawTool(tools, "patch_structured_file") {
			tools = append(tools, "patch_structured_file")
			changed = true
		}

		if changed {
			persona["allowed_tools"] = tools
			subagentTypes[id] = persona
		}
	}

	// Second pass: add shell_command to web_scraper if missing
	for id, personaRaw := range subagentTypes {
		persona, ok := personaRaw.(map[string]interface{})
		if !ok {
			continue
		}

		// Check if this is web_scraper
		normalizedID := normalizePersonaID(id)
		if normalizedID != "web_scraper" {
			continue
		}

		// Get allowed_tools array
		toolsRaw, hasTools := persona["allowed_tools"]
		if !hasTools {
			continue
		}

		tools, ok := toolsRaw.([]interface{})
		if !ok || len(tools) == 0 {
			continue
		}

		// Add shell_command if missing
		if !hasRawTool(tools, "shell_command") {
			tools = append(tools, "shell_command")
			persona["allowed_tools"] = tools
			subagentTypes[id] = persona
		}
	}
}

// hasRawTool checks if a tool name exists in a raw tools array
func hasRawTool(tools []interface{}, candidate string) bool {
	for _, tool := range tools {
		if toolStr, ok := tool.(string); ok && toolStr == candidate {
			return true
		}
	}
	return false
}

// applyDefaultPersonaAllowedTools merges current default tools into default personas'
// allowed_tools lists. This uses an additive merge strategy: missing default tools are
// added, but any user-added extras are preserved. Custom personas are left untouched.
//
// This ensures that saved configs stay honest about what tools are available when new
// tools are added to defaults, without silently removing user customizations.
func applyDefaultPersonaAllowedTools(raw map[string]interface{}) {
	subagentTypes, ok := raw["subagent_types"].(map[string]interface{})
	if !ok {
		return
	}

	defaults := defaultSubagentTypes()

	for id, personaRaw := range subagentTypes {
		persona, ok := personaRaw.(map[string]interface{})
		if !ok {
			continue
		}

		// Resolve the persona ID: check both the map key and the "id" field
		// This matches GetSubagentType()'s lookup behavior.
		normalizedID := normalizePersonaID(id)
		if idVal, hasID := persona["id"]; hasID {
			if idStr, ok := idVal.(string); ok {
				normalizedIDFromField := normalizePersonaID(idStr)
				if _, exists := defaults[normalizedIDFromField]; exists {
					normalizedID = normalizedIDFromField
				}
			}
		}

		defaultPersona, exists := defaults[normalizedID]
		if !exists {
			continue // Custom persona — leave untouched
		}

		// Merge: add any missing default tools, preserve user extras
		existingTools, hasTools := persona["allowed_tools"]
		if !hasTools {
			// No tools at all — set to defaults
			defaultTools := make([]interface{}, len(defaultPersona.AllowedTools))
			for i, tool := range defaultPersona.AllowedTools {
				defaultTools[i] = tool
			}
			persona["allowed_tools"] = defaultTools
			subagentTypes[id] = persona
			continue
		}

		toolsSlice, ok := existingTools.([]interface{})
		if !ok || len(toolsSlice) == 0 {
			// Malformed or empty — set to defaults
			defaultTools := make([]interface{}, len(defaultPersona.AllowedTools))
			for i, tool := range defaultPersona.AllowedTools {
				defaultTools[i] = tool
			}
			persona["allowed_tools"] = defaultTools
			subagentTypes[id] = persona
			continue
		}

		// Build a set of existing tools
		existingSet := make(map[string]bool, len(toolsSlice))
		for _, t := range toolsSlice {
			if s, ok := t.(string); ok {
				existingSet[s] = true
			}
		}

		// Add missing default tools
		changed := false
		for _, tool := range defaultPersona.AllowedTools {
			if !existingSet[tool] {
				toolsSlice = append(toolsSlice, tool)
				changed = true
			}
		}

		if changed {
			persona["allowed_tools"] = toolsSlice
			subagentTypes[id] = persona
		}
	}
}

// applyV3Defaults applies version 3.0 defaults.
// This syncs default persona allowed_tools with current definitions.
func applyV3Defaults(raw map[string]interface{}) error {
	applyDefaultPersonaAllowedTools(raw)
	return nil
}

// migrateV2ToV2_1 repairs the wakeup all-zeros artifact. The pre-pointer
// partial-settings patch path (see wakeupPatch's doc comment) zeroed the
// whole WakeupConfig on any wakeup patch, persisting {"enabled": false,
// "max_tokens_per_session": 0, "max_resumes_per_session": 0} into user
// configs and permanently disabling auto-resume — which is meant to default
// to ON. A deliberate opt-out carries the default budgets (5000/10) because
// any full save materializes them, so the exact false+0+0 signature uniquely
// identifies the wipe artifact: drop the block entirely and let defaults
// apply. Idempotent: configs without the artifact pass through unchanged.
func migrateV2ToV2_1(raw map[string]interface{}) error {
	wakeup, ok := raw["wakeup"].(map[string]interface{})
	if !ok {
		return nil
	}
	enabled, _ := wakeup["enabled"].(bool)
	if enabled {
		return nil
	}
	// Require the exact machine-written artifact signature: all three keys
	// present with false/0/0. A hand-written {"enabled": false} (budgets
	// absent) is treated as a deliberate minimal opt-out and kept.
	maxTokens, hasTokens := numericField(wakeup, "max_tokens_per_session")
	if !hasTokens || maxTokens != 0 {
		return nil
	}
	maxResumes, hasResumes := numericField(wakeup, "max_resumes_per_session")
	if !hasResumes || maxResumes != 0 {
		return nil
	}
	delete(raw, "wakeup")
	return nil
}

// numericField reads a numeric field that may have decoded as float64
// (json.Unmarshal into map[string]interface{}) or int (hand-built test
// maps). Returns (value, present).
func numericField(m map[string]interface{}, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, true
}

// migrateV2ToV3 handles migration from version 2.0 to version 3.0.
// It syncs default persona allowed_tools with current defaults so that
// saved configs stay honest about what tools are actually available.
func migrateV2ToV3(raw map[string]interface{}) error {
	return applyV3Defaults(raw)
}

// applyV2Defaults applies all version 2.0 default values to a config.
// This function is idempotent and can be called multiple times safely.
func applyV2Defaults(raw map[string]interface{}) error {
	applyMapInitializations(raw)
	applyAPITimeoutDefaults(raw)
	applyPDFOCRDefaults(raw)
	applyZshCommandDetectionDefaults(raw)
	applyUnifiedRiskResolverDefault(raw)
	applyDaemonMultiSessionDefault(raw)
	applyDefaultSubagentTypes(raw)
	applyDefaultSkills(raw)
	applyLegacyToolAllowlistMigration(raw)
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
	// Migration registration happens lazily via ensureRegistered() (sync.Once)
	// so that a duplicate source version surfaces as an error from MigrateConfig
	// rather than panicking at process start.
}
