// Command generate_studio_providers derives the STUDIO_PROVIDERS snapshot
// embedded in the studio bridge (sprout-studio/shared/studio-bridge.js) from
// the provider registry, eliminating the hand-maintained copy that had
// drifted from the real provider configs.
//
// Sources of truth:
//   - pkg/providercatalog/providers.json — UX metadata (display name,
//     description, docs/signup URLs, API-key copy, recommended model + why).
//     Parsed from disk into providercatalog.Catalog rather than read via
//     providercatalog.Current(): Current() schedules an async refresh from
//     the remote catalog in non-test binaries, so its output can differ run
//     to run. A generator must be deterministic, so we read the checked-in
//     file — which is exactly what go:embed compiles in anyway.
//   - pkg/agent_providers/configs/*.json — auth (env var / requires-key),
//     display name, and the model list (models.model_info when present,
//     else the legacy models.available_models array).
//
// Provider order follows the catalog order. Fields the sources don't carry
// are emitted as empty strings (never omitted) so consumers always see the
// full 14-field shape.
//
// Usage:
//
//	go run ./cmd/generate_studio_providers -bridge ../sprout-studio/shared/studio-bridge.js
//	go run ./cmd/generate_studio_providers -o /tmp/studio-providers.json
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// studioProvider is the object shape embedded in the bridge's
// `var STUDIO_PROVIDERS = [...];` line. Field order is load-bearing for
// diff review only — the bridge reads it as JSON — but every field is
// always emitted (no omitempty) because the bridge indexes into
// p.models.length and friends directly.
type studioProvider struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	EnvVar              string   `json:"envVar"`
	RequiresKey         bool     `json:"requiresKey"`
	Recommended         bool     `json:"recommended"`
	Description         string   `json:"description"`
	DocsURL             string   `json:"docsUrl"`
	SignupURL           string   `json:"signupUrl"`
	APIKeyLabel         string   `json:"apiKeyLabel"`
	APIKeyHelp          string   `json:"apiKeyHelp"`
	SetupHint           string   `json:"setupHint"`
	RecommendedModel    string   `json:"recommendedModel"`
	RecommendedModelWhy string   `json:"recommendedModelWhy"`
	Models              []string `json:"models"`
}

// openRouterMetaSet filters out OpenRouter routing aliases that are not real
// models. Mirrors the denylist in cmd/refresh_provider_catalog (both are
// package main, so the set is duplicated rather than shared).
var openRouterMetaSet = map[string]bool{
	"openrouter/auto":        true,
	"openrouter/auto-beta":   true,
	"openrouter/bodybuilder": true,
	"openrouter/fusion":      true,
	"openrouter/pareto-code": true,
}

// bridgeMarker identifies the snapshot line; bridgeLineRE decomposes it so
// the rewrite preserves the leading indent and anything after the closing
// `;` (e.g. a trailing comment or CR from a CRLF file).
const bridgeMarker = "var STUDIO_PROVIDERS"

var bridgeLineRE = regexp.MustCompile(`^(\s*var STUDIO_PROVIDERS = )\[.*\](;.*)$`)

func main() {
	bridgePath := flag.String("bridge", "", "rewrite ONLY the `var STUDIO_PROVIDERS = [...];` line in this JS file, in place")
	outPath := flag.String("o", "", "write the snapshot as a pretty-printed JSON array to this path (for inspection/tests)")
	configsDir := flag.String("configs", filepath.Join("pkg", "agent_providers", "configs"), "directory containing the per-provider config JSON files")
	catalogPath := flag.String("catalog", filepath.Join("pkg", "providercatalog", "providers.json"), "path to the provider catalog JSON")
	flag.Parse()

	if *bridgePath == "" && *outPath == "" {
		failf("nothing to do: pass -bridge <studio-bridge.js> and/or -o <output.json>")
	}

	snapshot, err := buildStudioProviders(*configsDir, *catalogPath)
	if err != nil {
		failf("build provider snapshot: %v", err)
	}

	totalModels := 0
	for _, p := range snapshot {
		totalModels += len(p.Models)
	}
	fmt.Printf("derived %d providers / %d models from %s + %s\n", len(snapshot), totalModels, *configsDir, *catalogPath)

	if *outPath != "" {
		if err := writeJSONFile(*outPath, snapshot); err != nil {
			failf("write %s: %v", *outPath, err)
		}
		fmt.Printf("wrote %s\n", *outPath)
	}

	if *bridgePath != "" {
		if err := rewriteBridgeSnapshot(*bridgePath, snapshot); err != nil {
			failf("rewrite %s: %v", *bridgePath, err)
		}
		fmt.Printf("rewrote STUDIO_PROVIDERS line in %s\n", *bridgePath)
	}
}

