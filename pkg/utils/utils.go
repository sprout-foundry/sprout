package utils

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func GenerateRequestHash(instructions string) string {
	h := sha1.New()
	h.Write([]byte(instructions))
	return hex.EncodeToString(h.Sum(nil))
}

func GenerateFileRevisionHash(filename, code string) string {
	h := sha1.New()
	h.Write([]byte(filename + code))
	return hex.EncodeToString(h.Sum(nil))
}

func SaveFile(filename, content string) error {
	if strings.TrimSpace(content) == "" {
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(filename, []byte(content), 0644)
}

func ReadFile(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// LogUserPrompt saves the user's original prompt to a file in .ledit/prompts/
func LogUserPrompt(prompt string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting user home directory for prompt logging: %v\n", err)
		return
	}

	promptsDir := filepath.Join(homeDir, ".ledit", "prompts")
	if err := os.MkdirAll(promptsDir, os.ModePerm); err != nil {
		fmt.Printf("Error creating prompts directory %s: %v\n", promptsDir, err)
		return
	}

	timestamp := time.Now().UnixMilli()
	filename := filepath.Join(promptsDir, fmt.Sprintf("%d.txt", timestamp))

	if err := os.WriteFile(filename, []byte(prompt), 0644); err != nil {
		fmt.Printf("Error saving user prompt to %s: %v\n", filename, err)
	}
}
