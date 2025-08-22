package editor

import (
	"fmt"
	"os"
	"os/exec"
)

// OpenInEditor opens the provided content in the user's default editor (or vim)
// and returns the edited content.
func OpenInEditor(content, fileExtension string) (string, error) {
	tempFile, err := os.CreateTemp("", "ledit-*"+fileExtension)
	if err != nil {
		return "", fmt.Errorf("could not create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())

	if _, err := tempFile.WriteString(content); err != nil {
		return "", fmt.Errorf("could not write to temp file: %w", err)
	}
	tempFile.Close()

	editorPath := os.Getenv("EDITOR")
	if editorPath == "" {
		editorPath = "vim" // A reasonable default
	}
	cmd := exec.Command(editorPath, tempFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error running editor: %w", err)
	}

	editedContent, err := os.ReadFile(tempFile.Name())
	if err != nil {
		return "", fmt.Errorf("could not read edited file: %w", err)
	}
	return string(editedContent), nil
}
