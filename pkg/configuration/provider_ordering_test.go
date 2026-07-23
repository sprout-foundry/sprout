package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrderProvidersByUsage_EmptyAndNoCreds covers the simplest case: no
// config history, no credentials, no env vars. The helper must return the
// input slice unchanged (no shuffling, no dropping).
func TestOrderProvidersByUsage_EmptyAndNoCreds(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Make sure no env vars are leaking from the host.
	for _, p := range KnownProviderNames() {
		metadata, err := GetProviderAuthMetadata(p)
		if err != nil || metadata.EnvVar == "" {
			continue
		}
		t.Setenv(metadata.EnvVar, "")
	}

	cfg := NewConfig()
	cfg.ProviderModels = map[string]string{} // no history

	input := []string{"alpha", "bravo", "charlie"}
	got := orderProvidersByUsage(input, cfg)
	assert.Equal(t, input, got, "no history + no creds should be a no-op")
}

// TestOrderProvidersByUsage_OneProviderWithCreds: when only one of the
// input providers has credentials (env var set), it must move to the
// front. Without this reordering, that provider would be presented
// wherever KnownProviderNames() happened to list it — typically not
// first.
func TestOrderProvidersByUsage_OneProviderWithCreds(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	// Wipe every known env var so only OPENAI_API_KEY lights up below.
	for _, p := range KnownProviderNames() {
		metadata, err := GetProviderAuthMetadata(p)
		if err != nil || metadata.EnvVar == "" {
			continue
		}
		t.Setenv(metadata.EnvVar, "")
	}
	t.Setenv("OPENAI_API_KEY", "sk-test-12345")

	cfg := NewConfig()
	cfg.ProviderModels = map[string]string{}

	input := []string{"openrouter", "openai", "deepinfra"}
	got := orderProvidersByUsage(input, cfg)
	require.NotEmpty(t, got)
	assert.Equal(t, "openai", got[0], "openai has creds; should be first")
	// The rest stay in input order (they have no creds).
	assert.Equal(t, []string{"openrouter", "deepinfra"}, got[1:])
}

// TestOrderProvidersByUsage_Tier1BeatsTier2 covers the core regression:
// a provider the user has used before (ProviderModels entry) beats one
// that merely has credentials but no history. This is the user-asked-for
// rule — previously-used is a stronger signal than "configured".
func TestOrderProvidersByUsage_Tier1BeatsTier2(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	for _, p := range KnownProviderNames() {
		metadata, err := GetProviderAuthMetadata(p)
		if err != nil || metadata.EnvVar == "" {
			continue
		}
		t.Setenv(metadata.EnvVar, "")
	}
	t.Setenv("OPENAI_API_KEY", "sk-test-12345")
	t.Setenv("DEEPINFRA_API_KEY", "di-test-12345")

	cfg := NewConfig()
	cfg.ProviderModels = map[string]string{
		"deepinfra": "deepseek-ai/DeepSeek-V3.1-Terminus",
	}

	// Put deepinfra LAST in the input to confirm the helper reorders.
	input := []string{"openai", "openrouter", "deepinfra"}
	got := orderProvidersByUsage(input, cfg)
	require.Len(t, got, 3)
	assert.Equal(t, "deepinfra", got[0], "tier 1 (used-before) should beat tier 2 (creds only)")
	assert.Equal(t, "openai", got[1], "tier 2 providers keep input order")
}

// TestOrderProvidersByUsage_NoDuplicates: passing the same name twice
// must collapse to a single entry. The helper is fed a union of two
// sources in selectInitialProvider (envProviders and providersWithKeys)
// so duplicates are realistic.
func TestOrderProvidersByUsage_NoDuplicates(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	cfg := NewConfig()
	cfg.ProviderModels = map[string]string{}

	input := []string{"openai", "openrouter", "openai", "openrouter", "deepinfra"}
	got := orderProvidersByUsage(input, cfg)

	seen := make(map[string]int)
	for _, name := range got {
		seen[name]++
	}
	for name, count := range seen {
		assert.Equal(t, 1, count, "%s appeared %d times, want 1", name, count)
	}
	assert.Len(t, got, 3, "expected 3 unique providers")
}

// TestOrderProvidersByUsage_StableWithinTier: when two providers are in
// the same tier, the one earlier in the input list wins. This is what
// makes the helper's output predictable: reordering is purely by tier,
// never by alphabetical re-shuffling within a tier.
func TestOrderProvidersByUsage_StableWithinTier(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	cfg := NewConfig()
	cfg.ProviderModels = map[string]string{}

	// All three have no creds (tier 3) — input order must be preserved.
	input := []string{"zebra", "alpha", "mango"}
	got := orderProvidersByUsage(input, cfg)
	assert.Equal(t, input, got, "tier 3 must preserve input order")
}

// TestOrderProvidersByUsage_NilCfgSafe: the helper accepts a nil cfg
// for callers that haven't loaded config yet (e.g. GetAvailableProviders
// during a corrupted-config boot). Tier 1 collapses to tier 2 in that
// case, but it must not panic.
func TestOrderProvidersByUsage_NilCfgSafe(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	for _, p := range KnownProviderNames() {
		metadata, err := GetProviderAuthMetadata(p)
		if err != nil || metadata.EnvVar == "" {
			continue
		}
		t.Setenv(metadata.EnvVar, "")
	}
	t.Setenv("OPENAI_API_KEY", "sk-test-12345")

	got := orderProvidersByUsage([]string{"openrouter", "openai", "deepinfra"}, nil)
	require.NotEmpty(t, got)
	assert.Equal(t, "openai", got[0], "with nil cfg, openai (creds) should still lead")
}

