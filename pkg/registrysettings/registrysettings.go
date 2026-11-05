// Package registrysettings parses the environment overrides shared by the
// model and provider registries.
//
// Both registries expose the same four knobs under their own prefix
// (`MODEL_REGISTRY_*`, `PROVIDER_REGISTRY_*`): the base URL, a cache TTL, a
// negative-cache TTL and an HTTP timeout. The parsing rules are identical —
// an unset or unparsable value keeps the caller's default, and a URL of
// "off"/"none"/"disabled" disables the registry — so they live here rather
// than being copied into each package and drifting apart.
package registrysettings

import (
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// Settings is the resolved environment configuration for one registry.
type Settings struct {
	// BaseURL is the registry root with any trailing slash removed. It is
	// empty when the registry is disabled.
	BaseURL string
	// TTL, NegativeTTL and Timeout hold the caller's defaults unless the
	// environment supplied a positive, parsable duration.
	TTL         time.Duration
	NegativeTTL time.Duration
	Timeout     time.Duration
}

// Load resolves the settings for the registry whose environment variables use
// prefix (e.g. "MODEL_REGISTRY"). The defaults are returned for every knob the
// environment does not override.
func Load(prefix string, defaults Settings) Settings {
	s := defaults

	if v := strings.TrimSpace(envutil.GetEnvSimple(prefix + "_URL")); v != "" {
		switch strings.ToLower(v) {
		case "off", "none", "disabled":
			s.BaseURL = ""
		default:
			s.BaseURL = strings.TrimRight(v, "/")
		}
	}

	s.TTL = durationOr(prefix+"_TTL", s.TTL)
	s.NegativeTTL = durationOr(prefix+"_NEGATIVE_TTL", s.NegativeTTL)
	s.Timeout = durationOr(prefix+"_TIMEOUT", s.Timeout)

	return s
}

// durationOr returns the duration in the named variable when it is set,
// parsable and positive; otherwise it returns fallback.
func durationOr(name string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(envutil.GetEnvSimple(name))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
