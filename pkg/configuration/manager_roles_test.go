package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRolesTestManager creates a hermetic Manager rooted at a temp HOME so
// the test never touches the user's real config (same isolation pattern
// as TestSaveConfig_AppliesDeletionAndScalarUpdates).
func newRolesTestManager(t *testing.T) *Manager {
	t.Helper()
	t.Setenv("CI", "1")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SPROUT_CONFIG", "")
	m, err := NewManager()
	require.NoError(t, err)
	return m
}

// TestManagerSetRole_Persists verifies the round-trip: SetRole writes the
// roles section to the on-disk config, and a fresh Load sees it
// (SP-150 item 150.6).
func TestManagerSetRole_Persists(t *testing.T) {
	m := newRolesTestManager(t)

	require.NoError(t, m.SetRole(RolePlanner, RoleConfig{Provider: "zai", Model: "GLM-4.6"}))
	assert.Equal(t, RoleConfig{Provider: "zai", Model: "GLM-4.6"}, m.GetRole(RolePlanner))

	loaded, err := Load()
	require.NoError(t, err)
	assert.Equal(t, RoleConfig{Provider: "zai", Model: "GLM-4.6"}, loaded.GetRole(RolePlanner))

	// A second manager on the same home sees the persisted section too.
	m2, err := NewManager()
	require.NoError(t, err)
	assert.Equal(t, RoleConfig{Provider: "zai", Model: "GLM-4.6"}, m2.GetRole(RolePlanner))
}

// TestManagerGetRole_ReturnsStoredEntry verifies GetRole returns the raw
// stored entry without alias/conversation fallback, and zero when unset.
func TestManagerGetRole_ReturnsStoredEntry(t *testing.T) {
	m := newRolesTestManager(t)

	assert.Equal(t, RoleConfig{}, m.GetRole(RoleCoder), "unset role is zero")
	assert.Equal(t, RoleConfig{}, m.GetRole("unknown-role"), "unknown name is zero")

	require.NoError(t, m.SetRole(RoleCoder, RoleConfig{Provider: "openai", Model: "gpt-4"}))
	assert.Equal(t, RoleConfig{Provider: "openai", Model: "gpt-4"}, m.GetRole(RoleCoder))

	// A partial entry is stored as-is (no fallback resolution here).
	require.NoError(t, m.SetRole(RoleCommit, RoleConfig{Model: "gpt-4o"}))
	assert.Equal(t, RoleConfig{Model: "gpt-4o"}, m.GetRole(RoleCommit))
}

// TestManagerSetRole_TestProviderRejected verifies the defense-in-depth
// guard: the literal "test" provider is rejected with an error (the same
// contract as SetProvider) and is not persisted through the roles section.
func TestManagerSetRole_TestProviderRejected(t *testing.T) {
	m := newRolesTestManager(t)

	// SetRole rejects the test provider with an error and stores nothing.
	err := m.SetRole(RoleCoder, RoleConfig{Provider: "test", Model: "m"})
	require.Error(t, err)
	assert.Equal(t, RoleConfig{}, m.GetRole(RoleCoder))

	loaded, loadErr := Load()
	require.NoError(t, loadErr)
	assert.Equal(t, RoleConfig{}, loaded.GetRole(RoleCoder))

	// A real provider on the same role still persists.
	require.NoError(t, m.SetRole(RoleCoder, RoleConfig{Provider: "openai", Model: "gpt-4"}))
	assert.Equal(t, RoleConfig{Provider: "openai", Model: "gpt-4"}, m.GetRole(RoleCoder))
}
