// Package gpulink implements `sprout gpu`: linking a GPU this machine runs
// (Ollama, llama.cpp, vLLM, MLX, LM Studio: any engine answering OpenAI's
// /v1/chat/completions) to a platform account, and the agent that serves the
// platform's model requests to it over a tunnel it dials out on, so no port
// is opened on the person's network.
//
// The platform side of the protocol is the platform repo's
// docs/runners/GPU-LINK.md: the runner device flow (kind "gpu") to link, then
// the runner relay framing (pkg/runner/relay) over /internal/gpu/tunnel.
package gpulink

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

const (
	// credentialKey names the link's agent key in sprout's credential store.
	credentialKey = "sprout-gpu-link" //nolint:gosec // G101: a credential-store entry *name*, not a credential value
	// engineKeyCredential names the engine's own API key, when it has one.
	engineKeyCredential = "sprout-gpu-engine" //nolint:gosec // G101: a credential-store entry *name*, not a credential value
)

// DefaultEngine is Ollama's address, the most common local engine.
const DefaultEngine = "127.0.0.1:11434"

// EngineSinter is the Engine of a link served in-process by sinter, with
// the model in State.Model, instead of by a server at host:port.
const EngineSinter = "sinter"

// State is the link's persistent configuration. The keys are not part of
// it: they live in the credential store.
type State struct {
	PlatformURL string `json:"platform_url"`
	LinkID      string `json:"link_id"`
	Name        string `json:"name"`
	// Account is the email of the account the GPU is linked to.
	Account string `json:"account,omitempty"`
	// Engine is the engine's host:port as this machine reaches it, or
	// EngineSinter.
	Engine string `json:"engine"`
	// Model is the sinter model directory when Engine is EngineSinter.
	Model string `json:"model,omitempty"`
	// Fleet marks a link to the platform's own fleet: its agent keeps two
	// tunnels and rotates its key every FleetKeyRotation.
	Fleet bool `json:"fleet,omitempty"`
	// KeyRotatedAt is when the agent key was last rotated (or issued).
	KeyRotatedAt time.Time `json:"key_rotated_at,omitempty"`
}

// FleetKeyRotation is how often a fleet GPU's agent rotates its key.
const FleetKeyRotation = 30 * 24 * time.Hour

// Linked reports whether `sprout gpu link` completed.
func (s *State) Linked() bool { return s.LinkID != "" }

// Dir is where the link keeps its state.
func Dir() (string, error) {
	base, err := configuration.GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config dir: %w", err)
	}
	return filepath.Join(base, "gpu"), nil
}

func statePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gpu.json"), nil
}

// LoadState returns the saved state, or an empty one when no GPU was linked.
func LoadState() (*State, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}
	s := &State{Engine: DefaultEngine}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading gpu link state: %w", err)
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
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
		return fmt.Errorf("creating gpu link dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing gpu link state: %w", err)
	}
	return os.Rename(tmp, path)
}

// Remove deletes the state and both keys.
func Remove() error {
	_ = deleteSecret(credentialKey)
	_ = deleteSecret(engineKeyCredential)
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// SaveAPIKey stores the agent key in sprout's encrypted file store, where the
// background service can read it.
func SaveAPIKey(key string) (string, error) { return saveSecret(credentialKey, key) }

// LoadAPIKey reads the agent key.
func LoadAPIKey() (string, error) { return loadSecret(credentialKey) }

// SaveEngineKey stores the engine's own API key ("" removes it).
func SaveEngineKey(key string) error {
	if key == "" {
		return deleteSecret(engineKeyCredential)
	}
	_, err := saveSecret(engineKeyCredential, key)
	return err
}

// LoadEngineKey reads the engine's own API key; "" when there is none.
func LoadEngineKey() string {
	key, _ := loadSecret(engineKeyCredential)
	return key
}

// The link's keys are kept in sprout's encrypted file store, not the OS
// keyring: the agent runs as a background service (systemd user unit,
// launchd agent), and on a machine nobody is logged in to the keyring is
// locked or unreachable, so a service reading it fails or waits on an unlock
// prompt no one sees.
func saveSecret(name, value string) (string, error) {
	if err := credentials.NewFileBackend().Set(name, value); err != nil {
		return "", fmt.Errorf("storing %s: %w", name, err)
	}
	return "encrypted file store", nil
}

// keyringReadTimeout bounds reading a key an older link left in the OS
// keyring: a locked keyring can wait on an unlock prompt indefinitely.
const keyringReadTimeout = 3 * time.Second

func loadSecret(name string) (string, error) {
	fileValue, fileErr := credentials.NewFileBackend().Get(name)
	if fileErr == nil && fileValue != "" {
		return fileValue, nil
	}
	// A link made before keys moved to the file store kept its key in the
	// keyring: read it there (bounded) and move it, so the service finds it
	// next time.
	type result struct {
		value string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		v, _, err := credentials.GetFromActiveBackend(name)
		done <- result{v, err}
	}()
	select {
	case r := <-done:
		if r.err == nil && r.value != "" {
			_ = credentials.NewFileBackend().Set(name, r.value)
			return r.value, nil
		}
		if fileErr != nil {
			return "", fileErr
		}
		return "", r.err
	case <-time.After(keyringReadTimeout):
		if fileErr != nil {
			return "", fileErr
		}
		return "", fmt.Errorf("reading %s: the OS keyring did not answer (locked, or no login session)", name)
	}
}

// MoveKeysToFileStore copies the link's keys into the file store when an
// older link left them only in the OS keyring. `sprout gpu install` runs it
// from the person's session, where the keyring opens, so the service never
// needs the keyring.
func MoveKeysToFileStore() error {
	if _, err := loadSecret(credentialKey); err != nil {
		return err
	}
	_, _ = loadSecret(engineKeyCredential)
	return nil
}

func deleteSecret(name string) error {
	err := credentials.DeleteFromActiveBackend(name)
	_ = credentials.NewFileBackend().Delete(name)
	return err
}
