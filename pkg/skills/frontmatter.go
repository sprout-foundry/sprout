package skills

import (
	"strings"
)

// SkillFrontmatter is the typed view of a SKILL.md YAML frontmatter block.
// Both Name and Description are required and non-empty for a skill to be
// considered installable.
type SkillFrontmatter struct {
	Name        string
	Description string
}

// ValidateFrontmatter ensures frontmatter is present and required fields are populated.
// Returns ErrInvalidFrontmatter (a sentinel error declared in install.go) when invalid.
func ValidateFrontmatter(fm SkillFrontmatter) error {
	if strings.TrimSpace(fm.Name) == "" {
		return ErrInvalidFrontmatter
	}
	if strings.TrimSpace(fm.Description) == "" {
		return ErrInvalidFrontmatter
	}
	return nil
}

// parseSkillFrontmatter wraps parseFrontmatter to produce a typed SkillFrontmatter.
func parseSkillFrontmatter(content string) (SkillFrontmatter, error) {
	raw, err := parseFrontmatter(content)
	if err != nil {
		return SkillFrontmatter{}, err
	}
	fm := SkillFrontmatter{
		Name:        strings.TrimSpace(raw["name"]),
		Description: strings.TrimSpace(raw["description"]),
	}
	if err := ValidateFrontmatter(fm); err != nil {
		return SkillFrontmatter{}, err
	}
	return fm, nil
}
