package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// ---------------------------------------------------------------------------
// comparePricing
// ---------------------------------------------------------------------------

func TestComparePricing_NoDrift(t *testing.T) {
	d := comparePricing(
		PricingEntry{ID: "m", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
		providers.ModelInfo{ID: "m", InputCost: 0.14, OutputCost: 0.28, CachedCost: 0.0028},
	)
	if len(d) != 0 {
		t.Errorf("expected no drifts, got %d: %v", len(d), d)
	}
}

func TestComparePricing_SingleFieldDrift(t *testing.T) {
	tests := []struct {
		name     string
		manifest PricingEntry
		config   providers.ModelInfo
		want     string
	}{
		{"input", PricingEntry{ID: "m", InputPerMTok: 0.14, OutputPerMTok: 0.28},
			providers.ModelInfo{ID: "m", InputCost: 0.99, OutputCost: 0.28}, "input_cost"},
		{"output", PricingEntry{ID: "m", InputPerMTok: 0.14, OutputPerMTok: 0.28},
			providers.ModelInfo{ID: "m", InputCost: 0.14, OutputCost: 0.99}, "output_cost"},
		{"cached", PricingEntry{ID: "m", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
			providers.ModelInfo{ID: "m", InputCost: 0.14, OutputCost: 0.28, CachedCost: 0.999}, "cached_input_cost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := comparePricing(tt.manifest, tt.config)
			if len(d) != 1 || d[0].Field != tt.want {
				t.Errorf("expected %s drift, got %v", tt.want, d)
			}
		})
	}
}

func TestComparePricing_AllFieldsDrift(t *testing.T) {
	d := comparePricing(
		PricingEntry{ID: "m", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
		providers.ModelInfo{ID: "m", InputCost: 1.0, OutputCost: 2.0, CachedCost: 3.0},
	)
	if len(d) != 3 {
		t.Fatalf("expected 3 drifts, got %d", len(d))
	}
	want := []string{"input_cost", "output_cost", "cached_input_cost"}
	for i, d := range d {
		if d.Field != want[i] {
			t.Errorf("drift[%d] = %q, want %q", i, d.Field, want[i])
		}
	}
}

func TestComparePricing_ZeroCachedInManifest_NoDrift(t *testing.T) {
	d := comparePricing(
		PricingEntry{ID: "m", InputPerMTok: 0.14, OutputPerMTok: 0.28},
		providers.ModelInfo{ID: "m", InputCost: 0.14, OutputCost: 0.28, CachedCost: 0.5},
	)
	if len(d) != 0 {
		t.Errorf("zero cached in manifest should not report drift, got %v", d)
	}
}

func TestComparePricing_EpsilonEquality(t *testing.T) {
	d := comparePricing(
		PricingEntry{ID: "m", InputPerMTok: 0.14, OutputPerMTok: 0.28},
		providers.ModelInfo{ID: "m", InputCost: 0.14 + 1e-10, OutputCost: 0.28 + 1e-10},
	)
	if len(d) != 0 {
		t.Errorf("epsilon-equal values should not drift, got %v", d)
	}
}

// ---------------------------------------------------------------------------
// Helper: write a minimal provider config JSON
// ---------------------------------------------------------------------------

func writeTestConfig(t *testing.T, dir, providerID string, models []providers.ModelInfo) string {
	t.Helper()
	cfg := providers.ProviderConfig{
		Name: providerID, Endpoint: "https://api.example.com/v1",
		Auth:   providers.AuthConfig{Type: "none"},
		Models: providers.ModelConfig{DefaultContextLimit: 100000, ModelInfo: models},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, providerID+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// auditProvider
// ---------------------------------------------------------------------------

func TestAuditProvider_AllVerified(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1", InputCost: 0.14, OutputCost: 0.28, CachedCost: 0.0028},
		{ID: "m2", InputCost: 1.0, OutputCost: 2.0},
	})
	manifest := ProviderManifest{Source: "https://x.com", Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
		{ID: "m2", InputPerMTok: 1.0, OutputPerMTok: 2.0},
	}}
	result := auditProvider("p", manifest, filepath.Join(dir, "p.json"))
	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result.Models))
	}
	for _, m := range result.Models {
		if m.Status != "verified" {
			t.Errorf("%s = %q, want verified", m.ModelID, m.Status)
		}
	}
}

