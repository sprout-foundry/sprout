package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// TestCarryForwardProbeData verifies that refresh_provider_catalog preserves
// probe + RecommendedRoles across a refresh. The CI's enrich_registry step
// handles the live-baseline merge; this is the local-file guarantee so running
// refresh_provider_catalog alone (locally, in a PR, in a debug workflow) does
// not silently drop probe data.
func TestCarryForwardProbeData(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	prior := modelcontract.ProviderFile{
		SchemaVersion: 2,
		Provider:      "openrouter",
		Models: []modelcontract.CanonicalModel{
			{
				ID: "openai/gpt-4o",
				Probe: &modelcontract.ProbeResult{
					Passed: true, Complex: true, Score: 0.95,
					LastProbedAt: "2026-06-20T00:00:00Z", ProbeVersion: "gates+todos-v5",
				},
				RecommendedRoles: []string{"primary", "subagent"},
			},
			{
				ID: "anthropic/claude-sonnet-4",
				Probe: &modelcontract.ProbeResult{
					Passed: true, Complex: false, Score: 0.6,
					LastProbedAt: "2026-06-20T00:00:00Z", ProbeVersion: "gates+todos-v5",
				},
				RecommendedRoles: []string{"subagent"},
			},
			{
				ID: "old/legacy-model",
				Probe: &modelcontract.ProbeResult{
					Passed: false, Complex: false, Score: 0.2,
				},
				RecommendedRoles: nil, // failed probe → no recommendation
			},
		},
	}
	priorBytes, err := json.MarshalIndent(prior, "", "  ")
	if err != nil {
		t.Fatalf("marshal prior: %v", err)
	}
	priorPath := filepath.Join(modelsDir, "openrouter.json")
	if err := os.WriteFile(priorPath, priorBytes, 0o755); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	// Fresh canonical list (e.g. from adapter): includes the two surviving
	// models plus a brand-new model. None carry probe data — that's the bug.
	fresh := []modelcontract.CanonicalModel{
		{ID: "openai/gpt-4o", ContextWindow: 128000},
		{ID: "anthropic/claude-sonnet-4", ContextWindow: 200000},
		{ID: "newcomer/just-launched", ContextWindow: 256000},
	}

	out := carryForwardProbeData(dir, "openrouter", fresh)

	if len(out) != 3 {
		t.Fatalf("expected 3 models in output, got %d", len(out))
	}

	// gpt-4o: probe carried forward with full detail
	if out[0].Probe == nil {
		t.Errorf("gpt-4o: Probe is nil after carry-forward")
	} else if !out[0].Probe.Passed || !out[0].Probe.Complex {
		t.Errorf("gpt-4o: Probe fields lost in carry-forward: %+v", out[0].Probe)
	}
	if got := out[0].RecommendedRoles; len(got) != 2 || got[0] != "primary" || got[1] != "subagent" {
		t.Errorf("gpt-4o: RecommendedRoles = %v, want [primary subagent]", got)
	}

	// claude-sonnet-4: subagent-only recommendation carried forward
	if out[1].Probe == nil || !out[1].Probe.Passed || out[1].Probe.Complex {
		t.Errorf("claude-sonnet-4: probe not preserved correctly: %+v", out[1].Probe)
	}
	if got := out[1].RecommendedRoles; len(got) != 1 || got[0] != "subagent" {
		t.Errorf("claude-sonnet-4: RecommendedRoles = %v, want [subagent]", got)
	}

	// newcomer: not in prior file → no probe data stamped
	if out[2].Probe != nil {
		t.Errorf("newcomer: should not have probe data (not in prior), got %+v", out[2].Probe)
	}
	if len(out[2].RecommendedRoles) != 0 {
		t.Errorf("newcomer: should not have RecommendedRoles, got %v", out[2].RecommendedRoles)
	}
}

