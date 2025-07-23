package utils

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"strings"
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
