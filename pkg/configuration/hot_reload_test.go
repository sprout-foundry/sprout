package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiscoverSkillsCombined verifies the extracted discoverSkills() helper
// finds both user-level and project-level skills in one call. This is the
// helper that both Load() and LoadConfigWithLayers() (and thus Reload())
// now use, so hot-reload picks up new skills.
func TestDiscoverSkillsCombined(t *testing.T) {
	tmp := t.TempDir()

	// Create a user-level skill under <tmp>/skills/
	userSkill := filepath.Join(tmp, "skills", "my-user-skill")
	require.NoError(t, os.MkdirAll(userSkill, 0o755))
	userBody := "---\nname: User Skill\ndescription: A user-level test skill.\n---\nbody"
	require.NoError(t, os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte(userBody), 0o644))

	// Point SPROUT_CONFIG at tmp so GetConfigDir resolves our temp dir.
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("SPROUT_CONFIG", tmp)

	cfg := &Config{}
	discovered := cfg.discoverSkills()

	assert.Contains(t, discovered, "User Skill",
		"discoverSkills should find user-level skills")
	assert.Contains(t, cfg.Skills, "my-user-skill",
		"Config.Skills should contain the discovered user skill")
	assert.Equal(t, "user", cfg.Skills["my-user-skill"].Metadata["source"],
		"discovered user skill should have source=user")
}

// TestLoadConfigWithLayersDiscoversSkills verifies that the layered-load
// path (used by Manager.Reload for hot-reload) runs skill discovery.
// Previously discovery only ran in Load(), so Reload() missed new skills.
func TestLoadConfigWithLayersDiscoversSkills(t *testing.T) {
	tmp := t.TempDir()

	// Write a minimal config.json
	configPath := filepath.Join(tmp, "config.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{"version": "2.0"}`), 0o644))

	// Create a user-level skill
	userSkill := filepath.Join(tmp, "skills", "hot-reload-skill")
	require.NoError(t, os.MkdirAll(userSkill, 0o755))
	skillBody := "---\nname: Hot Reload\ndescription: Should be discovered by layered load.\n---\nbody"
	require.NoError(t, os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte(skillBody), 0o644))

	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("SPROUT_CONFIG", tmp)

	cfg, err := LoadConfigWithLayers(configPath, "", "", tmp)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	skill, ok := cfg.Skills["hot-reload-skill"]
	assert.True(t, ok, "LoadConfigWithLayers should discover user skills")
	if ok {
		assert.Equal(t, "Hot Reload", skill.Name)
		assert.Equal(t, "user", skill.Metadata["source"])
	}
}

// TestReloadPicksUpNewSkills is the end-to-end hot-reload test: create a
// Manager, then drop a new skill on disk, then Reload() and verify the
// skill is now visible. This simulates the webui skill-install → agent
// RefreshSkills flow.
func TestReloadPicksUpNewSkills(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{"version": "2.0"}`), 0o644))

	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("SPROUT_CONFIG", tmp)

	mgr, err := NewManagerWithDir(tmp)
	require.NoError(t, err)

	// Initially no custom skills
	cfg := mgr.GetConfig()
	_, exists := cfg.Skills["fresh-skill"]
	assert.False(t, exists, "fresh-skill should not exist yet")

	// Drop a new skill on disk
	skillDir := filepath.Join(tmp, "skills", "fresh-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	skillBody := "---\nname: Fresh\nhdescription: Added after startup.\n---\nbody"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillBody), 0o644))

	// Reload — this is what Agent.RefreshSkills() calls
	require.NoError(t, mgr.Reload())

	// The new skill should now be visible
	cfg = mgr.GetConfig()
	skill, ok := cfg.Skills["fresh-skill"]
	assert.True(t, ok, "Reload should discover the new skill")
	if ok {
		assert.Equal(t, "Fresh", skill.Name)
	}
}
