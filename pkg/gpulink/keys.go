package gpulink

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// Legacy entry names an older sprout used for the single link per machine.
// A link found under them is migrated to per-link names on first run.
const (
	legacyAgentKey  = "sprout-gpu-link"   //nolint:gosec // G101: a credential-store entry *name*, not a credential value
	legacyEngineKey = "sprout-gpu-engine" //nolint:gosec // G101: a credential-store entry *name*, not a credential value
)

// AgentKeyName is the credential-store entry holding a link's agent key.
func AgentKeyName(slot string) string { return "sprout-gpu-" + slot + "-link" }

// EngineKeyName is the credential-store entry holding a link's engine key.
func EngineKeyName(slot string) string { return "sprout-gpu-" + slot + "-engine" }

// SaveAPIKeyFor stores a link's agent key in sprout's encrypted file store,
// where the background service can read it.
func SaveAPIKeyFor(slot, key string) (string, error) { return saveSecret(AgentKeyName(slot), key) }

// LoadAPIKeyFor reads a link's agent key, following one left by an older
// sprout under the single-link name. The legacy fallback only applies while
// this machine has at most one link: with several, the legacy key's owner
// is unknown and reading it into the wrong link would cross-wire their
// identities.
func LoadAPIKeyFor(slot string) (string, error) {
	key, err := loadSecret(AgentKeyName(slot))
	if err == nil && key != "" {
		return key, nil
	}
	if !soleLink(slot) {
		return "", err
	}
	legacy, legacyErr := loadSecret(legacyAgentKey)
	if legacyErr == nil && legacy != "" {
		_, _ = saveSecret(AgentKeyName(slot), legacy)
		return legacy, nil
	}
	return "", err
}

// soleLink reports whether slot is the only link on this machine.
func soleLink(slot string) bool {
	links, err := LoadLinks()
	if err != nil {
		return false
	}
	return len(links) == 1 && links[0].Slot() == slot
}

// SaveEngineKeyFor stores a link's engine key ("" removes it).
func SaveEngineKeyFor(slot, key string) error {
	if key == "" {
		return deleteSecret(EngineKeyName(slot))
	}
	_, err := saveSecret(EngineKeyName(slot), key)
	return err
}

// LoadEngineKeyFor reads a link's engine key; "" when there is none. The
// legacy fallback only applies while this is the machine's only link (see
// LoadAPIKeyFor).
func LoadEngineKeyFor(slot string) string {
	key, _ := loadSecret(EngineKeyName(slot))
	if key != "" {
		return key
	}
	if !soleLink(slot) {
		return ""
	}
	legacy, _ := loadSecret(legacyEngineKey)
	return legacy
}

// MoveKeysToFileStore copies links' keys into the file store when an older
// link left them only in the OS keyring. `sprout gpu install` runs it from
// the person's session, where the keyring opens, so the service never needs
// the keyring.
func MoveKeysToFileStore() error {
	links, err := LoadLinks()
	if err != nil {
		return err
	}
	for _, st := range links {
		key, err := LoadAPIKeyFor(st.Slot())
		if err != nil {
			return err
		}
		if key == "" {
			return fmt.Errorf("the link %q has no agent key in the credential store; run `sprout gpu link` again", st.Name)
		}
		_ = LoadEngineKeyFor(st.Slot())
	}
	return nil
}

// migrateLegacy moves the single gpu.json and its single-named keys, written
// by an older sprout, to the per-link layout. It runs before every state
// read, so today's link keeps working without relinking.
func migrateLegacy() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	oldPath := filepath.Join(dir, "gpu.json")
	data, err := os.ReadFile(oldPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", oldPath, err)
	}
	st := &State{Engine: DefaultEngine}
	if err := json.Unmarshal(data, st); err != nil {
		return fmt.Errorf("parsing %s: %w", oldPath, err)
	}
	// A name that reduces to nothing (or to a Windows device name) cannot
	// name a state file or service instance; give the link a usable one
	// rather than failing every gpu command until the file is hand-deleted.
	// Taken slots come from the links dir directly: LoadLinks would
	// re-enter this migration while gpu.json is still on disk.
	if st.Name == "" || Slot(st.Name) == "" {
		st.Name = "gpu"
		if slots, err := takenSlots(); err == nil {
			st.Name = DefaultLinkName(slots, st.Engine)
		}
	}
	// A per-link file at this slot (a link made after a downgrade, say) is
	// newer than the legacy one; keep it and drop only the legacy file. The
	// links dir is read directly: LoadLink would re-enter this migration
	// while gpu.json is still on disk. saveAt is used for the same reason —
	// Save's guard reads the links dir being created here.
	path, err := statePath(st.Slot())
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil {
		have := &State{Engine: DefaultEngine}
		if json.Unmarshal(existing, have) == nil && have.Linked() {
			return nil
		}
	}
	if err := st.saveAt(st.Slot()); err != nil {
		return err
	}
	// Keys move under the link's own names; the legacy entries go once the
	// new ones hold the keys (a key still in the OS keyring is picked up
	// there, and stays as fallback when the file store refuses the write).
	if key, err := loadSecret(legacyAgentKey); err == nil && key != "" {
		_, _ = saveSecret(AgentKeyName(st.Slot()), key)
		if moved, _ := loadSecret(AgentKeyName(st.Slot())); moved == key {
			_ = deleteSecret(legacyAgentKey)
		}
	}
	if engineKey, err := loadSecret(legacyEngineKey); err == nil && engineKey != "" {
		_, _ = saveSecret(EngineKeyName(st.Slot()), engineKey)
		if moved, _ := loadSecret(EngineKeyName(st.Slot())); moved == engineKey {
			_ = deleteSecret(legacyEngineKey)
		}
	}
	if err := os.Remove(oldPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing %s: %w", oldPath, err)
	}
	return nil
}

// takenSlots lists the slots of the per-link state files currently on disk,
// without triggering the legacy migration (migrateLegacy uses it while
// gpu.json is still on disk).
func takenSlots() ([]*State, error) {
	dir, err := LinksDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading gpu link dir: %w", err)
	}
	var links []*State
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		st := &State{Engine: DefaultEngine}
		if json.Unmarshal(data, st) == nil && st.Linked() {
			links = append(links, st)
		}
	}
	return links, nil
}

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
	// A key made before keys moved to the file store lives in the OS
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

func deleteSecret(name string) error {
	err := credentials.DeleteFromActiveBackend(name)
	_ = credentials.NewFileBackend().Delete(name)
	return err
}