// TestCarryForwardProbeData_NoPriorFile verifies the no-op path: when there's
// no prior per-provider file, fresh models are returned untouched.
func TestCarryForwardProbeData_NoPriorFile(t *testing.T) {
	dir := t.TempDir()
	// Intentionally do NOT create models/<provider>.json.
	fresh := []modelcontract.CanonicalModel{
		{ID: "some/model", ContextWindow: 100000},
	}
	out := carryForwardProbeData(dir, "openrouter", fresh)
	if len(out) != 1 || out[0].ID != "some/model" {
		t.Fatalf("expected fresh passthrough, got %+v", out)
	}
	if out[0].Probe != nil || len(out[0].RecommendedRoles) != 0 {
		t.Errorf("fresh model should not gain probe data when no prior file exists")
	}
}

// TestCarryForwardProbeData_CorruptPriorFile verifies that a malformed prior
// file is logged and ignored (the refresh continues with fresh data only).
func TestCarryForwardProbeData_CorruptPriorFile(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	priorPath := filepath.Join(modelsDir, "openrouter.json")
	if err := os.WriteFile(priorPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt prior: %v", err)
	}

	fresh := []modelcontract.CanonicalModel{{ID: "some/model", ContextWindow: 100000}}
	out := carryForwardProbeData(dir, "openrouter", fresh)

	if len(out) != 1 || out[0].Probe != nil {
		t.Errorf("corrupt prior should be ignored; got %+v", out[0].Probe)
	}
}

// TestCarryForwardProbeData_PreservesFreshFields verifies the carry-forward
// only adds Probe + RecommendedRoles; everything else (context, tags, pricing)
// comes from the fresh adapter output and is left alone.
func TestCarryForwardProbeData_PreservesFreshFields(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	prior := modelcontract.ProviderFile{
		SchemaVersion: 2,
		Provider:      "openrouter",
		Models: []modelcontract.CanonicalModel{
			{
				ID:               "openai/gpt-4o",
				Probe:            &modelcontract.ProbeResult{Passed: true, Complex: true},
				RecommendedRoles: []string{"primary", "subagent"},
				// ContextWindow left zero in the prior — should NOT overwrite the
				// fresh value via the carry-forward (Probe + RecommendedRoles only).
			},
		},
	}
	priorBytes, _ := json.MarshalIndent(prior, "", "  ")
	if err := os.WriteFile(filepath.Join(modelsDir, "openrouter.json"), priorBytes, 0o755); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	fresh := []modelcontract.CanonicalModel{
		{
			ID:            "openai/gpt-4o",
			ContextWindow: 128000, // fresh, must be preserved
			Capabilities:  modelcontract.Capabilities{Tools: modelcontract.Bool(true)},
		},
	}
	out := carryForwardProbeData(dir, "openrouter", fresh)

	if out[0].ContextWindow != 128000 {
		t.Errorf("ContextWindow from fresh data lost: got %d, want 128000", out[0].ContextWindow)
	}
	if out[0].Capabilities.Tools == nil || !*out[0].Capabilities.Tools {
		t.Errorf("Capabilities from fresh data lost: %+v", out[0].Capabilities)
	}
	if out[0].Probe == nil || !out[0].Probe.Passed {
		t.Errorf("Probe from prior not stamped onto fresh: %+v", out[0].Probe)
	}
}

// helperResetOpenRouterCache resets the OpenRouter cache and restores the URL
// after the test. Call it at the start of each test; it registers cleanup on t.
func helperResetOpenRouterCache(t *testing.T) {
	openRouterModelsCache = nil
	prevURL := openRouterModelsURL
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	t.Cleanup(func() {
		openRouterModelsURL = prevURL
		openRouterModelsCache = nil
	})
}

