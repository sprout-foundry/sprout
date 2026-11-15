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

// State is the link's persistent configuration. The keys are not part of
// it: they live in the credential store.
type State struct {
	PlatformURL string `json:"platform_url"`
	LinkID      string `json:"link_id"`
	Name        string `json:"name"`
	// Engine is the engine's host:port as this machine reaches it.
	Engine string `json:"engine"`
}

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

// SaveAPIKey stores the agent key. The platform issues it once, so when the
// OS keyring refuses it falls back to sprout's encrypted file store.
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

func saveSecret(name, value string) (string, error) {
	if err := credentials.SetToActiveBackend(name, value); err == nil {
		return "credential store", nil
	} else if fileErr := credentials.NewFileBackend().Set(name, value); fileErr != nil {
		return "", fmt.Errorf("%w; file fallback: %v", err, fileErr)
	}
	return "encrypted file (the OS keyring was unavailable)", nil
}

func loadSecret(name string) (string, error) {
	value, _, err := credentials.GetFromActiveBackend(name)
	if err == nil && value != "" {
		return value, nil
	}
	if fileValue, fileErr := credentials.NewFileBackend().Get(name); fileErr == nil && fileValue != "" {
		return fileValue, nil
	}
	return value, err
}

func deleteSecret(name string) error {
	err := credentials.DeleteFromActiveBackend(name)
	_ = credentials.NewFileBackend().Delete(name)
	return err
}
