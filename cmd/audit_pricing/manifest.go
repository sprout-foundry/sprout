package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
)

// PricingEntry is the verified pricing for a single model.
type PricingEntry struct {
	ID            string  `json:"id"`
	InputPerMTok  float64 `json:"input_per_mtok"`
	OutputPerMTok float64 `json:"output_per_mtok"`
	CachedPerMTok float64 `json:"cached_per_mtok,omitempty"`
}

// ProviderManifest is the verified pricing for a provider's models.
type ProviderManifest struct {
	// Source is the official docs URL where pricing was verified.
	Source string `json:"source"`
	// LastVerified is when a human or agent last checked the source.
	LastVerified string         `json:"last_verified"`
	Models       []PricingEntry `json:"models"`
	// Retired lists model IDs the provider has confirmed removed from its API.
	// Discovery must not re-add them: OpenRouter's mirror still lists some
	// retired models under the original org prefix because other hosts (e.g.
	// Azure) continue to serve them, which would restore dead entries with
	// the wrong provider's prices.
	Retired []string `json:"retired,omitempty"`
}

//go:embed manifest.json
var manifestJSON []byte

// manifests is the authoritative pricing truth keyed by provider ID.
// Loaded from the embedded manifest.json file. When providers change pricing,
// a human or the --discover mode updates this file and the audit catches config drift.
var manifests = loadManifest()

func loadManifest() map[string]ProviderManifest {
	var m map[string]ProviderManifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		panic(fmt.Sprintf("audit_pricing: failed to parse embedded manifest.json: %v", err))
	}
	if m == nil {
		m = make(map[string]ProviderManifest)
	}
	return m
}

// saveManifest writes the full manifest map back to disk as indented JSON.
func saveManifest(path string, m map[string]ProviderManifest) error {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}
