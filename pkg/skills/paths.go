package skills

import (
	"os"
	"path/filepath"
)

// DefaultSkillsDir returns the canonical skills install dir.
// Honors SPROUT_SKILLS_DIR env override; otherwise returns
// <os.UserConfigDir>/sprout/skills (creating the parent sprout dir if needed).
func DefaultSkillsDir() (string, error) {
	if env := os.Getenv("SPROUT_SKILLS_DIR"); env != "" {
		return env, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "sprout", "skills"), nil
}

// SkillInstallDir returns DefaultSkillsDir()/<skillID>.
// Errors from DefaultSkillsDir are returned so callers cannot silently
// fall back to a CWD-relative path.
func SkillInstallDir(skillID string) (string, error) {
	dir, err := DefaultSkillsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, skillID), nil
}
