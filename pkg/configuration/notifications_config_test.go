package configuration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// NotificationsConfig.Resolve()
// ---------------------------------------------------------------------------

func TestNotificationsConfigResolve_Defaults(t *testing.T) {
	cfg := &NotificationsConfig{}
	result := cfg.Resolve()

	assert.Equal(t, false, result.CLIBell)
	assert.Equal(t, false, result.OSNotify)
	assert.Equal(t, false, result.Browser)
	assert.Equal(t, float64(10), result.MinSeconds)
}

func TestNotificationsConfigResolve_NilInput(t *testing.T) {
	var cfg *NotificationsConfig
	result := cfg.Resolve()

	assert.Equal(t, false, result.CLIBell)
	assert.Equal(t, false, result.OSNotify)
	assert.Equal(t, false, result.Browser)
	assert.Equal(t, float64(10), result.MinSeconds)
}

func TestNotificationsConfigResolve_UserValues(t *testing.T) {
	cfg := &NotificationsConfig{
		CLIBell:    true,
		OSNotify:   true,
		Browser:    true,
		MinSeconds: 30,
	}
	result := cfg.Resolve()

	assert.Equal(t, true, result.CLIBell)
	assert.Equal(t, true, result.OSNotify)
	assert.Equal(t, true, result.Browser)
	assert.Equal(t, float64(30), result.MinSeconds)
}

func TestNotificationsConfigResolve_PartialUserValues(t *testing.T) {
	// Only OSNotify and MinSeconds are set; CLIBell and Browser should stay
	// false, and MinSeconds should override the default.
	cfg := &NotificationsConfig{
		OSNotify:   true,
		MinSeconds: 0, // explicitly zero — should fall back to default
	}
	result := cfg.Resolve()

	assert.Equal(t, false, result.CLIBell)
	assert.Equal(t, true, result.OSNotify)
	assert.Equal(t, false, result.Browser)
	assert.Equal(t, float64(10), result.MinSeconds)
}

func TestNotificationsConfigResolve_PartialOnlyMinSeconds(t *testing.T) {
	cfg := &NotificationsConfig{
		MinSeconds: 5,
	}
	result := cfg.Resolve()

	assert.Equal(t, false, result.CLIBell)
	assert.Equal(t, false, result.OSNotify)
	assert.Equal(t, false, result.Browser)
	assert.Equal(t, float64(5), result.MinSeconds)
}

// ---------------------------------------------------------------------------
// JSON serialization
// ---------------------------------------------------------------------------

func TestNotificationsConfig_JSONSerialization(t *testing.T) {
	cfg := &NotificationsConfig{
		CLIBell:    true,
		OSNotify:   true,
		Browser:    true,
		MinSeconds: 30,
	}

	data, err := json.Marshal(cfg)
	assert.NoError(t, err)

	var decoded NotificationsConfig
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, true, decoded.CLIBell)
	assert.Equal(t, true, decoded.OSNotify)
	assert.Equal(t, true, decoded.Browser)
	assert.Equal(t, float64(30), decoded.MinSeconds)
}

func TestNotificationsConfig_OMITEmpty(t *testing.T) {
	// Zero-value fields should be omitted from JSON output.
	cfg := &NotificationsConfig{}
	data, err := json.Marshal(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "{}", string(data))
}

func TestNotificationsConfig_OMITEmpty_PartialFields(t *testing.T) {
	// Only non-zero fields should appear.
	cfg := &NotificationsConfig{
		OSNotify:   true,
		MinSeconds: 30,
	}
	data, err := json.Marshal(cfg)
	assert.NoError(t, err)
	// Should only contain os_notify and min_seconds.
	assert.Contains(t, string(data), `"os_notify"`)
	assert.Contains(t, string(data), `"min_seconds"`)
	assert.NotContains(t, string(data), `"cli_bell"`)
	assert.NotContains(t, string(data), `"browser"`)
}

func TestNotificationsConfig_JSONRoundTrip_Empty(t *testing.T) {
	input := `{}`
	var cfg NotificationsConfig
	err := json.Unmarshal([]byte(input), &cfg)
	assert.NoError(t, err)
	result := cfg.Resolve()
	assert.Equal(t, float64(10), result.MinSeconds)
}

func TestNotificationsConfig_JSONRoundTrip_Full(t *testing.T) {
	input := `{"cli_bell": true, "os_notify": true, "browser": true, "min_seconds": 60}`
	var cfg NotificationsConfig
	err := json.Unmarshal([]byte(input), &cfg)
	assert.NoError(t, err)
	result := cfg.Resolve()
	assert.Equal(t, true, result.CLIBell)
	assert.Equal(t, true, result.OSNotify)
	assert.Equal(t, true, result.Browser)
	assert.Equal(t, float64(60), result.MinSeconds)
}

func TestNotificationsConfig_JSONRoundTrip_Partial(t *testing.T) {
	input := `{"os_notify": true}`
	var cfg NotificationsConfig
	err := json.Unmarshal([]byte(input), &cfg)
	assert.NoError(t, err)
	result := cfg.Resolve()
	assert.Equal(t, false, result.CLIBell)
	assert.Equal(t, true, result.OSNotify)
	assert.Equal(t, false, result.Browser)
	assert.Equal(t, float64(10), result.MinSeconds)
}
