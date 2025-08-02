package filesystem

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LoadOriginalCode loads the content of a file.
// This function is intended for loading the current state of a file before modification.
func LoadOriginalCode(filename string) (string, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fmt.Printf("File %s not found. Continuing without it.\n", filename)
		return "", nil
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("error loading file %s: %w", filename, err)
	}
	return string(content), nil
}

// LoadFileContent loads and returns the content of a file or directory,
// with support for loading specific line ranges and glob patterns.
func LoadFileContent(path string) (string, error) {
	var content string

	if strings.HasSuffix(path, "/*") {
		dirPath := strings.TrimSuffix(path, "/*")
		files, err := filepath.Glob(filepath.Join(dirPath, "*"))
		if err != nil {
			return "", err
		}
		for _, file := range files {
			if !strings.HasPrefix(filepath.Base(file), ".") { // Ignore hidden files
				fileInfo, err := os.Stat(file)
				if err != nil {
					return "", err
				}
				if fileInfo.IsDir() {
					fmt.Printf("Skipping directory %s\n", file)
					continue
				}
				fileContent, err := os.ReadFile(file)
				if err != nil {
					return "", err
				}
				content += fmt.Sprintf("\n--- Start of content from %s ---\n\n%s\n\n--- End of content from %s ---\n", file, string(fileContent), file)
			}
		}
	} else if strings.HasSuffix(path, "/**/*") {
		dirPath := strings.TrimSuffix(path, "/**/*")
		var contentBuilder strings.Builder
		walkErr := filepath.WalkDir(dirPath, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil // Is a hidden file, skip.
			}

			if !d.IsDir() {
				fileContent, readErr := os.ReadFile(p)
				if readErr != nil {
					return readErr
				}
				contentBuilder.WriteString(fmt.Sprintf("\n--- Start of content from %s ---\n\n%s\n\n--- End of content from %s ---\n", p, string(fileContent), p))
			}
			return nil
		})

		if walkErr != nil {
			return "", walkErr
		}
		content = contentBuilder.String()
	} else {
		parts := strings.Split(path, ":")
		fileInfo, err := os.Stat(parts[0])
		if err != nil {
			return "", err
		}
		if fileInfo.IsDir() {
			fmt.Printf("Skipping directory %s\n", parts[0])
			return "", nil
		}
		contentBytes, err := os.ReadFile(parts[0])
		if err != nil {
			return "", err
		}
		content = string(contentBytes)
		if len(parts) > 1 {
			lineNumbers := strings.Split(parts[1], "-")
			if len(lineNumbers) == 2 {
				startLine, _ := strconv.Atoi(lineNumbers[0])
				endLine, _ := strconv.Atoi(lineNumbers[1])
				lines := strings.Split(content, "\n")
				if startLine > 0 && endLine > 0 && endLine <= len(lines) && startLine <= endLine {
					content = fmt.Sprintf("\n--- Start of partial content from %s ---\n\n%s\n\n--- End of partial content from %s ---\n", parts[0], strings.Join(lines[startLine-1:endLine], "\n"), parts[0])
				} else {
					// If line numbers are invalid, return full content as per original logic
					content = fmt.Sprintf("\n--- Start of full content from %s ---\n\n%s\n\n--- End of full content from %s ---\n", parts[0], content, parts[0])
				}
			} else {
				// If lineNumbers is not 2 parts (e.g., "filename:1"), treat as full content
				content = fmt.Sprintf("\n--- Start of full content from %s ---\n\n%s\n\n--- End of full content from %s ---\n", path, content, path)
			}
		} else {
			content = fmt.Sprintf("\n--- Start of full content from %s ---\n\n%s\n\n--- End of full content from %s ---\n", path, content, path)
		}
	}
	return content, nil
}