// buildStudioProviders derives one studioProvider per catalog entry, in
// catalog order. Config values (auth, models) win over catalog metadata
// because they are what the running client actually uses; the catalog
// supplies the curated UX copy.
func buildStudioProviders(configsDir, catalogPath string) ([]studioProvider, error) {
	catalog, err := loadCatalog(catalogPath)
	if err != nil {
		return nil, err
	}

	catalogByID := make(map[string]bool, len(catalog.Providers))
	for _, cp := range catalog.Providers {
		catalogByID[cp.ID] = true
	}

	// Warn (but don't fail) on configs the catalog doesn't curate — they'd
	// be silently dropped from the snapshot otherwise.
	if entries, err := os.ReadDir(configsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			if !catalogByID[id] {
				fmt.Fprintf(os.Stderr, "warn: config %s has no catalog entry; skipping\n", entry.Name())
			}
		}
	}

	snapshot := make([]studioProvider, 0, len(catalog.Providers))
	for _, cp := range catalog.Providers {
		cfg, err := providers.LoadProviderConfig(filepath.Join(configsDir, cp.ID+".json"))
		if err != nil {
			return nil, fmt.Errorf("load config for %s: %w", cp.ID, err)
		}

		// Auth derivation mirrors configuration.GetProviderAuthMetadata:
		// an explicit "none" (or absent) auth type means no key is needed.
		requiresKey := cfg.Auth.Type != "" && cfg.Auth.Type != "none"

		modelIDs := configModelIDs(cfg)
		if len(modelIDs) == 0 {
			// No models declared in the config (LM Studio lists them from a
			// live server; sprout-local/cerebras carry them only in the
			// catalog) — fall back to the catalog so model pickers that have
			// no live endpoint still get a list.
			modelIDs = catalogModelIDs(cp)
		}
		models := normalizeModelIDs(modelIDs)

		name := strings.TrimSpace(cp.Name)
		if name == "" {
			name = strings.TrimSpace(cfg.DisplayName)
		}
		if name == "" {
			name = cp.ID
		}

		recommended := strings.TrimSpace(cp.RecommendedModel)
		if recommended == "" {
			recommended = strings.TrimSpace(cp.DefaultModel)
		}
		if recommended == "" && len(models) > 0 {
			recommended = models[0]
		}

		snapshot = append(snapshot, studioProvider{
			ID:                  cp.ID,
			Name:                name,
			EnvVar:              strings.TrimSpace(cfg.Auth.EnvVar),
			RequiresKey:         requiresKey,
			Recommended:         cp.Recommended,
			Description:         strings.TrimSpace(cp.Description),
			DocsURL:             strings.TrimSpace(cp.DocsURL),
			SignupURL:           strings.TrimSpace(cp.SignupURL),
			APIKeyLabel:         strings.TrimSpace(cp.APIKeyLabel),
			APIKeyHelp:          strings.TrimSpace(cp.APIKeyHelp),
			SetupHint:           strings.TrimSpace(cp.SetupHint),
			RecommendedModel:    recommended,
			RecommendedModelWhy: strings.TrimSpace(cp.RecommendedModelWhy),
			Models:              models,
		})
	}

	return snapshot, nil
}

