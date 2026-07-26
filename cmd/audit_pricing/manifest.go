package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// PricingEntry is the verified pricing for a single model.
type PricingEntry struct {
	ID             string  `json:"id"`
	InputPerMTok   float64 `json:"input_per_mtok"`
	OutputPerMTok  float64 `json:"output_per_mtok"`
	CachedPerMTok  float64 `json:"cached_per_mtok,omitempty"`
}

// ProviderManifest is the verified pricing for a provider's models.
type ProviderManifest struct {
	// Source is the official docs URL where pricing was verified.
	Source string `json:"source"`
	// LastVerified is when a human or agent last checked the source.
	LastVerified string         `json:"last_verified"`
	Models       []PricingEntry `json:"models"`
}

//go:embed manifest.json
var manifestJSON []byte

// manifests is the authoritative pricing truth keyed by provider ID.
// Loaded from the embedded manifest.json file. When providers change pricing,
// a human or the --fetch mode updates this file and the audit catches config drift.
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