func TestAuditProvider_OneDrift(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1", InputCost: 0.14, OutputCost: 0.28},
		{ID: "m2", InputCost: 99.0, OutputCost: 0.28},
	})
	manifest := ProviderManifest{Source: "https://x.com", Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28},
		{ID: "m2", InputPerMTok: 0.14, OutputPerMTok: 0.28},
	}}
	result := auditProvider("p", manifest, filepath.Join(dir, "p.json"))
	sm := make(map[string]string)
	for _, m := range result.Models {
		sm[m.ModelID] = m.Status
	}
	if sm["m1"] != "verified" || sm["m2"] != "drift" {
		t.Errorf("m1=%q m2=%q, want verified/drift", sm["m1"], sm["m2"])
	}
}

func TestAuditProvider_MissingInConfig(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1", InputCost: 0.14, OutputCost: 0.28},
	})
	manifest := ProviderManifest{Source: "https://x.com", Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28},
		{ID: "m2", InputPerMTok: 1.0, OutputPerMTok: 2.0},
	}}
	result := auditProvider("p", manifest, filepath.Join(dir, "p.json"))
	sm := make(map[string]string)
	for _, m := range result.Models {
		sm[m.ModelID] = m.Status
	}
	if sm["m2"] != "missing_in_config" {
		t.Errorf("m2 = %q, want missing_in_config", sm["m2"])
	}
}

func TestAuditProvider_MissingInManifest(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1", InputCost: 0.14, OutputCost: 0.28},
		{ID: "m3", InputCost: 5.0, OutputCost: 10.0},
	})
	manifest := ProviderManifest{Source: "https://x.com", Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28},
	}}
	result := auditProvider("p", manifest, filepath.Join(dir, "p.json"))
	sm := make(map[string]string)
	for _, m := range result.Models {
		sm[m.ModelID] = m.Status
	}
	if sm["m3"] != "missing_in_manifest" {
		t.Errorf("m3 = %q, want missing_in_manifest", sm["m3"])
	}
}

func TestAuditProvider_ConfigNotFound(t *testing.T) {
	manifest := ProviderManifest{Source: "https://x.com", Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14}, {ID: "m2", InputPerMTok: 1.0},
	}}
	result := auditProvider("p", manifest, "/nonexistent.json")
	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result.Models))
	}
	for _, m := range result.Models {
		if m.Status != "missing_in_config" {
			t.Errorf("%s = %q, want missing_in_config", m.ModelID, m.Status)
		}
	}
}

func TestAuditProvider_SortedOutput(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "z-model", InputCost: 1.0, OutputCost: 2.0},
		{ID: "a-model", InputCost: 0.5, OutputCost: 1.0},
		{ID: "B-model", InputCost: 0.3, OutputCost: 0.6},
	})
	manifest := ProviderManifest{Source: "https://x.com", Models: []PricingEntry{
		{ID: "z-model", InputPerMTok: 1.0, OutputPerMTok: 2.0},
		{ID: "a-model", InputPerMTok: 0.5, OutputPerMTok: 1.0},
		{ID: "B-model", InputPerMTok: 0.3, OutputPerMTok: 0.6},
	}}
	result := auditProvider("p", manifest, filepath.Join(dir, "p.json"))
	want := []string{"a-model", "B-model", "z-model"}
	if len(result.Models) != len(want) {
		t.Fatalf("expected %d models, got %d", len(want), len(result.Models))
	}
	for i, w := range want {
		if result.Models[i].ModelID != w {
			t.Errorf("models[%d] = %q, want %q", i, result.Models[i].ModelID, w)
		}
	}
}

