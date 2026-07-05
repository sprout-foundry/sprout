package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// handleManageSettings manages application settings and provider credentials.
// Operations: get, set, list_providers, test_credential, describe, describe_all, preview.
func handleManageSettings(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	operation, err := getStringArg(args, "operation")
	if err != nil {
		return "", agenterrors.Wrapf(err, "operation is required")
	}

	switch operation {
	case "get":
		return handleSettingsGet(a, args)
	case "set":
		return handleSettingsSet(a, args)
	case "list_providers":
		return handleSettingsListProviders(a, args)
	case "test_credential":
		return handleSettingsTestCredential(a, args)
	case "describe":
		return handleSettingsDescribe(a, args)
	case "describe_all":
		return handleSettingsDescribeAll(a, args)
	case "preview":
		return handleSettingsPreview(a, args)
	default:
		return "", agenterrors.NewValidation(fmt.Sprintf("unknown operation %q: must be one of get, set, list_providers, test_credential, describe, describe_all, preview", operation), nil)
	}
}

// handleSettingsGet retrieves a setting value by key.
func handleSettingsGet(a *Agent, args map[string]interface{}) (string, error) {
	key, err := getStringArg(args, "key")
	if err != nil {
		return "", agenterrors.Wrapf(err, "key is required for get operation")
	}

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return "", agenterrors.NewConfig("configuration not loaded", nil)
	}

	value, err := getConfigValue(cfg, key)
	if err != nil {
		return "", err
	}

	if value == "" {
		return fmt.Sprintf("Setting %q is not set", key), nil
	}
	return value, nil
}

// handleSettingsSet updates a setting value by key.
func handleSettingsSet(a *Agent, args map[string]interface{}) (string, error) {
	key, err := getStringArg(args, "key")
	if err != nil {
		return "", agenterrors.Wrapf(err, "key is required for set operation")
	}

	value, err := getStringArg(args, "value")
	if err != nil {
		return "", agenterrors.Wrapf(err, "value is required for set operation")
	}

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	if err := validateSettingKey(key); err != nil {
		return "", err
	}

	err = mgr.UpdateConfig(func(cfg *configuration.Config) error {
		return setConfigValue(cfg, key, value)
	})
	if err != nil {
		return "", agenterrors.Wrapf(err, "failed to set %q", key)
	}

	return fmt.Sprintf("Setting %q updated to %q", key, value), nil
}

// handleSettingsListProviders lists available providers.
func handleSettingsListProviders(a *Agent, args map[string]interface{}) (string, error) {
	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	providers := mgr.GetAvailableProviders()
	if len(providers) == 0 {
		return "No providers available", nil
	}

	// Optionally filter by a provider argument
	filter, _ := args["provider"].(string)
	filter = strings.TrimSpace(strings.ToLower(filter))

	var names []string
	for _, p := range providers {
		name := string(p)
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		names = append(names, name)
	}

	sort.Strings(names)

	var lines []string
	lines = append(lines, fmt.Sprintf("Available providers (%d):", len(names)))
	for _, n := range names {
		lines = append(lines, fmt.Sprintf("  - %s", n))
	}

	return strings.Join(lines, "\n"), nil
}

// handleSettingsTestCredential tests whether a provider has valid credentials configured.
func handleSettingsTestCredential(a *Agent, args map[string]interface{}) (string, error) {
	provider, err := getStringArg(args, "provider")
	if err != nil {
		return "", agenterrors.Wrapf(err, "provider is required for test_credential operation")
	}

	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return "", agenterrors.NewValidation("provider cannot be empty", nil)
	}

	if configuration.HasProviderAuth(provider) {
		return fmt.Sprintf("Provider %q has valid credentials configured", provider), nil
	}
	return fmt.Sprintf("Provider %q does not have credentials configured", provider), nil
}

// SettingDetail holds metadata for a setting key used by describe and describe_all.
type SettingDetail struct {
	Key         string
	Description string
	ValidValues string
	GetValue    func(cfg *configuration.Config) string
}

// AllSettings returns the complete list of setting definitions, derived from the
// single settingDefs registry.
func AllSettings() []SettingDetail {
	result := make([]SettingDetail, len(settingDefs))
	for i, d := range settingDefs {
		result[i] = SettingDetail{
			Key:         d.Key,
			Description: d.Description,
			ValidValues: d.ValidValues,
			GetValue:    d.GetValue,
		}
	}
	return result
}

// handleSettingsDescribe returns the description, valid values, and current value for a single setting.
func handleSettingsDescribe(a *Agent, args map[string]interface{}) (string, error) {
	key, err := getStringArg(args, "key")
	if err != nil {
		return "", agenterrors.Wrapf(err, "key is required for describe operation")
	}

	key = strings.ToLower(strings.TrimSpace(key))

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return "", agenterrors.NewConfig("configuration not loaded", nil)
	}

	for _, s := range AllSettings() {
		if s.Key == key {
			value := s.GetValue(cfg)
			if value == "" {
				value = "(not set)"
			}
			return fmt.Sprintf("%s — %s\nValid values: %s\nCurrent value: %s", s.Key, s.Description, s.ValidValues, value), nil
		}
	}

	return "", agenterrors.NewNotFound(fmt.Sprintf("setting key %q", key))
}

