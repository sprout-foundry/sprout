package skills

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

//go:embed library/registry.json
var registryJSON []byte

// RegistryEntry is a single starter skill in the embedded registry.
type RegistryEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	GitURL      string `json:"git_url"`
	GitRef      string `json:"git_ref"`
	PathInRepo  string `json:"path_in_repo"`
}

// Registry is the decoded embedded registry.
type Registry struct {
	Version int             `json:"version"`
	Skills  []RegistryEntry `json:"skills"`
}

var (
	testRegistryOverrideMu sync.Mutex
	testRegistryOverride   *Registry
)

var (
	loadRegistryOnce sync.Once
	cachedRegistry   Registry
	loadRegistryErr  error
)

// LoadRegistry returns the embedded registry, decoded once.
func LoadRegistry() (*Registry, error) {
	loadRegistryOnce.Do(func() {
		if err := json.Unmarshal(registryJSON, &cachedRegistry); err != nil {
			loadRegistryErr = fmt.Errorf("decode embedded registry: %w", err)
		}
	})
	if loadRegistryErr != nil {
		return nil, loadRegistryErr
	}
	return &cachedRegistry, nil
}

// ErrRegistryNotFound is returned when a registry ID is not present.
var ErrRegistryNotFound = errors.New("registry entry not found")

// LookupByID returns the registry entry for the given ID, or ErrRegistryNotFound.
func (r *Registry) LookupByID(id string) (*RegistryEntry, error) {
	for i := range r.Skills {
		if r.Skills[i].ID == id {
			return &r.Skills[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrRegistryNotFound, id)
}

// RegistryOverrideForTest allows tests to inject a registry instead of the
// embedded default. Pass nil to clear. Only intended for use in *_test.go.
//
// Safe to call from parallel tests: the override is protected by a mutex.
// Use the defer pattern to ensure cleanup:
//
//	RegistryOverrideForTest(fakeReg)
//	defer RegistryOverrideForTest(nil)
func RegistryOverrideForTest(r *Registry) {
	testRegistryOverrideMu.Lock()
	defer testRegistryOverrideMu.Unlock()
	testRegistryOverride = r
}

// effectiveRegistry returns the test override if set, else the embedded one.
// The bool indicates whether a test override is active (true) or the embedded
// registry was used (false). This avoids callers re-reading the unprotected
// variable, which would be a data race.
func effectiveRegistry() (*Registry, bool, error) {
	testRegistryOverrideMu.Lock()
	override := testRegistryOverride
	testRegistryOverrideMu.Unlock()
	if override != nil {
		return override, true, nil
	}
	reg, err := LoadRegistry()
	return reg, false, err
}
