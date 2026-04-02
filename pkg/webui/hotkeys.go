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
	Global      bool   `json:"global,omitempty"` // If true, works even when input is focused
}

// HotkeyConfig represents the complete hotkey configuration
type HotkeyConfig struct {
	Version string        `json:"version"`
	Hotkeys []HotkeyEntry `json:"hotkeys"`
}

// sharedUniversalHotkeys returns the set of hotkeys common to every preset
// (app-level shortcuts like command palette, file explorer, etc.).
func sharedUniversalHotkeys() []HotkeyEntry {
	return []HotkeyEntry{
		{Key: "Ctrl+Shift+P", CommandID: "command_palette", Description: "Toggle command palette"},
		{Key: "Cmd+Shift+P", CommandID: "command_palette", Description: "Toggle command palette (Mac)"},
		{Key: "Ctrl+P", CommandID: "quick_open", Description: "Quick open file"},
		{Key: "Cmd+P", CommandID: "quick_open", Description: "Quick open file (Mac)"},
		{Key: "Ctrl+Shift+E", CommandID: "toggle_explorer", Description: "Toggle file explorer"},
		{Key: "Cmd+Shift+E", CommandID: "toggle_explorer", Description: "Toggle file explorer (Mac)"},
		{Key: "Ctrl+B", CommandID: "toggle_sidebar", Description: "Toggle sidebar"},
		{Key: "Cmd+B", CommandID: "toggle_sidebar", Description: "Toggle sidebar (Mac)"},
		{Key: "Ctrl+`", CommandID: "toggle_terminal", Description: "Toggle terminal"},
		{Key: "Cmd+`", CommandID: "toggle_terminal", Description: "Toggle terminal (Mac)"},
		{Key: "Ctrl+W", CommandID: "close_editor", Description: "Close editor tab"},
		{Key: "Cmd+W", CommandID: "close_editor", Description: "Close editor tab (Mac)"},
		{Key: "Ctrl+S", CommandID: "save_file", Description: "Save file", Global: true},
		{Key: "Cmd+S", CommandID: "save_file", Description: "Save file (Mac)", Global: true},
		{Key: "Ctrl+Shift+S", CommandID: "save_all_files", Description: "Save all files", Global: true},
		{Key: "Cmd+Shift+S", CommandID: "save_all_files", Description: "Save all files (Mac)", Global: true},
		{Key: "Ctrl+N", CommandID: "new_file", Description: "New file"},
		{Key: "Cmd+N", CommandID: "new_file", Description: "New file (Mac)"},
		{Key: "Ctrl+\\", CommandID: "split_editor_vertical", Description: "Split editor vertical"},
		{Key: "Ctrl+1", CommandID: "focus_tab_1", Description: "Focus first tab in active split"},
		{Key: "Cmd+1", CommandID: "focus_tab_1", Description: "Focus first tab in active split (Mac)"},
		{Key: "Ctrl+2", CommandID: "focus_tab_2", Description: "Focus second tab in active split"},
		{Key: "Cmd+2", CommandID: "focus_tab_2", Description: "Focus second tab in active split (Mac)"},
		{Key: "Ctrl+3", CommandID: "focus_tab_3", Description: "Focus third tab in active split"},
		{Key: "Cmd+3", CommandID: "focus_tab_3", Description: "Focus third tab in active split (Mac)"},
		{Key: "Ctrl+4", CommandID: "focus_tab_4", Description: "Focus fourth tab in active split"},
		{Key: "Cmd+4", CommandID: "focus_tab_4", Description: "Focus fourth tab in active split (Mac)"},
		{Key: "Ctrl+5", CommandID: "focus_tab_5", Description: "Focus fifth tab in active split"},
		{Key: "Cmd+5", CommandID: "focus_tab_5", Description: "Focus fifth tab in active split (Mac)"},
		{Key: "Ctrl+6", CommandID: "focus_tab_6", Description: "Focus sixth tab in active split"},
		{Key: "Cmd+6", CommandID: "focus_tab_6", Description: "Focus sixth tab in active split (Mac)"},
		{Key: "Ctrl+7", CommandID: "focus_tab_7", Description: "Focus seventh tab in active split"},
		{Key: "Cmd+7", CommandID: "focus_tab_7", Description: "Focus seventh tab in active split (Mac)"},
		{Key: "Ctrl+8", CommandID: "focus_tab_8", Description: "Focus eighth tab in active split"},
		{Key: "Cmd+8", CommandID: "focus_tab_8", Description: "Focus eighth tab in active split (Mac)"},
		{Key: "Ctrl+9", CommandID: "focus_tab_9", Description: "Focus ninth tab in active split"},
		{Key: "Cmd+9", CommandID: "focus_tab_9", Description: "Focus ninth tab in active split (Mac)"},
		{Key: "Ctrl+Tab", CommandID: "focus_next_tab", Description: "Focus next tab"},
		{Key: "Ctrl+Shift+Tab", CommandID: "focus_prev_tab", Description: "Focus previous tab"},
		// Note: No Cmd+Tab / Cmd+Shift+Tab variants — these are intercepted by macOS
		// for native app switching and cannot be overridden in a browser.
		{Key: "Ctrl+Shift+F", CommandID: "open_search", Description: "Open search", Global: true},
		{Key: "Cmd+Shift+F", CommandID: "open_search", Description: "Open search (Mac)", Global: true},
	}
}

