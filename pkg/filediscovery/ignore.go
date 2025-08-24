package filediscovery

import (
	"bufio"
	"os"
	"path/filepath"

	ignore "github.com/sabhiram/go-gitignore"
)

// GetIgnoreRules reads ignore files (.gitignore, .ledit/.ignore) and returns a gitignore object.
func GetIgnoreRules(rootDir string) *ignore.GitIgnore {
	var allRules []string

	// Read .gitignore
	gitignorePath := filepath.Join(rootDir, ".gitignore")
	if rules, err := readIgnoreFile(gitignorePath); err == nil {
		allRules = append(allRules, rules...)
	}

	// Read .ledit/.ignore
	leditIgnorePath := filepath.Join(rootDir, ".ledit", ".ignore")
	if rules, err := readIgnoreFile(leditIgnorePath); err == nil {
		allRules = append(allRules, rules...)
	}

	if len(allRules) == 0 {
		return nil
	}

	// Create a gitignore object from the combined rules
	ignore := ignore.CompileIgnoreLines(allRules...)

	return ignore
}

// readIgnoreFile reads a single ignore file and returns its lines.
func readIgnoreFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
