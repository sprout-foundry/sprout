package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// ---------------------------------------------------------------------------
// Test allocateProbeBudget (Fix 1: fair-share budget distribution)
// ---------------------------------------------------------------------------

func TestAllocateProbeBudget(t *testing.T) {
	tests := []struct {
		name            string
		numProviders    int
		maxProbes       int
		wantPerProvider int
		wantTotal       int
	}{
		{
			name:            "6 providers, 50 probes → cap 8 each",
			numProviders:    6,
			maxProbes:       50,
			wantPerProvider: 8, // 50/6 = 8
			wantTotal:       50,
		},
		{
			name:            "3 providers, 15 probes → cap 5 each",
			numProviders:    3,
			maxProbes:       15,
			wantPerProvider: 5, // 15/3 = 5
			wantTotal:       15,
		},
		{
			name:            "3 providers, 10 probes → cap 5 each (min floor)",
			numProviders:    3,
			maxProbes:       10,
			wantPerProvider: 5, // 10/3 = 3, but min is 5
			wantTotal:       10,
		},
		{
			name:            "1 provider, 50 probes → cap 50",
			numProviders:    1,
			maxProbes:       50,
			wantPerProvider: 50, // 50/1 = 50
			wantTotal:       50,
		},
		{
			name:            "0 providers → cap 0",
			numProviders:    0,
			maxProbes:       50,
			wantPerProvider: 0,
			wantTotal:       50,
		},
		{
			name:            "11 providers, 50 probes → cap 5 each (min floor)",
			numProviders:    11,
			maxProbes:       50,
			wantPerProvider: 5, // 50/11 = 4, but min is 5
			wantTotal:       50,
		},
		{
			name:            "2 providers, 50 probes → cap 25 each",
			numProviders:    2,
			maxProbes:       50,
			wantPerProvider: 25,
			wantTotal:       50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPerProvider, gotTotal := allocateProbeBudget(tt.numProviders, tt.maxProbes)
			if gotPerProvider != tt.wantPerProvider {
				t.Errorf("perProvider = %d, want %d", gotPerProvider, tt.wantPerProvider)
			}
			if gotTotal != tt.wantTotal {
				t.Errorf("total = %d, want %d", gotTotal, tt.wantTotal)
			}
		})
	}
}

// TestEnrich_DistributesBudgetAcrossProviders verifies the budget allocation
// logic ensures each provider gets a fair share. With 3 providers and 15
// probes, each should get at least 5 (the per-provider cap).
func TestEnrich_DistributesBudgetAcrossProviders(t *testing.T) {
	// 3 providers, 15 max probes.
	perProvider, _ := allocateProbeBudget(3, 15)
	if perProvider < 5 {
		t.Errorf("per-provider cap too low: got %d, want >= 5", perProvider)
	}
	// With 3 providers × 5 cap = 15, exactly matches the budget.
	// With 3 providers × 30 models each = 90 eligible, only 15 get probed.
	// Each provider gets 5 probes, total = 15.
	if perProvider*3 != 15 {
		t.Errorf("3 providers × cap %d = %d, want 15", perProvider, perProvider*3)
	}
}

// ---------------------------------------------------------------------------
// Test recommendedRoles (Fix 2: probe is ground truth, no EligibleRoles filter)
// ---------------------------------------------------------------------------

func TestRecommendedRoles(t *testing.T) {
	tests := []struct {
		name    string
		passed  bool
		complex bool
		want    []string
	}{
		{
			name:    "passed + complex → subagent + primary",
			passed:  true,
			complex: true,
			want:    []string{modelcontract.RoleSubagent, modelcontract.RolePrimary},
		},
		{
			name:    "passed only → subagent",
			passed:  true,
			complex: false,
			want:    []string{modelcontract.RoleSubagent},
		},
		{
			name:    "complex only (shouldn't happen, but handle gracefully) → primary",
			passed:  false,
			complex: true,
			want:    []string{modelcontract.RolePrimary},
		},
		{
			name:    "failed → empty",
			passed:  false,
			complex: false,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recommendedRoles(tt.passed, tt.complex)
			if len(got) != len(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got %v, want %v", got, tt.want)
					return
				}
			}
		})
	}
}

