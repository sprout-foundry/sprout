package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// credentialKey names the runner's API key in sprout's credential store
// (OS keyring, or the encrypted file fallback).
const credentialKey = "sprout-runner"

// DefaultListenAddr is where the host server listens for platform calls.
const DefaultListenAddr = "127.0.0.1:8935"

// DefaultImage is the workspace image container mode runs.
const DefaultImage = "ghcr.io/sprout-foundry/sprout:latest"

// State is the runner's persistent configuration. The API key is not part of
// it: it lives in the credential store.
type State struct {
	PlatformURL string `json:"platform_url"`
	RunnerID    string `json:"runner_id"`
	Name        string `json:"name"`
	Mode        string `json:"mode"`
	// PublicURL is the HTTPS base the platform reaches the host server at
	// (direct mode). Empty means the relay tunnel, once the platform offers it.
	PublicURL  string `json:"public_url,omitempty"`
	ListenAddr string `json:"listen_addr,omitempty"`
	Image      string `json:"image,omitempty"`
	// Writable are extra paths native mode may write (toolchain caches).
	Writable []string `json:"writable,omitempty"`
}

// Linked reports whether the runner has completed `sprout runner link`.
func (s *State) Linked() bool { return s.RunnerID != "" }

// Dir is where the runner keeps its state and workspaces.
func Dir() (string, error) {
	base, err := configuration.GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config dir: %w", err)
	}
	return filepath.Join(base, "runner"), nil
}

// WorkspacesDir holds one clone per workspace; a runner never touches the
// user's own working copies. It lives in sprout's state dir, outside the
// config dir whose credential files native mode hides from workspaces.
func WorkspacesDir() (string, error) {
	base, err := envutil.StateDir()
	if err != nil {
		return "", fmt.Errorf("locating state dir: %w", err)
	}
	return filepath.Join(base, "runner", "workspaces"), nil
}

func statePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "runner.json"), nil
}

// LoadState returns the saved state, or defaults when the runner was never
// linked.
func LoadState() (*State, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}
	s := &State{Mode: ModeContainer, ListenAddr: DefaultListenAddr, Image: DefaultImage}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading runner state: %w", err)
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if !ValidMode(s.Mode) {
		return nil, fmt.Errorf("%s has unknown mode %q", path, s.Mode)
	}
	return s, nil
}

// Save writes the state with owner-only permissions.
func (s *State) Save() error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating runner dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing runner state: %w", err)
	}
	return os.Rename(tmp, path)
}

// SaveAPIKey stores the runner's API key in the credential store. The
// platform issues the key exactly once, so when the OS keyring refuses (no
// GUI session, locked keychain) it falls back to sprout's encrypted file
// store rather than lose it.
func SaveAPIKey(key string) (source string, err error) {
	if err := credentials.SetToActiveBackend(credentialKey, key); err == nil {
		return "credential store", nil
	} else if fileErr := credentials.NewFileBackend().Set(credentialKey, key); fileErr != nil {
		return "", fmt.Errorf("%w; file fallback: %v", err, fileErr)
	}
	return "encrypted file (the OS keyring was unavailable)", nil
}

// LoadAPIKey reads the runner's API key from the credential store, or the
// file fallback SaveAPIKey may have used.
func LoadAPIKey() (string, error) {
	key, _, err := credentials.GetFromActiveBackend(credentialKey)
	if err == nil && key != "" {
		return key, nil
	}
	if fileKey, fileErr := credentials.NewFileBackend().Get(credentialKey); fileErr == nil && fileKey != "" {
		return fileKey, nil
	}
	return key, err
}

// DeleteAPIKey removes the runner's API key from both stores.
func DeleteAPIKey() error {
	err := credentials.DeleteFromActiveBackend(credentialKey)
	_ = credentials.NewFileBackend().Delete(credentialKey)
	return err
}
