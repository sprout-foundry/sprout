package configuration

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextProfileZeroValueHasFullModeLeverDefaults(t *testing.T) {
	var profile ContextProfile

	assert.Empty(t, profile.Mode, "an unresolved zero-value profile need not name the full preset")
	assert.Empty(t, profile.ToolAllowlist)
	assert.Empty(t, profile.SystemPromptPath)
	assert.False(t, profile.SkipProactiveContext)
	assert.Zero(t, profile.CompactionTriggerFraction)
	assert.Zero(t, profile.RecentTurnsToPreserve)
	assert.Zero(t, profile.RepoMapDefaultDepth)
}

func TestResolveContextProfileReturnsExactLowContextPreset(t *testing.T) {
	profile, err := ResolveContextProfile(
		&Config{ContextMode: ContextModeLowContext},
		128_000,
	)
	require.NoError(t, err)

	assert.Equal(t, ContextProfile{
		Mode: ContextModeLowContext,
		ToolAllowlist: []string{
			"shell_command",
			"read_file",
			"write_file",
			"edit_file",
			"search_files",
			"commit",
			"list_changes",
			"recover_file",
		},
		SystemPromptPath:          "prompts/system_prompt.lite.md",
		SkipProactiveContext:      true,
		CompactionTriggerFraction: 0.85,
		RecentTurnsToPreserve:     2,
		RepoMapDefaultDepth:       1,
	}, profile)
}

func TestResolveContextProfileResolutionPrecedence(t *testing.T) {
	tests := []struct {
		name               string
		cfg                *Config
		modelContextWindow int
		wantMode           ContextMode
	}{
		{
			name:               "nil config and zero unknown window default to full",
			modelContextWindow: 0,
			wantMode:           ContextModeFull,
		},
		{
			name:               "nil config and negative unknown window default to full",
			modelContextWindow: -1,
			wantMode:           ContextModeFull,
		},
		{
			name:               "nil config and threshold window use full",
			modelContextWindow: 64_000,
			wantMode:           ContextModeFull,
		},
		{
			name:               "nil config and representative high window use full",
			modelContextWindow: 128_000,
			wantMode:           ContextModeFull,
		},
		{
			name:               "floor boundary auto-detects low context",
			modelContextWindow: 8_000,
			wantMode:           ContextModeLowContext,
		},
		{
			name:               "16K window auto-detects low context",
			modelContextWindow: 16_000,
			wantMode:           ContextModeLowContext,
		},
		{
			name:               "32K window auto-detects low context",
			modelContextWindow: 32_000,
			wantMode:           ContextModeLowContext,
		},
		{
			name:               "one below full threshold auto-detects low context",
			modelContextWindow: 63_999,
			wantMode:           ContextModeLowContext,
		},
		{
			name:               "explicit low overrides high window",
			cfg:                &Config{ContextMode: ContextModeLowContext},
			modelContextWindow: 128_000,
			wantMode:           ContextModeLowContext,
		},
		{
			name:               "explicit low overrides zero unknown window",
			cfg:                &Config{ContextMode: ContextModeLowContext},
			modelContextWindow: 0,
			wantMode:           ContextModeLowContext,
		},
		{
			name:               "explicit low overrides negative unknown window",
			cfg:                &Config{ContextMode: ContextModeLowContext},
			modelContextWindow: -1,
			wantMode:           ContextModeLowContext,
		},
		{
			name:               "explicit full overrides floor boundary",
			cfg:                &Config{ContextMode: ContextModeFull},
			modelContextWindow: 8_000,
			wantMode:           ContextModeFull,
		},
		{
			name:               "explicit full overrides representative low window",
			cfg:                &Config{ContextMode: ContextModeFull},
			modelContextWindow: 32_000,
			wantMode:           ContextModeFull,
		},
		{
			name:               "explicit full overrides one below full threshold",
			cfg:                &Config{ContextMode: ContextModeFull},
			modelContextWindow: 63_999,
			wantMode:           ContextModeFull,
		},
		{
			name:               "empty mode falls through to low auto-detection",
			cfg:                &Config{ContextMode: ""},
			modelContextWindow: 32_000,
			wantMode:           ContextModeLowContext,
		},
		{
			name:               "empty mode falls through to full default",
			cfg:                &Config{ContextMode: ""},
			modelContextWindow: 128_000,
			wantMode:           ContextModeFull,
		},
		{
			name:               "unknown mode falls through to low auto-detection",
			cfg:                &Config{ContextMode: ContextMode("typo")},
			modelContextWindow: 32_000,
			wantMode:           ContextModeLowContext,
		},
		{
			name:               "unknown mode falls through to full default",
			cfg:                &Config{ContextMode: ContextMode("typo")},
			modelContextWindow: 128_000,
			wantMode:           ContextModeFull,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, err := ResolveContextProfile(tt.cfg, tt.modelContextWindow)
			require.NoError(t, err)

			switch tt.wantMode {
			case ContextModeFull:
				assertFullContextProfile(t, profile)
			case ContextModeLowContext:
				assertLowContextProfile(t, profile)
			default:
				t.Fatalf("test has unsupported expected mode %q", tt.wantMode)
			}
		})
	}
}