func TestAuditProvider_PricingFieldsPopulated(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1", InputCost: 0.14, OutputCost: 0.28, CachedCost: 0.0028},
		{ID: "m2", InputCost: 5.0, OutputCost: 10.0},
	})
	manifest := ProviderManifest{Source: "https://x.com", Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
		{ID: "m2", InputPerMTok: 5.0, OutputPerMTok: 10.0},
		{ID: "m3", InputPerMTok: 3.0, OutputPerMTok: 6.0, CachedPerMTok: 0.1},
	}}
	result := auditProvider("p", manifest, filepath.Join(dir, "p.json"))
	sm := make(map[string]ModelAudit)
	for _, m := range result.Models {
		sm[m.ModelID] = m
	}
	// Verified: pricing from config
	if m := sm["m1"]; m.InputCost != 0.14 || m.OutputCost != 0.28 || m.CachedCost != 0.0028 {
		t.Errorf("m1 pricing wrong: in=%f out=%f cached=%f", m.InputCost, m.OutputCost, m.CachedCost)
	}
	// missing_in_config: pricing from manifest
	if m := sm["m3"]; m.InputCost != 3.0 || m.OutputCost != 6.0 || m.CachedCost != 0.1 {
		t.Errorf("m3 pricing wrong: in=%f out=%f cached=%f", m.InputCost, m.OutputCost, m.CachedCost)
	}
}

// ---------------------------------------------------------------------------
// safeID
// ---------------------------------------------------------------------------

func TestSafeID(t *testing.T) {
	tests := []struct {
		entry map[string]interface{}
		want  string
		ok    bool
	}{
		{map[string]interface{}{"id": "abc"}, "abc", true},
		{map[string]interface{}{"id": nil}, "", false},
		{map[string]interface{}{"id": 123}, "", false},
		{map[string]interface{}{}, "", false},
		{map[string]interface{}{"name": "x"}, "", false},
	}
	for _, tt := range tests {
		got, ok := safeID(tt.entry)
		if got != tt.want || ok != tt.ok {
			t.Errorf("safeID(%v) = %q, %v; want %q, %v", tt.entry, got, ok, tt.want, tt.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// formatReport
// ---------------------------------------------------------------------------

func TestFormatReport_SymbolsAndSummary(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "tp", []providers.ModelInfo{
		{ID: "m1", InputCost: 0.14, OutputCost: 0.28, CachedCost: 0.0028},
		{ID: "m2", InputCost: 99.0, OutputCost: 0.28},
	})
	manifest := ProviderManifest{Source: "https://x.com", Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
		{ID: "m2", InputPerMTok: 0.14, OutputPerMTok: 0.28},
	}}
	result := auditProvider("tp", manifest, filepath.Join(dir, "tp.json"))
	report := formatReport([]ProviderAuditResult{result}, dir)

	for _, sym := range []string{"✓", "✗"} {
		if !strings.Contains(report, sym) {
			t.Errorf("missing %s in report: %s", sym, report)
		}
	}
	for _, sub := range []string{"tp", "https://x.com", "Summary:", "1 verified", "1 drift"} {
		if !strings.Contains(report, sub) {
			t.Errorf("missing %q in report: %s", sub, report)
		}
	}
}

func TestFormatReport_MissingSymbols(t *testing.T) {
	// ⚠ for missing_in_config (uses pricing from ModelAudit fields directly)
	r1 := formatReport([]ProviderAuditResult{
		{ProviderID: "p", Source: "https://x.com",
			Models: []ModelAudit{{ModelID: "m1", Status: "missing_in_config",
				InputCost: 0.14, OutputCost: 0.28, CachedCost: 0.0028}},
		},
	}, "")
	if !strings.Contains(r1, "⚠") || !strings.Contains(r1, "MISSING in config") {
		t.Errorf("missing ⚠/MISSING: %s", r1)
	}
	// ? for missing_in_manifest
	r2 := formatReport([]ProviderAuditResult{
		{ProviderID: "p", Source: "https://x.com",
			Models: []ModelAudit{{ModelID: "extra", Status: "missing_in_manifest",
				InputCost: 1.0, OutputCost: 2.0}},
		},
	}, "")
	if !strings.Contains(r2, "?") || !strings.Contains(r2, "NOT in manifest") {
		t.Errorf("missing ?/NOT in manifest: %s", r2)
	}
}

// ---------------------------------------------------------------------------
// updateConfig
// ---------------------------------------------------------------------------

func TestUpdateConfig_CorrectsAndPreserves(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1", Name: "Orig", Description: "desc", ContextLength: 128000,
			Tags: []string{"coding"}, InputCost: 99.0, OutputCost: 88.0},
	})
	path := filepath.Join(dir, "p.json")
	if err := updateConfig(path, ProviderManifest{Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28},
	}}); err != nil {
		t.Fatalf("updateConfig: %v", err)
	}
	cfg, err := providers.LoadProviderConfig(path)
	if err != nil {
		t.Fatalf("LoadProviderConfig: %v", err)
	}
	mi := cfg.Models.ModelInfo[0]
	if mi.InputCost != 0.14 || mi.OutputCost != 0.28 {
		t.Errorf("pricing wrong: in=%f out=%f", mi.InputCost, mi.OutputCost)
	}
	if mi.Name != "Orig" || mi.ContextLength != 128000 {
		t.Errorf("fields lost: name=%q ctx=%d", mi.Name, mi.ContextLength)
	}
}