// VsCodeHotkeyConfig returns a hotkey configuration matching VS Code defaults.
func VsCodeHotkeyConfig() *HotkeyConfig {
	editor := []HotkeyEntry{
		{Key: "Ctrl+G", CommandID: "editor_goto_line", Description: "Go to line"},
		{Key: "Cmd+G", CommandID: "editor_goto_line", Description: "Go to line (Mac)"},
		{Key: "Alt+ArrowUp", CommandID: "editor_move_line_up", Description: "Move line up"},
		{Key: "Alt+ArrowDown", CommandID: "editor_move_line_down", Description: "Move line down"},
		{Key: "Shift+Alt+ArrowDown", CommandID: "editor_duplicate_line_down", Description: "Duplicate line down"},
		{Key: "Shift+Alt+ArrowUp", CommandID: "editor_duplicate_line_up", Description: "Duplicate line up"},
		{Key: "Ctrl+Shift+K", CommandID: "editor_delete_line", Description: "Delete current line"},
		{Key: "Cmd+Shift+K", CommandID: "editor_delete_line", Description: "Delete current line (Mac)"},
		{Key: "Ctrl+Enter", CommandID: "editor_insert_line_below", Description: "Insert line below"},
		{Key: "Cmd+Enter", CommandID: "editor_insert_line_below", Description: "Insert line below (Mac)"},
		{Key: "Ctrl+Shift+Enter", CommandID: "editor_insert_line_above", Description: "Insert line above"},
		{Key: "Cmd+Shift+Enter", CommandID: "editor_insert_line_above", Description: "Insert line above (Mac)"},
		{Key: "Ctrl+Shift+L", CommandID: "editor_select_all_occurrences", Description: "Select all occurrences of find match"},
		{Key: "Cmd+Shift+L", CommandID: "editor_select_all_occurrences", Description: "Select all occurrences of find match (Mac)"},
	}
	all := append(sharedUniversalHotkeys(), editor...)
	return &HotkeyConfig{Version: "1.0", Hotkeys: all}
}

