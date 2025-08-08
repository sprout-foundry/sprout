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
			fmt.Printf("🗑️  Removing file: %s\n", filename)
			return os.Remove(filename)
		} else if os.IsNotExist(err) {
			// File does not exist, nothing to do
			return nil
		} else {
			// Other error checking file stat
			return fmt.Errorf("error checking file %s: %w", filename, err)
		}
	}

	// Notify user about file being written
	fmt.Printf("💾 Writing file: %s (%d bytes)\n", filename, len(content))

	// Ensure the directory exists
	dir := filepath.Dir(filename)
	if dir != "" {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("could not create directory %s: %w", dir, err)
		}
	}

	err := os.WriteFile(filename, []byte(content), 0644)
	if err != nil {
		fmt.Printf("   ❌ Failed to write file: %v\n", err)
	} else {
		fmt.Printf("   ✅ File written successfully\n")
	}
	return err
}

// ReadFile reads the content of a file.
func ReadFile(filename string) (string, error) {
	fmt.Printf("📖 Reading file: %s\n", filename)

	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("   ❌ Failed to read file: %v\n", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}

	fmt.Printf("   ✅ File read successfully (%d bytes)\n", len(content))
	return string(content), nil
}
