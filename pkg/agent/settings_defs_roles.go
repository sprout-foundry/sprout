package agent

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// settings_defs_roles.go — the role-model section
// of the settingDefs registry: one def per built-in role for its
// model and provider, keyed `role.<name>.model` and
// `role.<name>.provider`. This is the SET path for roles
// (`sprout config set role.coder.model …`); the read path
// (`sprout config get roles.coder`) resolves through the serialized
// roles section without a def.

var settingDefsRoles = buildRoleSettingDefs()

// buildRoleSettingDefs derives the role defs from the built-in role list so
// adding a built-in role stays a one-line change in
// configuration.BuiltInRoles. Every def read-modifies-writes the stored
// role entry, preserving the other field.
func buildRoleSettingDefs() []settingDef {
	defs := make([]settingDef, 0, len(configuration.BuiltInRoles())*2)
	for _, role := range configuration.BuiltInRoles() {
		defs = append(defs, roleSettingDef(role, "model"), roleSettingDef(role, "provider"))
	}
	return defs
}

func roleSettingDef(role, field string) settingDef {
	key := "role." + role + "." + field
	desc := "Provider for the " + role + " role"
	validValues := "provider name"
	if field == "model" {
		desc = "Model for the " + role + " role"
		validValues = "provider-specific model name"
	}
	return settingDef{
		Key:         key,
		Description: desc,
		ValidValues: validValues,
		GetValue: func(cfg *configuration.Config) string {
			if field == "model" {
				return cfg.GetRole(role).Model
			}
			return cfg.GetRole(role).Provider
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			if field == "provider" && value == "test" {
				// Config.SetRole would silently drop the test provider;
				// surface it here so the set is reported as an error
				// instead of a no-op (same contract as Manager.SetProvider).
				return agenterrors.NewValidation(fmt.Sprintf("role %q provider: test provider cannot be persisted", role), nil)
			}
			// Read-modify-write: preserve the role's other stored field.
			rc := cfg.GetRole(role)
			if field == "model" {
				rc.Model = value
			} else {
				rc.Provider = value
			}
			cfg.SetRole(role, rc)
			return nil
		},
	}
}