// handleSettingsDescribeAll returns all settings with descriptions, valid values, and current values.
func handleSettingsDescribeAll(a *Agent, args map[string]interface{}) (string, error) {
	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return "", agenterrors.NewConfig("configuration not loaded", nil)
	}

	var lines []string
	lines = append(lines, "Settings Overview")
	lines = append(lines, strings.Repeat("-", 70))

	for _, s := range AllSettings() {
		value := s.GetValue(cfg)
		if value == "" {
			value = "(not set)"
		}
		lines = append(lines, fmt.Sprintf("%-22s %s", s.Key+":", s.Description))
		lines = append(lines, fmt.Sprintf("  %-14s %s", "Valid values:", s.ValidValues))
		lines = append(lines, fmt.Sprintf("  %-14s %s", "Current:", value))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n"), nil
}

// handleSettingsPreview shows what would change before applying a setting, without actually changing it.
func handleSettingsPreview(a *Agent, args map[string]interface{}) (string, error) {
	key, err := getStringArg(args, "key")
	if err != nil {
		return "", agenterrors.Wrapf(err, "key is required for preview operation")
	}

	value, err := getStringArg(args, "value")
	if err != nil {
		return "", agenterrors.Wrapf(err, "value is required for preview operation")
	}

	key = strings.ToLower(strings.TrimSpace(key))

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return "", agenterrors.NewConfig("configuration not loaded", nil)
	}

	// Get current value
	currentValue, err := getConfigValue(cfg, key)
	if err != nil {
		// Maybe it's an extended setting
		for _, s := range AllSettings() {
			if s.Key == key {
				currentValue = s.GetValue(cfg)
				break
			}
		}
		if err != nil && currentValue == "" {
			return "", agenterrors.NewNotFound(fmt.Sprintf("setting key %q", key))
		}
	}

	if currentValue == "" {
		currentValue = "(not set)"
	}

	// Validate the proposed value by dry-running setConfigValue on a shallow
	// copy of the config. We must copy the full struct so persona validation
	// (GetSubagentType / IsPersonaDisabled) has access to the real registries.
	// Deep-copy pointer fields that setConfigValue mutates in-place so the
	// preview never accidentally mutates the real config.
	previewCfg := *cfg
	if cfg.APITimeouts != nil {
		copy := *cfg.APITimeouts
		previewCfg.APITimeouts = &copy
	}
	if cfg.Notifications != nil {
		copy := *cfg.Notifications
		previewCfg.Notifications = &copy
	}
	setErr := setConfigValue(&previewCfg, key, value)

	var notes []string

	// Check credential status for provider-related keys
	providerKeys := map[string]bool{"provider": true, "subagent_provider": true, "commit_provider": true, "review_provider": true}
	if providerKeys[key] && value != "" {
		provider := strings.TrimSpace(strings.ToLower(value))
		if configuration.HasProviderAuth(provider) {
			notes = append(notes, fmt.Sprintf("credential check — %s has valid credentials", provider))
		} else {
			notes = append(notes, fmt.Sprintf("WARNING — %s does not have credentials configured", provider))
		}
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Preview: Changing %s\n", key))
	result.WriteString(fmt.Sprintf("  Current:  %s\n", currentValue))
	result.WriteString(fmt.Sprintf("  Proposed: %s\n", value))

	if setErr != nil {
		result.WriteString(fmt.Sprintf("  ERROR: %s\n", setErr.Error()))
	} else if len(notes) > 0 {
		for _, n := range notes {
			result.WriteString(fmt.Sprintf("  Notes:  %s\n", n))
		}
	} else {
		result.WriteString("  Notes:  no issues detected\n")
	}

	return result.String(), nil
}

// validateSettingKey checks that a key is a recognized setting.
func validateSettingKey(key string) error {
	normalized := strings.ToLower(key)
	if _, ok := supportedSettings[normalized]; ok {
		return nil
	}
	validKeys := make([]string, 0, len(supportedSettings))
	for k := range supportedSettings {
		validKeys = append(validKeys, k)
	}
	sort.Strings(validKeys)
	return agenterrors.NewValidation(fmt.Sprintf("unknown setting key %q; valid keys: %s", key, strings.Join(validKeys, ", ")), nil)
}

// getConfigValue returns the string representation of a config setting by key.
func getConfigValue(cfg *configuration.Config, key string) (string, error) {
	d := lookupSettingDef(key)
	if d == nil {
		return "", validateSettingKey(key)
	}
	return d.GetValue(cfg), nil
}

// setConfigValue updates a config setting by key and value string.
func setConfigValue(cfg *configuration.Config, key, value string) error {
	d := lookupSettingDef(key)
	if d == nil {
		return validateSettingKey(key)
	}
	return d.SetValue(cfg, value)
}

// GetSettingValue returns the string representation of a config setting by key.
// It's an exported wrapper around getConfigValue for use by other packages.
func GetSettingValue(cfg *configuration.Config, key string) (string, error) {
	return getConfigValue(cfg, key)
}

// SetSettingValue updates a config setting by key and value string.
// It's an exported wrapper around setConfigValue for use by other packages.
func SetSettingValue(cfg *configuration.Config, key, value string) error {
	return setConfigValue(cfg, key, value)
}

// SupportedSettingKeys returns a sorted slice of all supported setting keys.
func SupportedSettingKeys() []string {
	keys := make([]string, 0, len(supportedSettings))
	for k := range supportedSettings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
