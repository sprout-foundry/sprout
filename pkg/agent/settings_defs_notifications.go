package agent

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// settings_defs_notifications.go — the notifications and API-timeout sections
// of the settingDefs registry. Split out of settings_defs.go.
var settingDefsNotifications = []settingDef{
	// --- Notifications ---
	{
		Key:         "notifications.cli_bell",
		Description: "Terminal bell on completion",
		ValidValues: "true, false",
		GetValue: func(cfg *configuration.Config) string {
			if cfg.Notifications != nil {
				return fmt.Sprintf("%v", cfg.Notifications.CLIBell)
			}
			return "false"
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				if cfg.Notifications == nil {
					cfg.Notifications = &configuration.NotificationsConfig{}
				}
				cfg.Notifications.CLIBell = true
				return nil
			case "false":
				if cfg.Notifications == nil {
					cfg.Notifications = &configuration.NotificationsConfig{}
				}
				cfg.Notifications.CLIBell = false
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("notifications.cli_bell must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"true", "false"},
	},
	{
		Key:         "notifications.os_notify",
		Description: "OS desktop notification on completion",
		ValidValues: "true, false",
		GetValue: func(cfg *configuration.Config) string {
			if cfg.Notifications != nil {
				return fmt.Sprintf("%v", cfg.Notifications.OSNotify)
			}
			return "false"
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				if cfg.Notifications == nil {
					cfg.Notifications = &configuration.NotificationsConfig{}
				}
				cfg.Notifications.OSNotify = true
				return nil
			case "false":
				if cfg.Notifications == nil {
					cfg.Notifications = &configuration.NotificationsConfig{}
				}
				cfg.Notifications.OSNotify = false
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("notifications.os_notify must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"true", "false"},
	},
	{
		Key:         "notifications.min_seconds",
		Description: "Min turn duration before notification (seconds)",
		ValidValues: "0-300 (supports fractional seconds)",
		GetValue: func(cfg *configuration.Config) string {
			if cfg.Notifications != nil {
				return fmt.Sprintf("%v", cfg.Notifications.MinSeconds)
			}
			return ""
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			val, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return agenterrors.NewValidation(fmt.Sprintf("notifications.min_seconds must be a number, got %q", value), nil)
			}
			if val < 0 || val > 300 {
				return agenterrors.NewValidation(fmt.Sprintf("notifications.min_seconds must be between 0 and 300, got %v", val), nil)
			}
			if cfg.Notifications == nil {
				cfg.Notifications = &configuration.NotificationsConfig{}
			}
			cfg.Notifications.MinSeconds = val
			return nil
		},
	},
	// --- API Timeouts ---
	{
		Key:         "api_timeouts.overall_timeout_sec",
		Description: "Overall API timeout (seconds)",
		ValidValues: "60-3600",
		GetValue: func(cfg *configuration.Config) string {
			if cfg.APITimeouts != nil {
				return strconv.Itoa(cfg.APITimeouts.OverallTimeoutSec)
			}
			return ""
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			val, err := strconv.Atoi(value)
			if err != nil {
				return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.overall_timeout_sec must be an integer, got %q", value), nil)
			}
			if val < 60 || val > 3600 {
				return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.overall_timeout_sec must be between 60 and 3600, got %d", val), nil)
			}
			if cfg.APITimeouts == nil {
				cfg.APITimeouts = &configuration.APITimeoutConfig{}
			}
			cfg.APITimeouts.OverallTimeoutSec = val
			return nil
		},
	},
	{
		Key:         "api_timeouts.connection_timeout_sec",
		Description: "Connection timeout (seconds)",
		ValidValues: "10-600",
		GetValue: func(cfg *configuration.Config) string {
			if cfg.APITimeouts != nil {
				return strconv.Itoa(cfg.APITimeouts.ConnectionTimeoutSec)
			}
			return ""
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			val, err := strconv.Atoi(value)
			if err != nil {
				return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.connection_timeout_sec must be an integer, got %q", value), nil)
			}
			if val < 10 || val > 600 {
				return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.connection_timeout_sec must be between 10 and 600, got %d", val), nil)
			}
			if cfg.APITimeouts == nil {
				cfg.APITimeouts = &configuration.APITimeoutConfig{}
			}
			cfg.APITimeouts.ConnectionTimeoutSec = val
			return nil
		},
	},
	{
		Key:         "api_timeouts.first_chunk_timeout_sec",
		Description: "First chunk timeout (seconds)",
		ValidValues: "30-1200",
		GetValue: func(cfg *configuration.Config) string {
			if cfg.APITimeouts != nil {
				return strconv.Itoa(cfg.APITimeouts.FirstChunkTimeoutSec)
			}
			return ""
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			val, err := strconv.Atoi(value)
			if err != nil {
				return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.first_chunk_timeout_sec must be an integer, got %q", value), nil)
			}
			if val < 30 || val > 1200 {
				return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.first_chunk_timeout_sec must be between 30 and 1200, got %d", val), nil)
			}
			if cfg.APITimeouts == nil {
				cfg.APITimeouts = &configuration.APITimeoutConfig{}
			}
			cfg.APITimeouts.FirstChunkTimeoutSec = val
			return nil
		},
	},
}
