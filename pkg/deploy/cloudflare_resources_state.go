// Deploy resource state for the Cloudflare Workers adapter.
//
// A Workers deploy creates account resources (D1 databases, KV namespaces, R2
// buckets) that must be reused on a re-run rather than duplicated, and must
// never be shared between two projects by accident. The adapter records the
// resources it created in a per-project state file under .sprout/, keyed by a
// stable resource name derived from the worker name — not the display name
// alone — so a second deploy finds the same resources and a different project
// gets its own.
//
// The file holds only resource ids and names; it never holds a credential.

package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeployResourceStateName is the on-disk file name of the deploy resource
// state, relative to the project's .sprout/ directory.
const DeployResourceStateName = "deploy-resources.json"

// deployResourceStateVersion is the schema version of the state file, so a
// later shape can be detected rather than misread.
const deployResourceStateVersion = 1

// DeployResourceState records the account resources a project's Workers deploy
// created, so a re-run reuses them. It is keyed by the stable resource name
// (see stableResourceName), so a resource is found even if the display name
// changes.
type DeployResourceState struct {
	// Version is the schema version of the file.
	Version int `json:"version"`
	// D1 maps a stable resource name to the D1 database id.
	D1 map[string]string `json:"d1,omitempty"`
	// KV maps a stable resource name to the KV namespace id.
	KV map[string]string `json:"kv,omitempty"`
	// R2 maps a stable resource name to the R2 bucket name.
	R2 map[string]string `json:"r2,omitempty"`
}

// emptyDeployResourceState returns a state with its maps initialized, so a
// caller can record into it without a nil-map check.
func emptyDeployResourceState() *DeployResourceState {
	return &DeployResourceState{
		Version: deployResourceStateVersion,
		D1:      map[string]string{},
		KV:      map[string]string{},
		R2:      map[string]string{},
	}
}

// deployResourceStatePath returns the path of the state file under a project
// root. It joins through the .sprout/ state directory name so the directory
// has a single home.
func deployResourceStatePath(root string) string {
	return filepath.Join(strings.TrimSpace(root), ".sprout", DeployResourceStateName)
}

// LoadDeployResourceState reads the state file under root. A missing file is
// the normal first-deploy case and yields an empty state (not an error); a
// corrupt file is an error, because silently starting over would risk creating
// duplicate resources.
func LoadDeployResourceState(root string) (*DeployResourceState, error) {
	path := deployResourceStatePath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyDeployResourceState(), nil
		}
		return nil, fmt.Errorf("cloudflare: read deploy resource state %s: %w", path, err)
	}
	state := emptyDeployResourceState()
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("cloudflare: parse deploy resource state %s: %w", path, err)
	}
	if state.D1 == nil {
		state.D1 = map[string]string{}
	}
	if state.KV == nil {
		state.KV = map[string]string{}
	}
	if state.R2 == nil {
		state.R2 = map[string]string{}
	}
	return state, nil
}

// save writes the state file under root, creating .sprout/ when needed.
func (s *DeployResourceState) save(root string) error {
	s.Version = deployResourceStateVersion
	path := deployResourceStatePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cloudflare: create state directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("cloudflare: encode deploy resource state: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("cloudflare: write deploy resource state %s: %w", path, err)
	}
	return nil
}

// stableResourceName builds the account-unique name for a resource from the
// worker name and a stable suffix. The suffix is derived from the worker name
// rather than the display name, so two projects with the same display name
// (say, two "My App" projects) still get distinct resources, and a re-run of
// the same project reproduces the same name.
func stableResourceName(worker, suffix string) string {
	worker = strings.TrimSpace(worker)
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return worker
	}
	return worker + "-" + suffix
}
