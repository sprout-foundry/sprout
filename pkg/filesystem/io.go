package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
)

// SaveFile saves or removes a file with the given content.
// If content is empty, the file is removed.
func SaveFile(filename, content string) error {
	if content == "" {
		if _, err := os.Stat(filename); err == nil {
			// File exists, remove it
			return os.Remove(filename)
		} else if os.IsNotExist(err) {
			// File does not exist, nothing to do
			return nil
		} else {
			// Other error checking file stat
			return fmt.Errorf("error checking file %s: %w", filename, err)
		}
	}

	// Ensure the directory exists
	dir := filepath.Dir(filename)
	if dir != "" {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("could not create directory %s: %w", dir, err)
		}
	}

	return os.WriteFile(filename, []byte(content), 0644)
}

// ReadFile reads the content of a file.
func ReadFile(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}
	return string(content), nil
}