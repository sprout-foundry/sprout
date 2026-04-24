package configuration

import "github.com/sprout-foundry/sprout/pkg/envutil"

// GetEnv checks for an environment variable using the Sprout (SPROUT_*) name first,
// falling back to the legacy Ledit (LEDIT_*) name.
//
// Convention: sproutKey should be the SPROUT_* equivalent, legacyKey the LEDIT_* original.
func GetEnv(sproutKey, legacyKey string) string {
	return envutil.GetEnv(sproutKey, legacyKey)
}

// GetEnvSimple checks SPROUT_* then LEDIT_* for a variable name suffix.
// E.g., GetEnvSimple("CONFIG") checks SPROUT_CONFIG then LEDIT_CONFIG.
func GetEnvSimple(suffix string) string {
	return envutil.GetEnvSimple(suffix)
}

// SetEnv sets both the SPROUT_* and LEDIT_* versions of an env var
// for backward compatibility during the transition period.
func SetEnv(suffix, value string) error {
	return envutil.SetEnv(suffix, value)
}

// LookupEnv checks SPROUT_* first, then LEDIT_*. Returns the value and whether it was found.
func LookupEnv(suffix string) (string, bool) {
	return envutil.LookupEnv(suffix)
}

// UnsetEnv removes both SPROUT_* and LEDIT_* versions of an env var.
func UnsetEnv(suffix string) {
	envutil.UnsetEnv(suffix)
}