// WebStormHotkeyConfig returns a hotkey configuration matching WebStorm/IntelliJ defaults.
func WebStormHotkeyConfig() *HotkeyConfig {
	editor := []HotkeyEntry{
		{Key: "Ctrl+G", CommandID: "editor_goto_line", Description: "Go to line"},
		{Key: "Cmd+G", CommandID: "editor_goto_line", Description: "Go to line (Mac)"},
		{Key: "Shift+Alt+ArrowUp", CommandID: "editor_move_line_up", Description: "Move line up"},
		{Key: "Shift+Alt+ArrowDown", CommandID: "editor_move_line_down", Description: "Move line down"},
		{Key: "Ctrl+D", CommandID: "editor_duplicate_line_down", Description: "Duplicate line down"},
		{Key: "Cmd+D", CommandID: "editor_duplicate_line_down", Description: "Duplicate line down (Mac)"},
		{Key: "Ctrl+Shift+Alt+ArrowUp", CommandID: "editor_duplicate_line_up", Description: "Duplicate line up"},
		{Key: "Cmd+Shift+Alt+ArrowUp", CommandID: "editor_duplicate_line_up", Description: "Duplicate line up (Mac)"},
		{Key: "Ctrl+Shift+D", CommandID: "editor_delete_line", Description: "Delete current line"},
		{Key: "Cmd+Shift+D", CommandID: "editor_delete_line", Description: "Delete current line (Mac)"},
		{Key: "Ctrl+Enter", CommandID: "editor_insert_line_below", Description: "Insert line below"},
		{Key: "Cmd+Enter", CommandID: "editor_insert_line_below", Description: "Insert line below (Mac)"},
		{Key: "Ctrl+Shift+Enter", CommandID: "editor_insert_line_above", Description: "Insert line above"},
		{Key: "Cmd+Shift+Enter", CommandID: "editor_insert_line_above", Description: "Insert line above (Mac)"},
		{Key: "Ctrl+Shift+L", CommandID: "editor_select_all_occurrences", Description: "Select all occurrences of find match"},
		{Key: "Cmd+Shift+L", CommandID: "editor_select_all_occurrences", Description: "Select all occurrences of find match (Mac)"},
	}
	all := append(sharedUniversalHotkeys(), editor...)
	return &HotkeyConfig{Version: "1.0", Hotkeys: all}
}

// HotkeyPresetConfig returns the hotkey configuration for a named preset.
// Supported presets: "vscode", "webstorm", "ledit".
// For unknown presets, falls back to the default config.
func HotkeyPresetConfig(preset string) *HotkeyConfig {
	switch strings.ToLower(preset) {
	case "vscode":
		return VsCodeHotkeyConfig()
	case "webstorm":
		return WebStormHotkeyConfig()
	case "ledit":
		return DefaultHotkeyConfig()
	default:
		return DefaultHotkeyConfig()
	}
}

// DefaultHotkeyConfig returns the default hotkey configuration
func DefaultHotkeyConfig() *HotkeyConfig {
	editor := []HotkeyEntry{
		{Key: "Ctrl+G", CommandID: "editor_goto_line", Description: "Go to line"},
		{Key: "Cmd+G", CommandID: "editor_goto_line", Description: "Go to line (Mac)"},
		{Key: "Alt+ArrowUp", CommandID: "editor_move_line_up", Description: "Move line up"},
		{Key: "Alt+ArrowDown", CommandID: "editor_move_line_down", Description: "Move line down"},
		{Key: "Shift+Alt+ArrowDown", CommandID: "editor_duplicate_line_down", Description: "Duplicate line down"},
		{Key: "Shift+Alt+ArrowUp", CommandID: "editor_duplicate_line_up", Description: "Duplicate line up"},
		{Key: "Ctrl+Shift+K", CommandID: "editor_delete_line", Description: "Delete current line"},
		{Key: "Cmd+Shift+K", CommandID: "editor_delete_line", Description: "Delete current line (Mac)"},
		{Key: "Ctrl+Enter", CommandID: "editor_insert_line_below", Description: "Insert line below"},
		{Key: "Cmd+Enter", CommandID: "editor_insert_line_below", Description: "Insert line below (Mac)"},
		{Key: "Ctrl+Shift+Enter", CommandID: "editor_insert_line_above", Description: "Insert line above"},
		{Key: "Cmd+Shift+Enter", CommandID: "editor_insert_line_above", Description: "Insert line above (Mac)"},
		{Key: "Ctrl+Shift+L", CommandID: "editor_select_all_occurrences", Description: "Select all occurrences of find match"},
		{Key: "Cmd+Shift+L", CommandID: "editor_select_all_occurrences", Description: "Select all occurrences of find match (Mac)"},
	}
	all := append(sharedUniversalHotkeys(), editor...)
	return &HotkeyConfig{Version: "1.0", Hotkeys: all}
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