// TestRecommendedRoles_IgnoresEligibleRoles verifies that recommendedRoles
// no longer filters by EligibleRoles — the probe is the ground truth.
func TestRecommendedRoles_IgnoresEligibleRoles(t *testing.T) {
	// Even if the model had empty eligible_roles, a passing probe should
	// still produce recommended_roles. This is the key behavioral change.
	got := recommendedRoles(true, true)
	if len(got) != 2 {
		t.Errorf("probe passed+complex should produce 2 roles regardless of eligible_roles, got %v", got)
	}
	if got[0] != modelcontract.RoleSubagent || got[1] != modelcontract.RolePrimary {
		t.Errorf("expected [subagent primary], got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Test collectProviderFiles (Fix 1: deterministic ordering)
// ---------------------------------------------------------------------------

func TestCollectProviderFiles(t *testing.T) {
	tmpDir := t.TempDir()
	modelsDir := filepath.Join(tmpDir, "models")
	os.MkdirAll(modelsDir, 0o755)

	// Create test provider files.
	providers := []string{"openrouter", "deepinfra", "chutes"}
	for _, p := range providers {
		pf := modelcontract.ProviderFile{
			SchemaVersion: modelcontract.SchemaVersion,
			Provider:      p,
			Models:        []modelcontract.CanonicalModel{{ID: "test-model"}},
		}
		data, _ := json.MarshalIndent(pf, "", "  ")
		os.WriteFile(filepath.Join(modelsDir, p+".json"), data, 0o644)
	}
	// Create an index file that should be skipped.
	os.WriteFile(filepath.Join(modelsDir, "index.json"), []byte("{}"), 0o644)

	got := collectProviderFiles(tmpDir)
	if len(got) != 3 {
		t.Fatalf("got %d providers, want 3", len(got))
	}

	// Verify sorted order.
	want := []string{"chutes", "deepinfra", "openrouter"}
	for i, p := range got {
		if p.providerID != want[i] {
			t.Errorf("provider[%d] = %s, want %s", i, p.providerID, want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Test buildCanonicalFromConfig (Fix 3: embedded config fallback)
// ---------------------------------------------------------------------------

func TestBuildCanonicalFromConfig(t *testing.T) {
	cfg := &providers.ProviderConfig{
		Name: "test-provider",
		Models: providers.ModelConfig{
			DefaultContextLimit: 128000,
			ModelInfo: []providers.ModelInfo{
				{
					ID:            "model-a",
					Name:          "Model A",
					ContextLength: 200000,
					Tags:          []string{"tools", "reasoning"},
					InputCost:     1.0,
					OutputCost:    3.0,
				},
				{
					ID:            "model-b",
					Name:          "Model B",
					ContextLength: 64000,
					Tags:          []string{"tools"},
				},
			},
			AvailableModels: []string{"model-a", "model-c"},
		},
	}

	got := buildCanonicalFromConfig("test-provider", cfg)
	if len(got) != 3 {
		t.Fatalf("got %d models, want 3 (model-a, model-b, model-c)", len(got))
	}

	// Check model-a (from model_info, with pricing).
	a := got[0]
	if a.ID != "model-a" {
		t.Errorf("first model ID = %s, want model-a", a.ID)
	}
	if a.ContextWindow != 200000 {
		t.Errorf("model-a context = %d, want 200000", a.ContextWindow)
	}
	if a.Pricing == nil {
		t.Error("model-a should have pricing from model_info")
	} else {
		if a.Pricing.InputPerMTok != 1.0 {
			t.Errorf("model-a input cost = %f, want 1.0", a.Pricing.InputPerMTok)
		}
	}

	// Check model-c (from available_models only, uses defaults).
	c := got[2]
	if c.ID != "model-c" {
		t.Errorf("third model ID = %s, want model-c", c.ID)
	}
	if c.ContextWindow != 128000 {
		t.Errorf("model-c context = %d, want 128000 (default)", c.ContextWindow)
	}
	if c.Pricing != nil {
		t.Error("model-c should NOT have pricing (not in model_info)")
	}
}

// TestBuildCanonicalFromConfig_EmptyConfig verifies graceful handling of
// configs with no models defined.
func TestBuildCanonicalFromConfig_EmptyConfig(t *testing.T) {
	cfg := &providers.ProviderConfig{
		Name:   "empty-provider",
		Models: providers.ModelConfig{},
	}

	got := buildCanonicalFromConfig("empty-provider", cfg)
	if len(got) != 0 {
		t.Errorf("expected 0 models from empty config, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Test loadProviderFile and writeProviderFile round-trip
// ---------------------------------------------------------------------------

func TestLoadWriteProviderFileRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test-provider.json")

	original := &modelcontract.ProviderFile{
		SchemaVersion: modelcontract.SchemaVersion,
		Provider:      "test-provider",
		GeneratedAt:   "2025-01-01T00:00:00Z",
		Models: []modelcontract.CanonicalModel{
			{
				ID:            "model-1",
				Provider:      "test-provider",
				ContextWindow: 128000,
				EligibleRoles: []string{modelcontract.RoleSubagent},
				Probe: &modelcontract.ProbeResult{
					Passed:       true,
					Complex:      true,
					Score:        0.85,
					LastProbedAt: "2025-01-01T00:00:00Z",
					ProbeVersion: "gates+todos-v5",
				},
				RecommendedRoles: []string{modelcontract.RoleSubagent, modelcontract.RolePrimary},
			},
		},
	}

	if err := writeProviderFile(path, original); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := loadProviderFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Provider != original.Provider {
		t.Errorf("provider = %s, want %s", loaded.Provider, original.Provider)
	}
	if len(loaded.Models) != 1 {
		t.Fatalf("models count = %d, want 1", len(loaded.Models))
	}
	m := loaded.Models[0]
	if m.ID != "model-1" {
		t.Errorf("model ID = %s, want model-1", m.ID)
	}
	if m.Probe == nil || !m.Probe.Passed {
		t.Error("probe data not preserved through round-trip")
	}
	if len(m.RecommendedRoles) != 2 {
		t.Errorf("recommended_roles = %v, want 2 roles", m.RecommendedRoles)
	}
}

// ---------------------------------------------------------------------------
// Integration-style test: embedded config fallback creates canonical file
// ---------------------------------------------------------------------------

func TestEnrich_FallsBackToEmbeddedConfig(t *testing.T) {
	tmpDir := t.TempDir()
	modelsDir := filepath.Join(tmpDir, "models")
	os.MkdirAll(modelsDir, 0o755)

	embeddedDir := t.TempDir()
	cfg := &providers.ProviderConfig{
		Name: "foo",
		Models: providers.ModelConfig{
			DefaultContextLimit: 128000,
			AvailableModels:     []string{"m1", "m2"},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(embeddedDir, "foo.json"), data, 0o644)

	// Verify no canonical file exists yet.
	canonicalPath := filepath.Join(modelsDir, "foo.json")
	if _, err := os.Stat(canonicalPath); err == nil {
		t.Fatal("canonical file should not exist yet")
	}

	// Build canonical models from the embedded config.
	loaded, err := providers.LoadProviderConfig(filepath.Join(embeddedDir, "foo.json"))
	if err != nil {
		t.Fatalf("load embedded config: %v", err)
	}
	models := buildCanonicalFromConfig("foo", loaded)
	if len(models) != 2 {
		t.Fatalf("expected 2 models from embedded config, got %d", len(models))
	}

	// Write the canonical file (simulating what enrich_registry does).
	pf := &modelcontract.ProviderFile{
		SchemaVersion: modelcontract.SchemaVersion,
		Provider:      "foo",
		Models:        models,
	}
	if err := writeProviderFile(canonicalPath, pf); err != nil {
		t.Fatalf("write canonical: %v", err)
	}

	// Verify the canonical file was created with the right content.
	loadedPf, err := loadProviderFile(canonicalPath)
	if err != nil {
		t.Fatalf("load canonical: %v", err)
	}
	if loadedPf.Provider != "foo" {
		t.Errorf("provider = %s, want foo", loadedPf.Provider)
	}
	if len(loadedPf.Models) != 2 {
		t.Errorf("models = %d, want 2", len(loadedPf.Models))
	}
	if loadedPf.Models[0].ID != "m1" || loadedPf.Models[1].ID != "m2" {
		t.Errorf("model IDs = %v, want [m1 m2]",
			[]string{loadedPf.Models[0].ID, loadedPf.Models[1].ID})
	}
}

// ---------------------------------------------------------------------------
// Test maxInt helper
// ---------------------------------------------------------------------------

func TestMaxInt(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{3, 5, 5},
		{5, 3, 5},
		{5, 5, 5},
		{0, 1, 1},
		{-1, 0, 0},
	}
	for _, tt := range tests {
		got := maxInt(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("maxInt(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