// TestOrderProvidersByUsage_AllKnownProvidersReturned: nothing should
// fall out of the result. The helper is a reorder, not a filter.
func TestOrderProvidersByUsage_AllKnownProvidersReturned(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	cfg := NewConfig()
	cfg.ProviderModels = map[string]string{}

	input := KnownProviderNames()
	got := orderProvidersByUsage(input, cfg)
	assert.Len(t, got, len(input), "no providers should be dropped")
}

// TestInitialize_CIPrefersPreviouslyUsedProvider: with both OPENAI_API_KEY
// and DEEPINFRA_API_KEY set, and ProviderModels recording prior use of
// deepinfra, the Initialize() CI branch must pick deepinfra — NOT openai.
// This is the bug the user reported: launch defaulted to openrouter
// regardless of what was actually used.
func TestInitialize_CIPrefersPreviouslyUsedProvider(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("CI", "1")
	t.Setenv("GITHUB_ACTIONS", "")
	for _, p := range KnownProviderNames() {
		metadata, err := GetProviderAuthMetadata(p)
		if err != nil || metadata.EnvVar == "" {
			continue
		}
		t.Setenv(metadata.EnvVar, "")
	}
	t.Setenv("OPENAI_API_KEY", "sk-test-12345")
	t.Setenv("DEEPINFRA_API_KEY", "di-test-12345")

	// Pre-seed config with deepinfra as previously-used provider.
	cfg := NewConfig()
	cfg.LastUsedProvider = "" // force CI branch
	cfg.ProviderModels = map[string]string{
		"deepinfra": "deepseek-ai/DeepSeek-V3.1-Terminus",
	}
	require.NoError(t, cfg.Save())

	config, _, err := Initialize()
	require.NoError(t, err)
	assert.Equal(t, "deepinfra", config.LastUsedProvider,
		"CI mode with deepinfra previously-used must pick deepinfra, not openai (tier 1 beats tier 2)")
}

// TestInitialize_CIPicksFirstCredsWhenNoHistory: when no ProviderModels
// entry exists, the CI branch should still pick a credentialed provider
// (just not openrouter-by-default). This preserves the original "any
// real provider wins over no provider" behavior.
func TestInitialize_CIPicksFirstCredsWhenNoHistory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("CI", "1")
	t.Setenv("GITHUB_ACTIONS", "")
	for _, p := range KnownProviderNames() {
		metadata, err := GetProviderAuthMetadata(p)
		if err != nil || metadata.EnvVar == "" {
			continue
		}
		t.Setenv(metadata.EnvVar, "")
	}
	t.Setenv("OPENAI_API_KEY", "sk-test-12345")
	// OPENROUTER_API_KEY explicitly off so we know openai wins by creds,
	// not by accident.
	t.Setenv("OPENROUTER_API_KEY", "")

	cfg := NewConfig()
	cfg.LastUsedProvider = ""
	cfg.ProviderModels = map[string]string{}
	require.NoError(t, cfg.Save())

	config, _, err := Initialize()
	require.NoError(t, err)
	// openai is the only credentialed provider here (deepinfra env was
	// cleared above), so it must win.
	assert.Equal(t, "openai", config.LastUsedProvider,
		"CI mode with only OPENAI_API_KEY set must pick openai, not openrouter")
}

// TestGetAvailableProviders_ReordersByTier: confirm that
// GetAvailableProviders now applies tier-based reordering, not pure
// alphabetical sort. With a ProviderModels entry recording deepinfra
// usage, deepinfra should appear before openrouter in the result.
func TestGetAvailableProviders_ReordersByTier(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	for _, p := range KnownProviderNames() {
		metadata, err := GetProviderAuthMetadata(p)
		if err != nil || metadata.EnvVar == "" {
			continue
		}
		t.Setenv(metadata.EnvVar, "")
	}
	t.Setenv("DEEPINFRA_API_KEY", "di-test-12345")

	cfg := NewConfig()
	cfg.ProviderModels = map[string]string{
		"deepinfra": "deepseek-ai/DeepSeek-V3.1-Terminus",
	}
	require.NoError(t, cfg.Save())

	result := GetAvailableProviders()
	require.NotEmpty(t, result)

	// Find deepinfra and openrouter positions.
	var deepinfraIdx, openrouterIdx = -1, -1
	for i, p := range result {
		if p == "deepinfra" {
			deepinfraIdx = i
		}
		if p == "openrouter" {
			openrouterIdx = i
		}
	}
	require.NotEqual(t, -1, deepinfraIdx, "deepinfra should be in result")
	require.NotEqual(t, -1, openrouterIdx, "openrouter should be in result")
	assert.Less(t, deepinfraIdx, openrouterIdx,
		"deepinfra (tier 1, previously used + creds) should come before openrouter (tier 2/3)")
}
