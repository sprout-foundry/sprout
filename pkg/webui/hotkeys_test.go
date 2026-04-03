package webui

import (
	"encoding/json"
	"testing"
)

func TestDefaultHotkeyConfig(t *testing.T) {
	config := DefaultHotkeyConfig()
	if config == nil {
		t.Fatal("DefaultHotkeyConfig returned nil")
	}
	if config.Version == "" {
		t.Error("Default config missing version")
	}
	if len(config.Hotkeys) == 0 {
		t.Error("Default config has no hotkeys")
	}

	// Validate default config passes its own validation
	if err := ValidateHotkeyConfig(config); err != nil {
		t.Errorf("Default hotkey config failed validation: %v", err)
	}
}

func TestValidateHotkeyConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name:    "valid minimal config",
			config:  `{"version": "1.0", "hotkeys": [{"key": "Ctrl+S", "command_id": "save_file"}]}`,
			wantErr: false,
		},
		{
			name:    "empty version",
			config:  `{"version": "", "hotkeys": [{"key": "Ctrl+S", "command_id": "save_file"}]}`,
			wantErr: true,
		},
		{
			name:    "nil hotkeys array",
			config:  `{"version": "1.0"}`,
			wantErr: true,
		},
		{
			name:    "empty key",
			config:  `{"version": "1.0", "hotkeys": [{"key": "", "command_id": "save"}]}`,
			wantErr: true,
		},
		{
			name:    "invalid key format",
			config:  `{"version": "1.0", "hotkeys": [{"key": "!!invalid!!", "command_id": "save"}]}`,
			wantErr: true,
		},
		{
			name:    "empty command_id",
			config:  `{"version": "1.0", "hotkeys": [{"key": "Ctrl+S", "command_id": ""}]}`,
			wantErr: true,
		},
		{
			name:    "invalid command_id",
			config:  `{"version": "1.0", "hotkeys": [{"key": "Ctrl+S", "command_id": "has spaces"}]}`,
			wantErr: true,
		},
		{
			name:    "duplicate command_id with same modifier",
			config:  `{"version": "1.0", "hotkeys": [{"key": "Ctrl+S", "command_id": "save"}, {"key": "Ctrl+Shift+S", "command_id": "save"}]}`,
			wantErr: true,
		},
		{
			name:    "same command_id different platform variants allowed",
			config:  `{"version": "1.0", "hotkeys": [{"key": "Ctrl+Shift+P", "command_id": "palette"}, {"key": "Cmd+Shift+P", "command_id": "palette"}]}`,
			wantErr: false,
		},
		{
			name:    "valid function key",
			config:  `{"version": "1.0", "hotkeys": [{"key": "F5", "command_id": "refresh"}]}`,
			wantErr: false,
		},
		{
			name:    "valid Backquote key",
			config:  `{"version": "1.0", "hotkeys": [{"key": "Ctrl+Backquote", "command_id": "toggle_terminal"}]}`,
			wantErr: false,
		},
		{
			name:    "valid Escape key",
			config:  `{"version": "1.0", "hotkeys": [{"key": "Escape", "command_id": "dismiss"}]}`,
			wantErr: false,
		},
		{
			name:    "valid Cmd modifier",
			config:  `{"version": "1.0", "hotkeys": [{"key": "Cmd+Shift+P", "command_id": "palette"}]}`,
			wantErr: false,
		},
		{
			name:    "valid with description",
			config:  `{"version": "1.0", "hotkeys": [{"key": "Ctrl+S", "command_id": "save", "description": "Save file"}]}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config HotkeyConfig
			if err := json.Unmarshal([]byte(tt.config), &config); err != nil {
				t.Fatalf("Failed to parse test config JSON: %v", err)
			}

			err := ValidateHotkeyConfig(&config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHotkeyConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHotkeyConfigPlatformFiltering(t *testing.T) {
	config := DefaultHotkeyConfig()

	macHotkeys := config.GetMacHotkeys()
	if len(macHotkeys) == 0 {
		t.Error("No Mac hotkeys found in default config")
	}
	for _, h := range macHotkeys {
		if h.Key[:3] != "Cmd" {
			t.Errorf("Mac hotkey should start with Cmd: %s", h.Key)
		}
	}

	nonMacHotkeys := config.GetNonMacHotkeys()
	if len(nonMacHotkeys) == 0 {
		t.Error("No non-Mac hotkeys found in default config")
	}
	for _, h := range nonMacHotkeys {
		if h.Key[:4] != "Ctrl" {
			t.Errorf("Non-Mac hotkey should start with Ctrl: %s", h.Key)
		}
	}
}

func TestHotkeyConfigSaveAndLoad(t *testing.T) {
	config := DefaultHotkeyConfig()

	// Verify save path exists function works
	path, err := GetHotkeysPath()
	if err != nil {
		t.Fatalf("GetHotkeysPath() returned error: %v", err)
	}
	if path == "" {
		t.Error("GetHotkeysPath() returned empty path")
	}

	// Save default config
	if err := SaveHotkeys(config); err != nil {
		t.Fatalf("SaveHotkeys() returned error: %v", err)
	}

	// Load it back
	loaded, err := LoadHotkeys()
	if err != nil {
		t.Fatalf("LoadHotkeys() returned error: %v", err)
	}

	if loaded.Version != config.Version {
		t.Errorf("Version mismatch: got %q, want %q", loaded.Version, config.Version)
	}

	if len(loaded.Hotkeys) != len(config.Hotkeys) {
		t.Errorf("Hotkey count mismatch: got %d, want %d", len(loaded.Hotkeys), len(config.Hotkeys))
	}
}

func TestHotkeyConfigNil(t *testing.T) {
	err := ValidateHotkeyConfig(nil)
	if err == nil {
		t.Error("ValidateHotkeyConfig(nil) should return error")
	}
}

func TestHotkeyPresetConfigs(t *testing.T) {
	for _, preset := range []string{"vscode", "webstorm", "ledit"} {
		t.Run(preset, func(t *testing.T) {
			config := HotkeyPresetConfig(preset)
			if config == nil {
				t.Fatal("HotkeyPresetConfig returned nil")
			}
			if config.Version == "" {
				t.Error("Preset config missing version")
			}
			if len(config.Hotkeys) == 0 {
				t.Error("Preset config has no hotkeys")
			}
			if err := ValidateHotkeyConfig(config); err != nil {
				t.Errorf("Preset %q config failed validation: %v", preset, err)
			}

			// Every preset must include the universal hotkeys.
			mustHave := []string{
				"save_file", "command_palette", "toggle_sidebar",
				"toggle_terminal", "close_editor", "open_search",
			}
			for _, cmd := range mustHave {
				found := false
				for _, h := range config.Hotkeys {
					if h.CommandID == cmd {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Preset %q missing required command_id %q", preset, cmd)
				}
			}

			// Every preset must include editor-specific hotkeys.
			editorCmds := []string{
				"editor_goto_line", "editor_move_line_up", "editor_move_line_down",
				"editor_duplicate_line_up", "editor_duplicate_line_down", "editor_delete_line",
				"editor_insert_line_below", "editor_insert_line_above", "editor_select_all_occurrences",
				"editor_goto_symbol", "editor_toggle_word_wrap",
			}
			for _, cmd := range editorCmds {
				found := false
				for _, h := range config.Hotkeys {
					if h.CommandID == cmd {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Preset %q missing editor command_id %q", preset, cmd)
				}
			}

			// VS Code / Ledit presets also include Ctrl+D for next-match
			// selection (WebStorm uses Ctrl+D for duplicate-line-down instead).
			if preset != "webstorm" {
				vscodeEditorCmds := []string{
					"editor_add_selection_to_next_match",
				}
				for _, cmd := range vscodeEditorCmds {
					found := false
					for _, h := range config.Hotkeys {
						if h.CommandID == cmd {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Preset %q missing editor command_id %q", preset, cmd)
					}
				}
			}

			// Regression guard: Ctrl+D/Cmd+D must NOT be bound to
			// editor_delete_line in vscode or ledit presets.  CodeMirror's
			// searchKeymap already provides Mod-d → selectNextOccurrence and
			// the override would break multi-cursor find-match selection.
			if preset != "webstorm" {
				for _, h := range config.Hotkeys {
					if h.CommandID == "editor_delete_line" && (h.Key == "Ctrl+D" || h.Key == "Cmd+D") {
						t.Errorf("Preset %q: editor_delete_line must not use Ctrl+D/Cmd+D (use Ctrl+Shift+K)", preset)
					}
				}
			}
		})
	}
}

func TestHotkeyPresetConfigUnknown(t *testing.T) {
	// Unknown preset should fall back to default.
	def := DefaultHotkeyConfig()
	got := HotkeyPresetConfig("unknown_ide")
	if len(got.Hotkeys) != len(def.Hotkeys) {
		t.Errorf("Unknown preset should return default; got %d hotkeys, want %d",
			len(got.Hotkeys), len(def.Hotkeys))
	}
}
