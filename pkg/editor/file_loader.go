package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/utils"
)

func loadOriginalCode(filename string) (string, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fmt.Printf("File %s not found. Continuing without it.\n", filename)
		return "", nil
	}
	content, err := utils.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("error loading file %s: %w", filename, err)
	}
	return content, nil
}

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
				fileContent, err := utils.ReadFile(file)
				if err != nil {
					return "", err
				}
				content += fmt.Sprintf("\n--- Start of content from %s ---\n\n%s\n\n--- End of content from %s ---\n", file, fileContent, file)
			}
		}
	} else if strings.HasSuffix(path, "/**/*") {
		dirPath := strings.TrimSuffix(path, "/**/*")
		files, err := filepath.Glob(filepath.Join(dirPath, "**", "*"))
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
				fileContent, err := utils.ReadFile(file)
				if err != nil {
					return "", err
				}
				content += fmt.Sprintf("\n--- Start of content from %s ---\n\n%s\n\n--- End of content from %s ---\n", file, fileContent, file)
			}
		}
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
		content, err = utils.ReadFile(parts[0])
		if err != nil {
			return "", err
		}
		if len(parts) > 1 {
			lineNumbers := strings.Split(parts[1], "-")
			if len(lineNumbers) == 2 {
				startLine, _ := strconv.Atoi(lineNumbers[0])
				endLine, _ := strconv.Atoi(lineNumbers[1])
				lines := strings.Split(content, "\n")
				if startLine > 0 && endLine > 0 && endLine <= len(lines) && startLine < endLine {
					content = fmt.Sprintf("\n--- Start of partial content from %s ---\n\n%s\n\n--- End of partial content from %s ---\n", parts[0], strings.Join(lines[startLine-1:endLine-1], "\n"), parts[0])
				}
			}
		} else {
			content = fmt.Sprintf("\n--- Start of full content from %s ---\n\n%s\n\n--- End of full content from %s ---\n", path, content, path)
		}
	}
	return content, nil
}
