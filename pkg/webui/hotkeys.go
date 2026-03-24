package webui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// HotkeyEntry represents a single hotkey binding
type HotkeyEntry struct {
	Key         string `json:"key"`
	CommandID   string `json:"command_id"`
	Description string `json:"description,omitempty"`
}

// HotkeyConfig represents the complete hotkey configuration
type HotkeyConfig struct {
	Version string        `json:"version"`
	Hotkeys []HotkeyEntry `json:"hotkeys"`
}

// DefaultHotkeyConfig returns the default hotkey configuration
func DefaultHotkeyConfig() *HotkeyConfig {
	return &HotkeyConfig{
		Version: "1.0",
		Hotkeys: []HotkeyEntry{
			{
				Key:         "Ctrl+Shift+P",
				CommandID:   "command_palette",
				Description: "Toggle command palette",
			},
			{
				Key:         "Cmd+Shift+P",
				CommandID:   "command_palette",
				Description: "Toggle command palette (Mac)",
			},
			{
			 Key:         "Ctrl+P",
				CommandID:   "quick_open",
				Description: "Quick open file",
			},
			{
				Key:         "Cmd+P",
				CommandID:   "quick_open",
				Description: "Quick open file (Mac)",
			},
			{
				Key:         "Ctrl+Shift+E",
				CommandID:   "toggle_explorer",
				Description: "Toggle file explorer",
			},
			{
				Key:         "Cmd+Shift+E",
				CommandID:   "toggle_explorer",
				Description: "Toggle file explorer (Mac)",
			},
			{
				Key:         "Ctrl+B",
				CommandID:   "toggle_sidebar",
				Description: "Toggle sidebar",
			},
			{
				Key:         "Cmd+B",
				CommandID:   "toggle_sidebar",
				Description: "Toggle sidebar (Mac)",
			},
			{
				Key:         "Ctrl+`",
				CommandID:   "toggle_terminal",
				Description: "Toggle terminal",
			},
			{
				Key:         "Cmd+`",
				CommandID:   "toggle_terminal",
				Description: "Toggle terminal (Mac)",
			},
			{
				Key:         "Ctrl+W",
				CommandID:   "close_editor",
				Description: "Close editor tab",
			},
			{
				Key:         "Cmd+W",
				CommandID:   "close_editor",
				Description: "Close editor tab (Mac)",
			},
			{
				Key:         "Ctrl+S",
				CommandID:   "save_file",
				Description: "Save file",
			},
			{
				Key:         "Cmd+S",
				CommandID:   "save_file",
				Description: "Save file (Mac)",
			},
			{
				Key:         "Ctrl+Shift+S",
				CommandID:   "save_all_files",
				Description: "Save all files",
			},
			{
				Key:         "Cmd+Shift+S",
				CommandID:   "save_all_files",
				Description: "Save all files (Mac)",
			},
			{
				Key:         "Ctrl+N",
				CommandID:   "new_file",
				Description: "New file",
			},
			{
				Key:         "Cmd+N",
				CommandID:   "new_file",
				Description: "New file (Mac)",
			},
			{
				Key:         "Ctrl+\\",
				CommandID:   "split_editor_vertical",
				Description: "Split editor vertical",
			},
		},
	}
}

// GetHotkeysPath returns the path to the hotkeys configuration file
func GetHotkeysPath() (string, error) {
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "hotkeys.json"), nil
}

// LoadHotkeys loads the hotkeys configuration from file
// If the file doesn't exist, returns the default configuration
func LoadHotkeys() (*HotkeyConfig, error) {
	path, err := GetHotkeysPath()
	if err != nil {
		return DefaultHotkeyConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultHotkeyConfig(), nil
		}
		return nil, fmt.Errorf("failed to read hotkeys file: %w", err)
	}

	var config HotkeyConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse hotkeys file: %w", err)
	}

	return &config, nil
}

// SaveHotkeys saves the hotkeys configuration to file
func SaveHotkeys(config *HotkeyConfig) error {
	path, err := GetHotkeysPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal hotkeys config: %w", err)
	}

	// Ensure config directory exists
	configDir := filepath.Dir(path)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write hotkeys file: %w", err)
	}

	return nil
}

