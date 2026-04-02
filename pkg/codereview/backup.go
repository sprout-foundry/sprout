package codereview

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// createBackup creates a backup of a file before making changes
func (s *CodeReviewService) createBackup(filePath string) (string, error) {
	// Create backup directory if it doesn't exist
	backupDir := filepath.Join(".ledit", "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create backup filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := filepath.Base(filePath)
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s.backup", filename, timestamp))

	// Copy file to backup
	src, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file for backup: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("failed to copy file to backup: %w", err)
	}

	s.logger.LogProcessStep(fmt.Sprintf("Created backup: %s -> %s", filePath, backupPath))
	return backupPath, nil
}

// restoreFromBackup restores a file from its backup
func (s *CodeReviewService) restoreFromBackup(backupPath, originalPath string) error {
	// Check if backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file does not exist: %s", backupPath)
	}

	// Copy backup back to original location
	src, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(originalPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to restore from backup: %w", err)
	}

	s.logger.LogProcessStep(fmt.Sprintf("Restored from backup: %s -> %s", backupPath, originalPath))
	return nil
}

// listBackups lists available backups for a file
func (s *CodeReviewService) listBackups(filePath string) ([]string, error) {
	backupDir := filepath.Join(".ledit", "backups")
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return nil, nil // No backup directory exists
	}

	filename := filepath.Base(filePath)
	pattern := fmt.Sprintf("%s.*.backup", filename)

	matches, err := filepath.Glob(filepath.Join(backupDir, pattern))
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	return matches, nil
}

// cleanupOldBackups removes old backup files to prevent backup directory from growing too large
func (s *CodeReviewService) cleanupOldBackups(maxBackups int) error {
	backupDir := filepath.Join(".ledit", "backups")
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return nil // No backup directory exists
	}

	files, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Sort files by modification time (newest first)
	var backupFiles []os.DirEntry
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".backup") {
			backupFiles = append(backupFiles, file)
		}
	}

	// If we have more backups than allowed, remove the oldest ones
	if len(backupFiles) > maxBackups {
		toRemove := len(backupFiles) - maxBackups
		for i := len(backupFiles) - 1; i >= len(backupFiles)-toRemove; i-- {
			filePath := filepath.Join(backupDir, backupFiles[i].Name())
			if err := os.Remove(filePath); err != nil {
				s.logger.LogProcessStep(fmt.Sprintf("Warning: Failed to remove old backup %s: %v", filePath, err))
			} else {
				s.logger.LogProcessStep(fmt.Sprintf("Removed old backup: %s", filePath))
			}
		}
	}

	return nil
}
