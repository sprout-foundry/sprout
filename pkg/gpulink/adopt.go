package gpulink

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// AdoptLink moves links saved under another config directory into the one
// this process uses. A link belongs to the machine, but `sprout gpu link`
// once saved its link to the repo's .sprout config, where the background
// service (started outside any repo) never looks. It reports whether any
// link was moved, and skips a link this config already has under the same
// slot (the repo's copy would be the same one).
func AdoptLink(fromDir string) (bool, error) {
	current, err := LoadLinks()
	if err != nil {
		return false, err
	}
	slots := map[string]bool{}
	for _, st := range current {
		slots[st.Slot()] = true
	}

	moved, err := adoptDirLinks(fromDir, slots)
	if err != nil {
		return moved, err
	}
	legacyMoved, err := adoptLegacyLink(fromDir, slots)
	if legacyMoved {
		moved = true
	}
	if moved {
		// The repo's config no longer looks linked; what stays is a link
		// this machine already had (skipped above).
		_ = os.RemoveAll(filepath.Join(fromDir, "gpu"))
	}
	return moved, err
}

// adoptDirLinks moves the gpu/links/*.json files the current sprout layout
// uses.
func adoptDirLinks(fromDir string, slots map[string]bool) (bool, error) {
	linksDir := filepath.Join(fromDir, "gpu", "links")
	entries, err := os.ReadDir(linksDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", linksDir, err)
	}
	moved := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(linksDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return moved, fmt.Errorf("reading %s: %w", path, err)
		}
		st := &State{Engine: DefaultEngine}
		if err := json.Unmarshal(data, st); err != nil {
			return moved, fmt.Errorf("parsing %s: %w", path, err)
		}
		if !st.Linked() || slots[st.Slot()] {
			continue
		}
		if err := adoptOne(fromDir, st); err != nil {
			return moved, err
		}
		slots[st.Slot()] = true
		moved = true
	}
	return moved, nil
}

// adoptLegacyLink moves the single gpu.json an older sprout saved in the
// repo's config.
func adoptLegacyLink(fromDir string, slots map[string]bool) (bool, error) {
	path := filepath.Join(fromDir, "gpu", "gpu.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	st := &State{Engine: DefaultEngine}
	if err := json.Unmarshal(data, st); err != nil {
		return false, fmt.Errorf("parsing %s: %w", path, err)
	}
	if !st.Linked() || slots[st.Slot()] {
		return false, nil
	}
	if err := adoptOne(fromDir, st); err != nil {
		return false, err
	}
	return true, nil
}

// adoptOne moves one link and its keys, then clears the source config's copy.
func adoptOne(fromDir string, st *State) error {
	var key, engineKey string
	var err error
	withConfigDir(fromDir, func() {
		key, err = loadSecret(AgentKeyName(st.Slot()))
		if err != nil || key == "" {
			key, _ = loadSecret(legacyAgentKey)
		}
		engineKey, _ = loadSecret(EngineKeyName(st.Slot()))
		if engineKey == "" {
			engineKey, _ = loadSecret(legacyEngineKey)
		}
	})
	if err != nil || key == "" {
		return fmt.Errorf("the link %q in %s has no key in its credential store; run `sprout gpu link` again", st.Name, fromDir)
	}
	if _, err := SaveAPIKeyFor(st.Slot(), key); err != nil {
		return err
	}
	if engineKey != "" {
		if err := SaveEngineKeyFor(st.Slot(), engineKey); err != nil {
			return err
		}
	}
	if err := st.Save(); err != nil {
		return err
	}
	// The source entries go (also the legacy key names an old sprout may
	// have used), so the repo's config no longer looks linked.
	withConfigDir(fromDir, func() {
		_ = credentials.NewFileBackend().Delete(AgentKeyName(st.Slot()))
		_ = credentials.NewFileBackend().Delete(EngineKeyName(st.Slot()))
		_ = credentials.NewFileBackend().Delete(legacyAgentKey)
		_ = credentials.NewFileBackend().Delete(legacyEngineKey)
	})
	path := filepath.Join(fromDir, "gpu", "links", st.Slot()+".json")
	_ = os.Remove(path)
	_ = os.Remove(filepath.Join(fromDir, "gpu", "gpu.json"))
	return nil
}

// withConfigDir runs fn with sprout's config directory pointed at dir, then
// puts it back.
func withConfigDir(dir string, fn func()) {
	restore := func(name string) func() {
		old, had := os.LookupEnv(name)
		return func() {
			if had {
				_ = os.Setenv(name, old)
			} else {
				_ = os.Unsetenv(name)
			}
		}
	}
	restoreDir, restoreConfig := restore("SPROUT_CONFIG_DIR"), restore("SPROUT_CONFIG")
	_ = os.Setenv("SPROUT_CONFIG_DIR", dir)
	credentials.ResetStorageBackend()
	defer func() {
		restoreDir()
		restoreConfig()
		credentials.ResetStorageBackend()
	}()
	fn()
}
