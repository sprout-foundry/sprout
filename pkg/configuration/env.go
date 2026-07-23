package configuration

import "github.com/sprout-foundry/sprout/pkg/envutil"

// GetEnvSimple checks SPROUT_* for a variable name suffix.
// E.g., GetEnvSimple("CONFIG") checks SPROUT_CONFIG.
func GetEnvSimple(suffix string) string {
	return envutil.GetEnvSimple(suffix)
}

// SetEnv sets the SPROUT_* version of an env var.
func SetEnv(suffix, value string) error {
	return envutil.SetEnv(suffix, value)
}

// LookupEnv checks SPROUT_*. Returns the value and whether it was found.
func LookupEnv(suffix string) (string, bool) {
	return envutil.LookupEnv(suffix)
}

// UnsetEnv removes the SPROUT_* version of an env var.
func UnsetEnv(suffix string) {
	envutil.UnsetEnv(suffix)
}
