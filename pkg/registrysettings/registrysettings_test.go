package registrysettings

import (
	"testing"
	"time"
)

func TestLoadUsesDefaultsWhenEnvIsUnset(t *testing.T) {
	defaults := Settings{
		BaseURL:     "https://example.test",
		TTL:         5 * time.Minute,
		NegativeTTL: 30 * time.Second,
		Timeout:     500 * time.Millisecond,
	}
	if got := Load("TEST_REGISTRY_UNSET", defaults); got != defaults {
		t.Errorf("Load() = %+v, want the defaults %+v", got, defaults)
	}
}

func TestLoadReadsEachKnob(t *testing.T) {
	t.Setenv("SPROUT_TEST_REGISTRY_URL", "https://mirror.test/")
	t.Setenv("SPROUT_TEST_REGISTRY_TTL", "2m")
	t.Setenv("SPROUT_TEST_REGISTRY_NEGATIVE_TTL", "10s")
	t.Setenv("SPROUT_TEST_REGISTRY_TIMEOUT", "250ms")

	got := Load("TEST_REGISTRY", Settings{})
	want := Settings{
		BaseURL:     "https://mirror.test",
		TTL:         2 * time.Minute,
		NegativeTTL: 10 * time.Second,
		Timeout:     250 * time.Millisecond,
	}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadDisablesOnOffValues(t *testing.T) {
	for _, off := range []string{"off", "OFF", "none", "disabled", "Disabled"} {
		t.Run(off, func(t *testing.T) {
			t.Setenv("SPROUT_TEST_REGISTRY_OFF_URL", off)
			got := Load("TEST_REGISTRY_OFF", Settings{BaseURL: "https://example.test"})
			if got.BaseURL != "" {
				t.Errorf("BaseURL = %q, want empty for %q", got.BaseURL, off)
			}
		})
	}
}

// A bad duration must keep the default rather than zero the knob: a zero TTL
// would disable caching, and a zero timeout would fail every request.
func TestLoadIgnoresUnparsableAndNonPositiveDurations(t *testing.T) {
	defaults := Settings{
		TTL:         time.Minute,
		NegativeTTL: time.Second,
		Timeout:     time.Second,
	}
	for _, bad := range []string{"not-a-duration", "0s", "-5s"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("SPROUT_TEST_REGISTRY_BAD_TTL", bad)
			t.Setenv("SPROUT_TEST_REGISTRY_BAD_NEGATIVE_TTL", bad)
			t.Setenv("SPROUT_TEST_REGISTRY_BAD_TIMEOUT", bad)
			if got := Load("TEST_REGISTRY_BAD", defaults); got != defaults {
				t.Errorf("Load() = %+v, want the defaults %+v", got, defaults)
			}
		})
	}
}

// The two registries share this parser, so the prefix must fully isolate them.
func TestLoadPrefixesAreIndependent(t *testing.T) {
	t.Setenv("SPROUT_AAA_REGISTRY_URL", "https://a.test")
	t.Setenv("SPROUT_BBB_REGISTRY_URL", "https://b.test")

	if got := Load("AAA_REGISTRY", Settings{}); got.BaseURL != "https://a.test" {
		t.Errorf("AAA BaseURL = %q", got.BaseURL)
	}
	if got := Load("BBB_REGISTRY", Settings{}); got.BaseURL != "https://b.test" {
		t.Errorf("BBB BaseURL = %q", got.BaseURL)
	}
}