// loadCatalog reads the checked-in catalog JSON. Deterministic by design —
// see the package comment for why providercatalog.Current() isn't used.
func loadCatalog(catalogPath string) (providercatalog.Catalog, error) {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return providercatalog.Catalog{}, fmt.Errorf("read catalog: %w", err)
	}

	var catalog providercatalog.Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return providercatalog.Catalog{}, fmt.Errorf("parse catalog %s: %w", catalogPath, err)
	}
	if len(catalog.Providers) == 0 {
		return providercatalog.Catalog{}, fmt.Errorf("catalog %s contains no providers", catalogPath)
	}
	return catalog, nil
}

// configModelIDs extracts the ordered model IDs from a provider config.
// models.model_info (the rich form: id + metadata per model) is preferred;
// the legacy models.available_models string array is the fallback. Models
// with negative cost are sentinel values (OpenRouter variable-pricing
// meta-models) and are skipped, matching normalizeModels in
// cmd/refresh_provider_catalog.
func configModelIDs(cfg *providers.ProviderConfig) []string {
	infos := cfg.Models.ModelInfo
	if len(infos) == 0 {
		return append([]string(nil), cfg.Models.AvailableModels...)
	}

	ids := make([]string, 0, len(infos))
	for _, mi := range infos {
		if mi.InputCost < 0 || mi.OutputCost < 0 {
			continue
		}
		ids = append(ids, mi.ID)
	}
	return ids
}

// catalogModelIDs extracts the ordered model IDs from a catalog provider
// entry. Same negative-cost filter as configModelIDs.
func catalogModelIDs(cp providercatalog.Provider) []string {
	ids := make([]string, 0, len(cp.Models))
	for _, m := range cp.Models {
		if m.InputCost < 0 || m.OutputCost < 0 {
			continue
		}
		ids = append(ids, m.ID)
	}
	return ids
}

// normalizeModelIDs trims, drops blanks / OpenRouter routing aliases /
// duplicates, then sorts case-insensitively for a stable, reviewable diff.
// The result is always non-nil so it marshals as [] rather than null — the
// bridge calls p.models.length on every entry.
func normalizeModelIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || openRouterMetaSet[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

// rewriteBridgeSnapshot replaces ONLY the `var STUDIO_PROVIDERS = [...];`
// line in path, preserving every other byte of the file (including the
// target line's indentation and any trailing content). It fails when the
// line is absent or appears more than once, so a renamed or duplicated
// marker can never silently pass.
func rewriteBridgeSnapshot(path string, snapshot []studioProvider) error {
	encoded, err := marshalJSON(snapshot, "")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	if bytes.ContainsAny(encoded, "\n\r") {
		return fmt.Errorf("internal error: compact snapshot is not single-line")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read bridge: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat bridge: %w", err)
	}

	// Split/Join on "\n" round-trips the file exactly: a trailing newline
	// yields a final empty element, and any "\r" (CRLF file) stays inside
	// the line content where the regex preserves it.
	lines := strings.Split(string(data), "\n")

	markerIdx := -1
	for i, line := range lines {
		if !strings.Contains(line, bridgeMarker) {
			continue
		}
		if markerIdx != -1 {
			return fmt.Errorf("found %s more than once in %s", bridgeMarker, path)
		}
		markerIdx = i
	}
	if markerIdx == -1 {
		return fmt.Errorf("could not find %s in %s", bridgeMarker, path)
	}

	match := bridgeLineRE.FindStringSubmatch(lines[markerIdx])
	if match == nil {
		return fmt.Errorf("line %d does not match expected `%s = [...];` shape", markerIdx+1, bridgeMarker)
	}

	lines[markerIdx] = match[1] + string(encoded) + match[2]
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), info.Mode().Perm()); err != nil {
		return fmt.Errorf("write bridge: %w", err)
	}
	return nil
}

// writeJSONFile writes the snapshot as a pretty-printed (2-space) JSON array.
func writeJSONFile(path string, snapshot []studioProvider) error {
	encoded, err := marshalJSON(snapshot, "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

// marshalJSON encodes v with HTML escaping disabled so the embedded snapshot
// stays plain JS-friendly text (JSON.stringify-style) instead of sprouting
// \u003c escapes if copy ever gains a <, >, or &.
func marshalJSON(v interface{}, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func failf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