func TestUpdateConfig_DoesNotAddModels(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1", InputCost: 0.14, OutputCost: 0.28},
	})
	manifest := ProviderManifest{Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28},
		{ID: "m2", InputPerMTok: 1.0, OutputPerMTok: 2.0},
	}}
	if err := updateConfig(filepath.Join(dir, "p.json"), manifest); err != nil {
		t.Fatalf("updateConfig: %v", err)
	}
	cfg, err := providers.LoadProviderConfig(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatalf("LoadProviderConfig: %v", err)
	}
	if len(cfg.Models.ModelInfo) != 1 {
		t.Errorf("expected 1 model (no adding), got %d", len(cfg.Models.ModelInfo))
	}
}

func TestUpdateConfig_DeletesStaleCachedCost(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1", InputCost: 0.14, OutputCost: 0.28, CachedCost: 0.5},
	})
	manifest := ProviderManifest{Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28},
	}}
	if err := updateConfig(filepath.Join(dir, "p.json"), manifest); err != nil {
		t.Fatalf("updateConfig: %v", err)
	}
	cfg, err := providers.LoadProviderConfig(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatalf("LoadProviderConfig: %v", err)
	}
	if cfg.Models.ModelInfo[0].CachedCost != 0 {
		t.Errorf("stale cached cost not removed: %f", cfg.Models.ModelInfo[0].CachedCost)
	}
}

func TestUpdateConfig_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1", InputCost: 0.14, OutputCost: 0.28},
	})
	if err := updateConfig(filepath.Join(dir, "p.json"), ProviderManifest{Models: []PricingEntry{
		{ID: "m1", InputPerMTok: 0.14, OutputPerMTok: 0.28},
	}}); err != nil {
		t.Fatalf("updateConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if data[len(data)-1] != '\n' {
		t.Error("missing trailing newline")
	}
	if err := json.Unmarshal(data, &map[string]interface{}{}); err != nil {
		t.Errorf("not valid JSON: %v", err)
	}
	if !strings.Contains(string(data), "  ") {
		t.Error("expected indented JSON")
	}
}

func TestUpdateConfig_NonExistentFile(t *testing.T) {
	err := updateConfig("/nonexistent.json", ProviderManifest{Models: []PricingEntry{{ID: "m1"}}})
	if err == nil {
		t.Error("expected error")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Errorf("error should mention 'read config': %v", err)
	}
}

// ---------------------------------------------------------------------------
// Manifest integrity
// ---------------------------------------------------------------------------

func TestManifest_Providers(t *testing.T) {
	for _, id := range []string{"deepseek", "zai", "openai", "minimax", "mistral"} {
		if _, ok := manifests[id]; !ok {
			t.Errorf("manifests missing %q", id)
		}
	}
}

func TestManifest_SourceAndVerification(t *testing.T) {
	for id, m := range manifests {
		if m.Source == "" || !strings.HasPrefix(m.Source, "https://") {
			t.Errorf("%q: invalid Source: %q", id, m.Source)
		}
		if m.LastVerified == "" {
			t.Errorf("%q: empty LastVerified", id)
		}
	}
}

func TestManifest_MinimumModelsAndUniqueness(t *testing.T) {
	for id, m := range manifests {
		if len(m.Models) < 2 {
			t.Errorf("%q has %d models, want ≥2", id, len(m.Models))
		}
		seen := make(map[string]bool)
		for _, mp := range m.Models {
			if seen[mp.ID] {
				t.Errorf("%q: duplicate model %q", id, mp.ID)
			}
			seen[mp.ID] = true
		}
	}
}
