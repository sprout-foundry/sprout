package agent

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// settingDef is the single source of truth for a configurable setting.
// All exported APIs (AllSettings, getConfigValue, setConfigValue, etc.)
// are derived from the settingDefs registry below.
type settingDef struct {
	Key         string
	Description string
	ValidValues string
	GetValue    func(cfg *configuration.Config) string
	SetValue    func(cfg *configuration.Config, value string) error
	EnumValues  []string // empty = not an enum
	ListType    bool     // true for comma-separated list settings (add/remove/set UI)
}

// settingDefs is the single registry of all configurable settings, assembled
// from the per-category slices in the settings_defs_*.go files. Every other
// data structure (supportedSettings map, AllSettings slice, the
// getConfigValue/setConfigValue switches) is derived from this slice. Entry
// order is preserved across the category files.
var settingDefs = func() []settingDef {
	defs := make([]settingDef, 0, len(settingDefsProvider)+len(settingDefsHistory)+len(settingDefsNotifications)+len(settingDefsShell))
	defs = append(defs, settingDefsProvider...)
	defs = append(defs, settingDefsHistory...)
	defs = append(defs, settingDefsNotifications...)
	defs = append(defs, settingDefsShell...)
	return defs
}()

// supportedSettings is built from settingDefs at init time.
// It is used by validateSettingKey for key existence checks and
// by SupportedSettingKeys for enumeration.
var supportedSettings = buildSupportedSettings()

func buildSupportedSettings() map[string]string {
	m := make(map[string]string, len(settingDefs))
	for _, d := range settingDefs {
		m[d.Key] = d.Description
	}
	return m
}

// lookupSettingDef returns the settingDef for a key (case-insensitive), or nil.
func lookupSettingDef(key string) *settingDef {
	k := strings.ToLower(key)
	for i := range settingDefs {
		if settingDefs[i].Key == k {
			return &settingDefs[i]
		}
	}
	return nil
}

// SettingEnumValues returns the enum values for a setting key, or nil if
// the setting is not an enum (freeform input). It is used by the interactive
// settings browser to offer a picker instead of raw text input.
func SettingEnumValues(key string) []string {
	d := lookupSettingDef(key)
	if d == nil || len(d.EnumValues) == 0 {
		return nil
	}
	return d.EnumValues
}

// SettingIsListType returns true if the setting key is a list-type setting
// that should get an add/remove/set sub-menu in the interactive browser.
func SettingIsListType(key string) bool {
	d := lookupSettingDef(key)
	return d != nil && d.ListType
}
