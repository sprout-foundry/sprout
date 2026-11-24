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
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// DefaultEngine is Ollama's address, the most common local engine.
const DefaultEngine = "127.0.0.1:11434"

// EngineSinter is the Engine of a link served in-process by sinter, with
// the model in State.Model, instead of by a server at host:port.
const EngineSinter = "sinter"

// State is one link's persistent configuration. The keys are not part of
// it: they live in the credential store, under the link's slot (Slot).
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

// Slot is this link's file-system-safe name: the key its state file,
// credential entries and background service all use.
func (s *State) Slot() string { return Slot(s.Name) }

// Slot reduces a link name to the form files, credential entries and service
// instances key on: letters, digits, dots, dashes and underscores only;
// anything else becomes a dash. It returns "" for a name that reduces to
// nothing (or to a Windows device name), which callers must reject.
func Slot(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-._")
	if s == "" {
		return ""
	}
	if isWindowsDeviceName(s) {
		s += "-link"
	}
	return s
}

// isWindowsDeviceName reports whether s names a DOS device, which Windows
// reserves even with an extension ("CON.json" is the console).
func isWindowsDeviceName(s string) bool {
	base := strings.SplitN(s, ".", 2)[0]
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 {
		rest := strings.ToUpper(base)
		if rest[:3] == "COM" || rest[:3] == "LPT" {
			return rest[3] >= '1' && rest[3] <= '9'
		}
	}
	return false
}

// Dir is where the GPU links keep their state.
func Dir() (string, error) {
	base, err := configuration.GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config dir: %w", err)
	}
	return filepath.Join(base, "gpu"), nil
}

// LinksDir is where one state file per link lives.
func LinksDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "links"), nil
}

func statePath(slot string) (string, error) {
	dir, err := LinksDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, slot+".json"), nil
}

// LoadLinks returns every saved link, sorted by name, migrating a link an
// older sprout saved to the single gpu.json first.
func LoadLinks() ([]*State, error) {
	if err := migrateLegacy(); err != nil {
		return nil, err
	}
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
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		st := &State{Engine: DefaultEngine}
		if err := json.Unmarshal(data, st); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		links = append(links, st)
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Name < links[j].Name })
	return links, nil
}

// LoadLink returns the link with this slot, or an empty one (Linked false)
// when nothing is linked under it.
func LoadLink(slot string) (*State, error) {
	links, err := LoadLinks()
	if err != nil {
		return nil, err
	}
	for _, st := range links {
		if st.Slot() == slot {
			return st, nil
		}
	}
	return &State{Engine: DefaultEngine}, nil
}

// Save writes the state to the link's own file with owner-only permissions.
func (s *State) Save() error {
	slot := s.Slot()
	if slot == "" {
		return fmt.Errorf("link name %q has no usable file name; pick another with --name", s.Name)
	}
	path, err := statePath(slot)
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

// RemoveLink deletes one link's state and both of its keys.
func RemoveLink(slot string) error {
	_ = deleteSecret(AgentKeyName(slot))
	_ = deleteSecret(EngineKeyName(slot))
	path, err := statePath(slot)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// DefaultLinkName picks a new link's name: the machine's hostname, or
// hostname-port when a link under the hostname already exists, so a second
// engine on one machine links without flags and fits the platform's
// unique-name check. Port is the engine's port ("sinter" without one).
func DefaultLinkName(links []*State, engine string) string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "gpu"
	}
	host = strings.TrimSuffix(host, ".local")
	taken := map[string]bool{}
	for _, st := range links {
		taken[st.Slot()] = true
	}
	if !taken[Slot(host)] {
		return host
	}
	if _, port, err := net.SplitHostPort(engine); err == nil && !taken[Slot(host+"-"+port)] {
		return host + "-" + port
	}
	for i := 2; ; i++ {
		if !taken[Slot(fmt.Sprintf("%s-%d", host, i))] {
			return fmt.Sprintf("%s-%d", host, i)
		}
	}
}