// TestEnrichFromOpenRouter_FillsPricingGaps verifies that a model with
// Pricing == nil gets pricing filled from a matching OpenRouter entry.
func TestEnrichFromOpenRouter_FillsPricingGaps(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [{
				"id": "deepseek/deepseek-v4-flash",
				"pricing": {
					"prompt": "0.00000009",
					"completion": "0.00000018",
					"input_cache_read": "0.000000018"
				}
			}]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	models := []modelcontract.CanonicalModel{
		{ID: "deepseek-v4-flash"},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing == nil {
		t.Fatalf("expected pricing to be filled, got nil")
	}
	if out[0].Pricing.Source != "openrouter-cross-ref" {
		t.Errorf("Source = %q, want %q", out[0].Pricing.Source, "openrouter-cross-ref")
	}
	if out[0].Pricing.InputPerMTok != 0.09 {
		t.Errorf("InputPerMTok = %f, want 0.09", out[0].Pricing.InputPerMTok)
	}
	if out[0].Pricing.OutputPerMTok != 0.18 {
		t.Errorf("OutputPerMTok = %f, want 0.18", out[0].Pricing.OutputPerMTok)
	}
	if out[0].Pricing.CachedPerMTok != 0.018 {
		t.Errorf("CachedPerMTok = %f, want 0.018", out[0].Pricing.CachedPerMTok)
	}
	if out[0].Pricing.Currency != "USD" {
		t.Errorf("Currency = %q, want %q", out[0].Pricing.Currency, "USD")
	}
}

// TestEnrichFromOpenRouter_DoesNotOverwriteExisting verifies that models
// that already have Pricing are NOT overwritten.
func TestEnrichFromOpenRouter_DoesNotOverwriteExisting(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [{
				"id": "deepseek/deepseek-v4-flash",
				"pricing": {
					"prompt": "0.00000099",
					"completion": "0.00000099"
				}
			}]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	existing := &modelcontract.Pricing{
		InputPerMTok:  1.0,
		OutputPerMTok: 2.0,
		Currency:      "USD",
		Source:        "native-api",
	}
	models := []modelcontract.CanonicalModel{
		{ID: "deepseek-v4-flash", Pricing: existing},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing != existing {
		t.Errorf("existing pricing was replaced")
	}
	if out[0].Pricing.InputPerMTok != 1.0 {
		t.Errorf("InputPerMTok = %f, want 1.0 (should not be overwritten)", out[0].Pricing.InputPerMTok)
	}
	if out[0].Pricing.Source != "native-api" {
		t.Errorf("Source = %q, want %q", out[0].Pricing.Source, "native-api")
	}
}

// TestEnrichFromOpenRouter_NoMatch verifies that a model with no OpenRouter
// match stays at Pricing == nil.
func TestEnrichFromOpenRouter_NoMatch(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data": [{"id": "other/model", "pricing": {"prompt": "0.01", "completion": "0.02"}}]}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	models := []modelcontract.CanonicalModel{
		{ID: "unknown-model-xyz"},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing != nil {
		t.Errorf("expected no pricing (no match), got %+v", out[0].Pricing)
	}
}

// TestEnrichFromOpenRouter_APIUnreachable verifies that when the OpenRouter
// API is unreachable, models are returned unchanged (no panic).
func TestEnrichFromOpenRouter_APIUnreachable(t *testing.T) {
	helperResetOpenRouterCache(t)

	// Point at an address that will refuse connections.
	openRouterModelsURL = "http://127.0.0.1:1"

	models := []modelcontract.CanonicalModel{
		{ID: "some/model"},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing != nil {
		t.Errorf("expected no pricing when API is unreachable, got %+v", out[0].Pricing)
	}
}

// ---------------------------------------------------------------------------
// Gap 1 (CRITICAL): Zero-valued non-nil Pricing handling
// ---------------------------------------------------------------------------

// TestEnrichFromConfig_FillsZeroValuedPricing verifies that a model with a
// non-nil Pricing struct that has zero values gets pricing from the config.
// This covers the ZAI/GLM case where the OpenAI-compatible endpoint returns
// empty pricing fields (non-nil Pricing with all zeros).
func TestEnrichFromConfig_FillsZeroValuedPricing(t *testing.T) {
	// Change to repo root so the relative config path resolves.
	repoRoot := findRepoRoot(t)
	t.Chdir(repoRoot)

	// Use deepseek config which has real pricing for deepseek-v4-flash.
	models := []modelcontract.CanonicalModel{
		{
			ID: "deepseek-v4-flash",
			// Non-nil pricing with zero values — simulates an OpenAI-compatible
			// endpoint that returns empty pricing.
			Pricing: &modelcontract.Pricing{},
		},
	}

	out := enrichFromConfig("deepseek", models)

	if out[0].Pricing == nil {
		t.Fatalf("expected pricing to be filled for zero-valued Pricing, got nil")
	}
	if out[0].Pricing.Source != "embedded-config" {
		t.Errorf("Source = %q, want %q", out[0].Pricing.Source, "embedded-config")
	}
	if out[0].Pricing.InputPerMTok != 0.14 {
		t.Errorf("InputPerMTok = %f, want 0.14", out[0].Pricing.InputPerMTok)
	}
	if out[0].Pricing.OutputPerMTok != 0.28 {
		t.Errorf("OutputPerMTok = %f, want 0.28", out[0].Pricing.OutputPerMTok)
	}
}

// findRepoRoot walks up from the test directory to locate the repo root
// (identified by the pkg/agent_providers/configs directory).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "pkg", "agent_providers", "configs")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find repo root (no pkg/agent_providers/configs found)")
	return ""
}

// TestEnrichFromOpenRouter_FillsZeroValuedPricing verifies that enrichFromOpenRouter
// also fills pricing when the existing Pricing is non-nil but zero-valued.
func TestEnrichFromOpenRouter_FillsZeroValuedPricing(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [{
				"id": "deepseek/deepseek-v4-flash",
				"pricing": {
					"prompt": "0.00000009",
					"completion": "0.00000018"
				}
			}]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	models := []modelcontract.CanonicalModel{
		{
			ID:      "deepseek-v4-flash",
			Pricing: &modelcontract.Pricing{}, // non-nil, zero-valued
		},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing == nil {
		t.Fatalf("expected pricing to be filled for zero-valued Pricing, got nil")
	}
	if out[0].Pricing.InputPerMTok != 0.09 {
		t.Errorf("InputPerMTok = %f, want 0.09", out[0].Pricing.InputPerMTok)
	}
	if out[0].Pricing.OutputPerMTok != 0.18 {
		t.Errorf("OutputPerMTok = %f, want 0.18", out[0].Pricing.OutputPerMTok)
	}
}

// TestEnrichFromOpenRouter_RespectsNonZeroPricing ensures that a non-nil,
// non-zero Pricing struct is NOT overwritten (existing behavior preserved).
func TestEnrichFromOpenRouter_RespectsNonZeroPricing(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [{
				"id": "deepseek/deepseek-v4-flash",
				"pricing": {
					"prompt": "0.00000099",
					"completion": "0.00000099"
				}
			}]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	models := []modelcontract.CanonicalModel{
		{
			ID: "deepseek-v4-flash",
			Pricing: &modelcontract.Pricing{
				InputPerMTok:  5.0,
				OutputPerMTok: 10.0,
			},
		},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing.InputPerMTok != 5.0 {
		t.Errorf("InputPerMTok = %f, want 5.0 (should not be overwritten)", out[0].Pricing.InputPerMTok)
	}
}

// ---------------------------------------------------------------------------
// Gap 3 (HIGH): Config-only models missing from catalog
// ---------------------------------------------------------------------------

// TestMergeConfigOnlyModels verifies that models in the config but not in the
// API results get added to the model list.
func TestMergeConfigOnlyModels(t *testing.T) {
	repoRoot := findRepoRoot(t)
	t.Chdir(repoRoot)

	// Simulate API returning only deepseek-v4-flash.
	models := []modelcontract.CanonicalModel{
		{ID: "deepseek-v4-flash"},
	}

	out := mergeConfigOnlyModels("deepseek", models)

	// Should have the original plus config-only models.
	found := make(map[string]bool)
	for _, m := range out {
		found[m.ID] = true
	}

	if !found["deepseek-v4-flash"] {
		t.Error("expected original model deepseek-v4-flash to be present")
	}
	// deepseek-chat is in config but not in our simulated API results
	if !found["deepseek-chat"] {
		t.Error("expected config-only model deepseek-chat to be added")
	}
	// deepseek-reasoner is also config-only
	if !found["deepseek-reasoner"] {
		t.Error("expected config-only model deepseek-reasoner to be added")
	}
}

// TestMergeConfigOnlyModels_NoDuplicates ensures that models already present
// in the API results are not duplicated by the config merge.
func TestMergeConfigOnlyModels_NoDuplicates(t *testing.T) {
	models := []modelcontract.CanonicalModel{
		{ID: "deepseek-v4-flash"},
		{ID: "deepseek-chat"},
	}

	out := mergeConfigOnlyModels("deepseek", models)

	counts := make(map[string]int)
	for _, m := range out {
		counts[m.ID]++
	}

	if counts["deepseek-v4-flash"] != 1 {
		t.Errorf("deepseek-v4-flash appears %d times, want 1", counts["deepseek-v4-flash"])
	}
	if counts["deepseek-chat"] != 1 {
		t.Errorf("deepseek-chat appears %d times, want 1", counts["deepseek-chat"])
	}
}

// TestMergeConfigOnlyModels_NoConfig verifies no-op when config doesn't exist.
func TestMergeConfigOnlyModels_NoConfig(t *testing.T) {
	models := []modelcontract.CanonicalModel{
		{ID: "some/model"},
	}
	out := mergeConfigOnlyModels("nonexistent-provider", models)
	if len(out) != 1 {
		t.Errorf("expected passthrough for unknown provider, got %d models", len(out))
	}
}

// TestMergeConfigOnlyModels_CarriesMetadata verifies that merged-in config
// models have their metadata (name, context, capabilities) populated.
func TestMergeConfigOnlyModels_CarriesMetadata(t *testing.T) {
	repoRoot := findRepoRoot(t)
	t.Chdir(repoRoot)

	models := []modelcontract.CanonicalModel{}

	out := mergeConfigOnlyModels("deepseek", models)

	// Find deepseek-chat in the output
	var found *modelcontract.CanonicalModel
	for i := range out {
		if out[i].ID == "deepseek-chat" {
			found = &out[i]
			break
		}
	}
	if found == nil {
		t.Fatal("deepseek-chat not found in merged models")
	}
	if found.DisplayName != "DeepSeek Chat" {
		t.Errorf("DisplayName = %q, want %q", found.DisplayName, "DeepSeek Chat")
	}
	if found.ContextWindow != 1000000 {
		t.Errorf("ContextWindow = %d, want 1000000", found.ContextWindow)
	}
	if found.Source != "embedded-config" {
		t.Errorf("Source = %q, want %q", found.Source, "embedded-config")
	}
	if found.Status != modelcontract.StatusActive {
		t.Errorf("Status = %q, want %q", found.Status, modelcontract.StatusActive)
	}
}

// ---------------------------------------------------------------------------
// Gap 4 (MEDIUM): Negative sentinel pricing from OpenRouter meta-models
// ---------------------------------------------------------------------------

// TestEnrichFromOpenRouter_SkipsNegativeSentinel verifies that negative
// pricing values from OpenRouter meta-models are not applied and no
// zero-valued Pricing struct is assigned (Pricing stays nil).
func TestEnrichFromOpenRouter_SkipsNegativeSentinel(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Simulate OpenRouter meta-model with negative sentinel pricing.
		fmt.Fprint(w, `{
			"data": [{
				"id": "openrouter/auto",
				"pricing": {
					"prompt": "-1000000",
					"completion": "-1000000",
					"input_cache_read": "-1000000"
				}
			}]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	models := []modelcontract.CanonicalModel{
		{ID: "auto"},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	// When all values are negative, no pricing struct should be assigned
	// (avoids a zero-valued struct that downstream code could misinterpret
	// as "priced at $0" rather than "pricing unknown").
	if out[0].Pricing != nil {
		t.Errorf("expected Pricing to remain nil when all sentinel values are negative, got %+v", out[0].Pricing)
	}
}

// TestEnrichFromOpenRouter_SkipsNegativeSentinel_MixedValues verifies that
// when some fields are negative but others are positive, the positive ones
// are kept and the negative ones are zeroed.
func TestEnrichFromOpenRouter_SkipsNegativeSentinel_MixedValues(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [{
				"id": "some/mixed-model",
				"pricing": {
					"prompt": "0.000001",
					"completion": "-1000000",
					"input_cache_read": "0.0000005"
				}
			}]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	models := []modelcontract.CanonicalModel{
		{ID: "mixed-model"},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing == nil {
		t.Fatalf("expected pricing to exist, got nil")
	}
	if out[0].Pricing.InputPerMTok != 1.0 {
		t.Errorf("InputPerMTok = %f, want 1.0", out[0].Pricing.InputPerMTok)
	}
	if out[0].Pricing.OutputPerMTok != 0 {
		t.Errorf("OutputPerMTok = %f, want 0 (negative sentinel skipped)", out[0].Pricing.OutputPerMTok)
	}
	if out[0].Pricing.CachedPerMTok != 0.5 {
		t.Errorf("CachedPerMTok = %f, want 0.5", out[0].Pricing.CachedPerMTok)
	}
}

// ---------------------------------------------------------------------------
// Gap 5 (MEDIUM): OpenAI dated-snapshot models miss pricing cross-ref
// ---------------------------------------------------------------------------

// TestStripDateSuffix verifies the date-stripping helper function.
func TestStripDateSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"gpt-5-2025-08-07", "gpt-5"},
		{"gpt-4o-2024-05-13", "gpt-4o"},
		{"claude-sonnet-4-2026-01-15", "claude-sonnet-4"},
		{"gpt-5", "gpt-5"},
		{"deepseek-v4-flash", "deepseek-v4-flash"},
		{"some-model-2024", "some-model-2024"},  // no -MM-DD suffix
		{"model-12345", "model-12345"},          // no date pattern
		{"", ""},
		{"gpt-5-2025-07", "gpt-5-2025-07"},      // partial date (no day)
	}
	for _, tt := range tests {
		got := stripDateSuffix(tt.input)
		if got != tt.expected {
			t.Errorf("stripDateSuffix(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestLookupModel_DirectMatch verifies the lookup helper with exact ID match.
func TestLookupModel_DirectMatch(t *testing.T) {
	lookup := map[string]providers.ModelInfo{
		"gpt-5": {ID: "gpt-5", InputCost: 1.0, OutputCost: 2.0},
	}

	mi, ok := lookupModel(lookup, "gpt-5")
	if !ok {
		t.Fatal("expected direct match to succeed")
	}
	if mi.InputCost != 1.0 {
		t.Errorf("InputCost = %f, want 1.0", mi.InputCost)
	}
}

// TestLookupModel_DateFallback verifies fuzzy matching strips the date suffix.
func TestLookupModel_DateFallback(t *testing.T) {
	lookup := map[string]providers.ModelInfo{
		"gpt-5": {ID: "gpt-5", InputCost: 1.0, OutputCost: 2.0},
	}

	mi, ok := lookupModel(lookup, "gpt-5-2025-08-07")
	if !ok {
		t.Fatal("expected fuzzy date match to succeed")
	}
	if mi.InputCost != 1.0 {
		t.Errorf("InputCost = %f, want 1.0", mi.InputCost)
	}
}

// TestLookupModel_NoMatch verifies no match returns false.
func TestLookupModel_NoMatch(t *testing.T) {
	lookup := map[string]providers.ModelInfo{
		"gpt-5": {ID: "gpt-5"},
	}
	_, ok := lookupModel(lookup, "unknown-model-2025-01-01")
	if ok {
		t.Error("expected no match for unknown model")
	}
}


// TestEnrichFromOpenRouter_FuzzyDateMatch verifies that a dated model ID
// gets pricing from an undated OpenRouter entry via date suffix stripping.
func TestEnrichFromOpenRouter_FuzzyDateMatch(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// OpenRouter returns the undated model "gpt-5".
		fmt.Fprint(w, `{
			"data": [{
				"id": "openai/gpt-5",
				"pricing": {
					"prompt": "0.000012",
					"completion": "0.000060"
				}
			}]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	// Our provider API returns the dated snapshot ID.
	models := []modelcontract.CanonicalModel{
		{ID: "gpt-5-2025-08-07"},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing == nil {
		t.Fatalf("expected pricing from fuzzy match, got nil")
	}
	if out[0].Pricing.InputPerMTok != 12.0 {
		t.Errorf("InputPerMTok = %f, want 12.0", out[0].Pricing.InputPerMTok)
	}
	if out[0].Pricing.OutputPerMTok != 60.0 {
		t.Errorf("OutputPerMTok = %f, want 60.0", out[0].Pricing.OutputPerMTok)
	}
}

// ---------------------------------------------------------------------------
// Gap 6 (LOW): Filter OpenRouter meta-models from catalog
// ---------------------------------------------------------------------------

// TestNormalizeModels_FiltersMetaAndNegative verifies that normalizeModels
// drops OpenRouter meta-models and models with negative costs.
func TestNormalizeModels_FiltersMetaAndNegative(t *testing.T) {
	models := []api.ModelInfo{
		{ID: "real-model", Name: "Real Model", InputCost: 1.0, OutputCost: 2.0},
		{ID: "openrouter/auto", Name: "Auto", InputCost: 0, OutputCost: 0},
		{ID: "openrouter/fusion", Name: "Fusion", InputCost: -1000000, OutputCost: -1000000},
		{ID: "openrouter/bodybuilder", Name: "Bodybuilder", InputCost: 0, OutputCost: 0},
		{ID: "openrouter/pareto-code", Name: "Pareto Code", InputCost: 0, OutputCost: 0},
		{ID: "negative-model", Name: "Negative", InputCost: -5, OutputCost: 10},
		{ID: "negative-output", Name: "Neg Out", InputCost: 5, OutputCost: -5},
		{ID: "openrouter/auto-beta", Name: "Auto Beta", InputCost: 0, OutputCost: 0},
	}

	out := normalizeModels(models)

	// Should only have real-model (others filtered).
	if len(out) != 1 {
		t.Errorf("expected 1 model, got %d: ", len(out))
		for _, m := range out {
			t.Logf("  - %s (input: %f, output: %f)", m.ID, m.InputCost, m.OutputCost)
		}
	}
	if len(out) > 0 && out[0].ID != "real-model" {
		t.Errorf("expected real-model, got %s", out[0].ID)
	}
}

// TestNormalizeModels_EmptyID verifies that blank IDs are skipped.
func TestNormalizeModels_EmptyID(t *testing.T) {
	models := []api.ModelInfo{
		{ID: "  "},
		{ID: ""},
		{ID: "valid", Name: "Valid"},
	}
	out := normalizeModels(models)
	if len(out) != 1 {
		t.Errorf("expected 1 model, got %d", len(out))
	}
	if len(out) > 0 && out[0].ID != "valid" {
		t.Errorf("expected 'valid', got %q", out[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Cross-cutting: zero-valued pricing should not prevent enrichFromOpenRouter
// ---------------------------------------------------------------------------

// TestEnrichFromOpenRouter_MixedZeroAndNil verifies behavior with a mix of
// nil Pricing, zero-valued Pricing, and valid Pricing in the same batch.
func TestEnrichFromOpenRouter_MixedZeroAndNil(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [
				{"id": "openai/m1", "pricing": {"prompt": "0.001", "completion": "0.002"}},
				{"id": "openai/m2", "pricing": {"prompt": "0.003", "completion": "0.004"}},
				{"id": "openai/m3", "pricing": {"prompt": "0.005", "completion": "0.006"}}
			]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	m3Ptr := &modelcontract.Pricing{InputPerMTok: 99.0, OutputPerMTok: 99.0}
	models := []modelcontract.CanonicalModel{
		{ID: "m1"},                                     // nil Pricing → should be filled
		{ID: "m2", Pricing: &modelcontract.Pricing{}},  // zero-valued → should be filled
		{ID: "m3", Pricing: m3Ptr},                      // non-zero → should be preserved
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing == nil {
		t.Error("m1: expected pricing filled for nil Pricing")
	} else if out[0].Pricing.InputPerMTok != 1000.0 {
		t.Errorf("m1: InputPerMTok = %f, want 1000.0", out[0].Pricing.InputPerMTok)
	}

	if out[1].Pricing == nil {
		t.Error("m2: expected pricing filled for zero-valued Pricing")
	} else if out[1].Pricing.InputPerMTok != 3000.0 {
		t.Errorf("m2: InputPerMTok = %f, want 3000.0", out[1].Pricing.InputPerMTok)
	}

	if out[2].Pricing != m3Ptr {
		t.Error("m3: expected existing non-zero pricing to be preserved")
	}
	if out[2].Pricing.InputPerMTok != 99.0 {
		t.Errorf("m3: InputPerMTok = %f, want 99.0 (should be preserved)", out[2].Pricing.InputPerMTok)
	}
}
