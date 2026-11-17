package gpulink

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// AdoptLink moves a link saved under another config directory into the one
// this process uses. A link belongs to the machine, but `sprout gpu link` run
// inside a git repo once saved it to that repo's .sprout config, where the
// background service (started outside any repo) never looks. It reports
// whether a link was moved, and does nothing when this config already has one.
func AdoptLink(fromDir string) (bool, error) {
	current, err := LoadState()
	if err != nil {
		return false, err
	}
	if current.Linked() {
		return false, nil
	}
	oldState := filepath.Join(fromDir, "gpu", "gpu.json")
	data, err := os.ReadFile(oldState)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", oldState, err)
	}
	st := &State{Engine: DefaultEngine}
	if err := json.Unmarshal(data, st); err != nil {
		return false, fmt.Errorf("parsing %s: %w", oldState, err)
	}
	if !st.Linked() {
		return false, nil
	}

	var key, engineKey string
	withConfigDir(fromDir, func() {
		key, err = loadSecret(credentialKey)
		engineKey, _ = loadSecret(engineKeyCredential)
	})
	if err != nil || key == "" {
		return false, fmt.Errorf("the link in %s has no key in its credential store; run `sprout gpu link` again", fromDir)
	}
	if _, err := SaveAPIKey(key); err != nil {
		return false, err
	}
	if engineKey != "" {
		if err := SaveEngineKey(engineKey); err != nil {
			return false, err
		}
	}
	if err := st.Save(); err != nil {
		return false, err
	}
	// The old copy goes, so the repo's config no longer looks linked.
	withConfigDir(fromDir, func() {
		_ = credentials.NewFileBackend().Delete(credentialKey)
		_ = credentials.NewFileBackend().Delete(engineKeyCredential)
	})
	_ = os.RemoveAll(filepath.Join(fromDir, "gpu"))
	return true, nil
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
