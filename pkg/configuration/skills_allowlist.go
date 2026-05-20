package configuration

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const allowedSkillsFile = "allowed_skills"

// ReadAllowedSkills reads the .sprout/allowed_skills file from the given
// project root (cwd). Returns a set of allowed skill IDs. If the file does
// not exist, returns nil (meaning all skills are allowed).
func ReadAllowedSkills(projectRoot string) map[string]bool {
	path := filepath.Join(projectRoot, ".sprout", allowedSkillsFile)
	f, err := os.Open(path)
	if err != nil {
		return nil // file missing → all skills allowed
	}
	defer f.Close()

	allowed := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			allowed[line] = true
		}
	}
	return allowed
}

// WriteAllowedSkills writes the .sprout/allowed_skills file in the given
// project root. IDs are sorted for deterministic output.
func WriteAllowedSkills(projectRoot string, ids []string) error {
	dir := filepath.Join(projectRoot, ".sprout")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create .sprout dir: %w", err)
	}

	sorted := make([]string, len(ids))
	copy(sorted, ids)
	sort.Strings(sorted)

	path := filepath.Join(dir, allowedSkillsFile)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	for _, id := range sorted {
		fmt.Fprintln(f, id)
	}
	return nil
}

// AllowedSkillsPath returns the full path to the allowed_skills file for a
// given project root.
func AllowedSkillsPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".sprout", allowedSkillsFile)
}
