//go:build !js

package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
)

var (
	automateLogsFollow bool
	automateLogsLines  int
)

func runAutomateLogs(sessionID string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	sproutDir := filepath.Join(cwd, ".sprout")

	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	if err != nil {
		return err
	}

	if info.OutputFilePath == "" {
		fmt.Printf("No captured output for session %s (CLI sessions pipe to terminal)\n", sessionID)
		return nil
	}

	data, err := os.ReadFile(info.OutputFilePath)
	if err != nil {
		return fmt.Errorf("read output file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	// Remove trailing empty element from trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if automateLogsLines > 0 && automateLogsLines < len(lines) {
		lines = lines[len(lines)-automateLogsLines:]
	}

	for _, line := range lines {
		fmt.Println(line)
	}

	if automateLogsFollow {
		return followLogFile(info.OutputFilePath, info.PID)
	}
	return nil
}

func followLogFile(path string, pid int) error {
	// Get the initial file size
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat output file: %w", err)
	}
	offset := fi.Size()

	for {
		if !automate.IsProcessAlive(pid) {
			// Process is gone — do one final read then exit
			break
		}
		time.Sleep(500 * time.Millisecond)

		// Check if the file was truncated (e.g. log rotation).
		fi, err := os.Stat(path)
		if err == nil && fi.Size() < offset {
			offset = 0 // file was truncated, restart from beginning
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}

		_, err = f.Seek(offset, io.SeekStart)
		if err != nil {
			f.Close()
			continue
		}

		newData, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}

		if len(newData) > 0 {
			fmt.Print(string(newData))
			offset += int64(len(newData))
		}
	}

	// Final read after process exits
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	_, err = f.Seek(offset, io.SeekStart)
	if err != nil {
		return nil
	}

	newData, err := io.ReadAll(f)
	if err != nil || len(newData) == 0 {
		return nil
	}
	fmt.Print(string(newData))
	return nil
}