// ValidateHotkeyConfig validates a hotkeys configuration
func ValidateHotkeyConfig(config *HotkeyConfig) error {
	if config == nil {
		return fmt.Errorf("hotkeys config is nil")
	}

	if config.Version == "" {
		return fmt.Errorf("hotkeys config must have a version field")
	}

	if config.Hotkeys == nil {
		return fmt.Errorf("hotkeys config must have a hotkeys array")
	}

	// Validate each hotkey entry and check for duplicates.
	// Allow the same command_id with Ctrl+X and Cmd+X (platform variants).
	seenCommandIDs := make(map[string]bool)
	// Track which command_ids have been seen per platform prefix.
	// A command_id is a true duplicate only when the same platform-specific
	// prefix (Ctrl or Cmd) is used more than once for that command_id.
	seenWithCtrl := make(map[string]bool)
	seenWithCmd := make(map[string]bool)
	keyPattern := regexp.MustCompile("^(?:(?:Ctrl|Cmd|Shift|Alt|Meta)\\+)*(?:[a-zA-Z0-9]|F[0-9]{1,2}|Backspace|Tab|Enter|Escape|Space|ArrowUp|ArrowDown|ArrowLeft|ArrowRight|Delete|Home|End|PageUp|PageDown|Insert|Backquote|[`\x60\\\\])$")

	for i, entry := range config.Hotkeys {
		// Validate key format
		if entry.Key == "" {
			return fmt.Errorf("hotkey entry at index %d has empty key", i)
		}
		if !keyPattern.MatchString(entry.Key) {
			return fmt.Errorf("hotkey entry at index %d has invalid key format %q (expected format: Ctrl+Shift+A, Cmd+P, F5, etc.)", i, entry.Key)
		}

		// Validate command_id
		if entry.CommandID == "" {
			return fmt.Errorf("hotkey entry at index %d has empty command_id", i)
		}
		commandIDPattern := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
		if !commandIDPattern.MatchString(entry.CommandID) {
			return fmt.Errorf("hotkey entry at index %d has invalid command_id %q (must be alphanumeric with underscores and hyphens only)", i, entry.CommandID)
		}

		// Check for true duplicate command_ids (same platform prefix + same command).
		// Ctrl+X and Cmd+X for the same command_id are allowed (platform variants).
		isCmdKey := strings.HasPrefix(entry.Key, "Cmd+")
		isCtrlKey := strings.HasPrefix(entry.Key, "Ctrl+") && !isCmdKey

		if isCmdKey && seenWithCmd[entry.CommandID] {
			return fmt.Errorf("duplicate command_id %q with Cmd modifier at index %d", entry.CommandID, i)
		}
		if isCtrlKey && seenWithCtrl[entry.CommandID] {
			return fmt.Errorf("duplicate command_id %q with Ctrl modifier at index %d", entry.CommandID, i)
		}
		if !isCmdKey && !isCtrlKey {
			// Bare key (no modifier) — only allow one per command_id
			if seenCommandIDs[entry.CommandID] {
				return fmt.Errorf("duplicate command_id %q found at index %d (each command can only be bound once)", entry.CommandID, i)
			}
			seenCommandIDs[entry.CommandID] = true
		}

		if isCmdKey {
			seenWithCmd[entry.CommandID] = true
		}
		if isCtrlKey {
			seenWithCtrl[entry.CommandID] = true
		}
	}

	return nil
}

// GetMacHotkeys returns only Mac-specific hotkeys (those with Cmd modifier)
func (h *HotkeyConfig) GetMacHotkeys() []HotkeyEntry {
	var macHotkeys []HotkeyEntry
	for _, entry := range h.Hotkeys {
		if strings.HasPrefix(entry.Key, "Cmd+") {
			macHotkeys = append(macHotkeys, entry)
		}
	}
	return macHotkeys
}

// GetNonMacHotkeys returns only non-Mac hotkeys (those with Ctrl modifier)
func (h *HotkeyConfig) GetNonMacHotkeys() []HotkeyEntry {
	var nonMacHotkeys []HotkeyEntry
	for _, entry := range h.Hotkeys {
		if strings.HasPrefix(entry.Key, "Ctrl+") {
			nonMacHotkeys = append(nonMacHotkeys, entry)
		}
	}
	return nonMacHotkeys
}

// GetHotkeyByCommandID returns the hotkey entry for a specific command_id
// Returns the first matching entry (typically the non-Mac variant)
func (h *HotkeyConfig) GetHotkeyByCommandID(commandID string) *HotkeyEntry {
	for _, entry := range h.Hotkeys {
		if entry.CommandID == commandID {
			return &entry
		}
	}
	return nil
}
