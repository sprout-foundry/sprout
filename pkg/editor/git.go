package editor

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func getGitRootDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out []byte
	var err error
	if out, err = cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not find git root: %v", string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func getFileGitPath(filename string) (string, error) {
	gitRoot, err := getGitRootDir()
	if err != nil {
		return filename, fmt.Errorf("failed to get git root directory: %w", err)
	}
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return filename, fmt.Errorf("failed to get absolute path for %s: %w", filename, err)
	}
	relPath, err := filepath.Rel(gitRoot, absPath)
	if err != nil {
		return filename, fmt.Errorf("failed to get relative path for %s: %w", filename, err)
	}
	return relPath, nil
}

func addAndCommitFile(newFilename, message string) error {
	if err := exec.Command("git", "add", newFilename).Run(); err != nil {
		return fmt.Errorf("error adding changes to git: %v", err)
	}
	if err := exec.Command("git", "commit", "-m", message).Run(); err != nil {
		return fmt.Errorf("error committing changes to git: %v", err)
	}
	fmt.Printf("Changes committed to git for %s\n", newFilename)
	return nil
}
