//go:build !js

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// firstRunStateFile is the absolute path to the persisted state used to
// track which workspaces have seen the first-run hint.
const firstRunStateFile = ".sprout/state.json"

// sproutState is the on-disk shape persisted at ~/.sprout/state.json. Only
// fields that survive across sprout versions belong here; per-run state
// (e.g. session metadata) lives elsewhere.
type sproutState struct {
	SeenFirstRunHint []string `json:"seen_first_run_hint,omitempty"`
}

var firstRunStateMu sync.Mutex

// maybeShowFirstRunHint prints a single brief hint about REPL keystrokes
// the very first time a workspace is opened in `sprout agent`. After
// printing it, the workspace path is appended to ~/.sprout/state.json so
// the hint never repeats in that workspace. SP-048-5b.
//
// Silent on:
//   - errors loading or persisting state (the hint is non-essential)
//   - non-interactive contexts (output would be noise)
//   - workspaces already in the seen list
//
// Note: this prints to stderr to avoid contaminating stdout for users
// who pipe sprout's output.
func maybeShowFirstRunHint() {
	firstRunStateMu.Lock()
	defer firstRunStateMu.Unlock()

	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	statePath, err := firstRunStatePath()
	if err != nil {
		return
	}

	state, _ := loadFirstRunState(statePath) // best-effort; nil state means show
	if state != nil {
		for _, ws := range state.SeenFirstRunHint {
			if ws == cwd {
				return
			}
		}
	} else {
		state = &sproutState{}
	}

	fmt.Fprintln(os.Stderr, "Press Tab to autocomplete /commands, Ctrl-D to exit, or just start typing.")

	state.SeenFirstRunHint = append(state.SeenFirstRunHint, cwd)
	_ = saveFirstRunState(statePath, state) // best-effort — we don't care if persist fails
}

func firstRunStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, firstRunStateFile), nil
}

func loadFirstRunState(path string) (*sproutState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s sproutState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func saveFirstRunState(path string, state *sproutState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