func TestResolveContextProfileRejectsWindowsBelowHardFloor(t *testing.T) {
	tests := []struct {
		name               string
		cfg                *Config
		modelContextWindow int
	}{
		{
			name:               "4096 with nil config",
			modelContextWindow: 4_096,
		},
		{
			name:               "one below floor with nil config",
			modelContextWindow: 7_999,
		},
		{
			name:               "explicit low cannot override hard floor",
			cfg:                &Config{ContextMode: ContextModeLowContext},
			modelContextWindow: 4_096,
		},
		{
			name:               "explicit full cannot override hard floor",
			cfg:                &Config{ContextMode: ContextModeFull},
			modelContextWindow: 7_999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, err := ResolveContextProfile(tt.cfg, tt.modelContextWindow)
			require.Error(t, err)

			message := err.Error()
			assert.Contains(t, message, strconv.Itoa(tt.modelContextWindow))
			assert.Contains(t, message, "8000-token minimum")
			assert.Contains(t, message, "Low-Context Mode")
			assert.Contains(t, message, "lite prompt")
			assert.Contains(t, message, "Switch to a larger-context model")
			assert.Contains(t, message, "raise the model's context limit")
			assertFullContextProfile(t, profile)
		})
	}
}

func assertFullContextProfile(t *testing.T, profile ContextProfile) {
	t.Helper()
	assert.Equal(t, ContextProfile{Mode: ContextModeFull}, profile)
}

func assertLowContextProfile(t *testing.T, profile ContextProfile) {
	t.Helper()
	assert.Equal(t, ContextProfile{
		Mode: ContextModeLowContext,
		ToolAllowlist: []string{
			"shell_command",
			"read_file",
			"write_file",
			"edit_file",
			"search_files",
			"commit",
			"list_changes",
			"recover_file",
		},
		SystemPromptPath:          "prompts/system_prompt.lite.md",
		SkipProactiveContext:      true,
		CompactionTriggerFraction: 0.85,
		RecentTurnsToPreserve:     2,
		RepoMapDefaultDepth:       1,
	}, profile)
}

// TestResolveEffectiveContextCap tests the SP-126 context cap resolver.
func TestResolveEffectiveContextCap(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *Config
		nativeWindow   int
		expected       int
		expectedErr    bool
		description    string
	}{
		{
			name:         "no cap - native flows through",
			cfg:          nil,
			nativeWindow: 1_000_000,
			expected:     1_000_000,
			description:  "no cap → native flows through",
		},
		{
			name:         "cap below native - cap applies",
			cfg:          &Config{MaxContextTokens: ptr(300_000)},
			nativeWindow: 1_000_000,
			expected:     300_000,
			description:  "cap < native → cap",
		},
		{
			name:         "cap above native - native applies",
			cfg:          &Config{MaxContextTokens: ptr(2_000_000)},
			nativeWindow: 1_000_000,
			expected:     1_000_000,
			description:  "cap > native → native (cap is no-op)",
		},
		{
			name:         "cap equals native - native applies",
			cfg:          &Config{MaxContextTokens: ptr(1_000_000)},
			nativeWindow: 1_000_000,
			expected:     1_000_000,
			description:  "cap == native → native",
		},
		{
			name:         "native unknown - cap applies",
			cfg:          &Config{MaxContextTokens: ptr(300_000)},
			nativeWindow: 0,
			expected:     300_000,
			description:  "native unknown, cap set → cap",
		},
		{
			name:         "neither known - returns zero",
			cfg:          nil,
			nativeWindow: 0,
			expected:     0,
			description:  "neither known → 0",
		},
		{
			name:         "nil cfg with known native - native applies",
			cfg:          nil,
			nativeWindow: 128_000,
			expected:     128_000,
			description:  "nil cfg (no config manager) → native",
		},
		{
			name:         "negative native - cap applies defensively",
			cfg:          &Config{MaxContextTokens: ptr(50_000)},
			nativeWindow: -1,
			expected:     50_000,
			description:  "negative native → cap (defensive)",
		},
		{
			name:         "tiny cap rejected by resolver",
			cfg:          &Config{MaxContextTokens: ptr(100)},
			nativeWindow: 1_000_000,
			expected:     0,
			expectedErr:  true,
			description:  "cap below minimum triggers error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveEffectiveContextCap(tt.cfg, tt.nativeWindow)
			if tt.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, got, tt.description)
			}
		})
	}
}

// ptr returns a pointer to the given value, for use in test tables.
func ptr[T any](v T) *T {
	return &v
}
